# gleaner 開發規格

資訊收集歸檔工具。規格日期：2026-10-19。

## 目標與範圍

本工具（gleaner）是一個以 Go 撰寫的命令列程式，在本機依排程執行，從 config 指定的網站與 RSS 收集符合關鍵字條件的內容，存成 Markdown 歸檔供日後閱讀。

已確定的決策：

| 項目 | 決定 |
| --- | --- |
| 開發語言 | Go 1.26 以上，編譯為單一執行檔 `gleaner.exe` |
| Go module | `github.com/elvis3434/gleaner24` |
| 執行地點 | 使用者本機（Windows 11），以工作排程器定時執行 |
| 開發方式 | Claude Code 雲端開發，本機實測 |
| 資料來源 | config 指定的多個網站、RSS / Atom；選配 Google News RSS |
| Config 格式 | TOML |
| 付費 API | 不使用 |
| 支援語言 | 英文、日文、繁體中文（可擴充） |

不在範圍內：全網搜尋、登入或付費牆後的內容、繞過反爬蟲機制或 CAPTCHA、將歸檔內容公開散布。

## 系統架構與處理流程

每個網址依序通過八個步驟，越便宜的檢查越早做，任何一步不符就略過並計數，只有新增或更新的內容會寫入歸檔。

1. **來源**：sites（sitemap、站內連結）、feeds、google_news 產生待抓網址。
2. **抓取**：爬行邊界、robots、限速、ETag 條件請求。→ 304 或在 `recheck_days` 內：略過。
3. **正文擷取**：去除選單與廣告，取標題、日期、正文。
4. **語言判定**：網址路徑 → lang 屬性 → lingua-go 偵測。→ 語言不符：略過。
5. **關鍵字比對**：正規化、繁簡互通後，對每個 topic 求值。→ 未命中：略過。
6. **去重與更新判定**：正文指紋與資料庫比對，判定新增或更新。→ 指紋相同：略過。
7. **歸檔與圖片**：Markdown + front matter，下載正文內圖片。
8. **寫入 log**：有效筆數 = 新增 + 更新，摘要存入 `runs` 表。

四個略過出口的筆數與有效筆數一起寫進本次 log；抓取或解析失敗另計為「失敗」。

## Config 規格

所有收集參數集中在一個 TOML 檔（預設 `config.toml`），程式啟動時載入並驗證，有錯誤就列出欄位名稱後中止，不開始收集。

```toml
[output]
archive_dir = "./archive"        # archive/2026/10/19/<topic>/<slug>.md
log_dir     = "./logs"
db          = "./data/index.db"
on_update   = "keep_versions"    # keep_versions | overwrite

[crawl]
user_agent            = "Gleaner/1.0 (personal archive)"
per_host_delay        = "2s"     # 同一網站兩次請求的最小間隔
concurrency           = 4        # 同時處理的網站數
timeout               = "20s"
retries               = 2
respect_robots        = true
max_pages_per_run     = 2000
recent_days           = 7        # 只處理最近 N 天發布的內容
recheck_days          = 3        # 已歸檔頁面 N 天內不重抓
follow_external_links = false

[[sources.sites]]
name          = "example-jp"
start_urls    = ["https://example.jp/news/"]
use_sitemap   = true
max_depth     = 2
max_pages     = 200
include_paths = ["/news/"]
exclude_paths = ["/tag/", "/login"]
languages     = ["ja"]           # 該站預期語言，用於提早過濾
render_js     = false            # true 時改用 chromedp 渲染

[[sources.feeds]]
name            = "example-feed"
url             = "https://example.com/feed.xml"
languages       = ["en"]
fetch_full_text = true           # 依 feed 連結抓回全文再比對

[sources.google_news]            # 選配，非官方保證的介面
enabled  = false
editions = ["zh-TW", "ja", "en"]

[[topics]]
name             = "tsmc"
query            = '(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞'
languages        = ["zh-TW", "en", "ja"]
match_in         = ["title", "body"]
case_sensitive   = false
cn_variant_merge = true          # 繁簡互通
sites            = []            # 空白 = 套用到所有來源

  [topics.images]
  enabled                = true
  min_width              = 300
  require_keyword_in_alt = false
  max_per_page           = 10
```

欄位說明：

| 區塊 | 用途 | 必填 |
| --- | --- | --- |
| `output` | 歸檔、log、資料庫位置與更新策略 | 是 |
| `crawl` | 全域爬行限制與禮儀 | 是（皆有預設值） |
| `sources.sites` | 指定網站，可多個，每站各自的邊界 | 至少一種來源 |
| `sources.feeds` | RSS / Atom，可多個 | 至少一種來源 |
| `sources.google_news` | 以 topic 關鍵字查詢的免費 RSS 入口 | 否 |
| `topics` | 關鍵字表達式與語言、圖片設定，可多個 | 是 |

每個網站的設定值若未指定，沿用 `crawl` 的全域值。時間長度欄位為字串，以 `time.ParseDuration` 解析。

## 功能規格

每次執行只處理 config 定義的範圍，範圍內的待爬佇列清空或碰到任一上限即結束，並在 log 記錄是哪一種結束。

### F1 來源

- **sites**：從 `start_urls` 出發；`use_sitemap = true` 時先讀 `robots.txt` 指向的 sitemap 與 `/sitemap.xml`，以 `lastmod` 篩出近期網址；其餘沿站內連結廣度優先爬行。
- **feeds**：解析 RSS / Atom；`fetch_full_text = true` 時依條目連結抓回原文。
- **google_news**（選配）：每個 topic 依版本組成查詢網址，取得結果連結後當作一般頁面處理；格式若變動，此來源記錄錯誤後略過，不影響其他來源。

### F2 爬行邊界

1. 網域邊界：只跟進與起始網址相同網域的連結；外部連結只記錄不跟進。
2. 深度上限 `max_depth`、每站頁數上限 `max_pages`、每次執行總上限 `max_pages_per_run`。
3. 時間窗 `recent_days`：依 sitemap `lastmod`、feed 發布日期或頁面 meta 的發布日期判斷；無法判斷日期的頁面照常處理。
4. 路徑規則：先套 `exclude_paths`，再套 `include_paths`（空白表示不限）。
5. 遵守 `robots.txt`；同一網站請求間隔不少於 `per_host_delay`。

### F3 關鍵字表達式

- 語法：關鍵字、`AND`、`OR`、`NOT`、括號；含空白的詞組用雙引號，例如 `"interest rate"`。
- 優先順序：`NOT` > `AND` > `OR`；括號優先於一切。
- 實作：自寫遞迴下降解析器，解析成語法樹後對文字求值；config 載入時就解析，語法錯誤立即回報位置。
- 比對方式：子字串比對（中日文無空格分詞）；比對前做 Unicode NFKC 正規化，全形半形視為相同；`case_sensitive = false` 時轉小寫；`cn_variant_merge = true` 時把文字與關鍵字都轉成繁體再比對。
- 記錄每篇命中的關鍵字清單，寫入存檔的 front matter。

### F4 語言判定

依成本由低到高依序檢查，任一步確定不符即丟棄：

1. 網址路徑（`/ja/`、`/zh-tw/`、`/en/`）。
2. HTTP `Content-Language` 標頭與 `<html lang>` 屬性。
3. 正文擷取後以 lingua-go 偵測；判定為中文時，再以繁體與簡體特有字元的比例區分 zh-TW 與 zh-CN。

語言代碼統一使用 `en`、`ja`、`zh-TW`、`zh-CN`。

### F5 去重與更新

- 網址正規化：小寫網域、移除 `#fragment`、移除 `utm_*`、`fbclid`、`gclid` 等追蹤參數、排序其餘參數、優先使用頁面的 `<link rel=canonical>`。
- 抓取時帶上前次的 `ETag` 與 `Last-Modified`；收到 304 視為未變。
- 內容指紋：正文擷取並正規化空白後的 SHA-256；與資料庫記錄相同即略過，不重存。
- 指紋不同時視為更新：`keep_versions` 存新檔 `<slug>_v2.md`，`overwrite` 覆蓋原檔；兩者都更新資料庫的最後更新時間。
- 已歸檔的網址不必每次重抓：`recheck_days`（預設 3 天）內抓過的頁面略過，列表頁、feed、sitemap 每次都抓。

### F6 存檔

- 路徑：`archive/YYYY/MM/DD/<topic>/<slug>.md`，日期為收集日；一篇命中多個 topic 時存在第一個 topic 下，front matter 列出全部。
- `slug`：標題轉成檔名安全字串，最長 80 字元，加上網址指紋前 8 碼避免重名。
- 內容格式：

```markdown
---
title: <標題>
source_url: <正規化後網址>
site: <來源名稱>
collected_at: 2026-10-19T09:00:00+08:00
published_at: 2026-10-18
language: zh-TW
topics: [tsmc]
matched_keywords: [台積電, 營收]
version: 1
images: [images/a1b2c3d4.jpg]
---

<正文 Markdown>
```

- 檔案寫入採先寫暫存檔再改名，避免中斷時留下殘缺檔案。

### F7 圖片

只處理正文區塊內的圖片，依下列規則篩選並下載：

1. 排除 logo、icon、廣告與追蹤像素：寬度小於 `min_width`（依 HTML 屬性，或下載後讀取圖檔尺寸）即丟棄。
2. alt 文字、圖說或檔名含命中關鍵字者優先；`require_keyword_in_alt = true` 時只收這類圖片。
3. 每頁最多 `max_per_page` 張；只收 jpg、png、webp、gif。
4. 以內容 SHA-256 去重，同一張圖只存一次，存放於該篇所在目錄的 `images/`。
5. 下載失敗只記錄，不影響文章歸檔。

### F8 收集 log

- 檔名：`logs/collect_YYYYMMDD_HHMM.log`，一天多次執行不會互相覆蓋。
- 有效筆數定義為「新增 + 更新」。
- 結尾摘要至少包含：

| 欄位 | 說明 |
| --- | --- |
| 開始 / 結束時間、耗時 | 本次執行時間 |
| 結束原因 | 範圍內完成 / 達到上限 |
| 抓取頁數 | 實際送出請求的頁數 |
| 新增 | 首次歸檔筆數 |
| 更新 | 內容變更後重新歸檔筆數 |
| 有效筆數 | 新增 + 更新 |
| 略過（重複） | 指紋相同或 304 |
| 略過（語言不符） | F4 篩除 |
| 略過（未命中） | 關鍵字不符 |
| 失敗 | 網路錯誤、解析錯誤，附網址 |
| 圖片下載 | 成功 / 失敗張數 |
| 各 topic、各網站有效筆數 | 分項統計 |

- 同時把摘要寫入資料庫 `runs` 表，日後可查詢歷次結果。

## 資料庫結構

使用單一 SQLite 檔（`data/index.db`），以純 Go 驅動 `modernc.org/sqlite` 存取，免裝 C 編譯器，Windows 可直接交叉編譯。啟動時自動建立或升級資料表。

```sql
CREATE TABLE pages (
  id             INTEGER PRIMARY KEY,
  url            TEXT NOT NULL UNIQUE,   -- 正規化後網址
  site           TEXT NOT NULL,
  title          TEXT,
  language       TEXT,
  published_at   TEXT,
  etag           TEXT,
  last_modified  TEXT,
  content_hash   TEXT,                   -- 正文 SHA-256
  status         TEXT NOT NULL,          -- archived | skipped | failed
  first_seen_at  TEXT NOT NULL,
  last_fetch_at  TEXT NOT NULL,
  last_change_at TEXT
);

CREATE TABLE archives (
  id          INTEGER PRIMARY KEY,
  page_id     INTEGER NOT NULL REFERENCES pages(id),
  version     INTEGER NOT NULL,
  file_path   TEXT NOT NULL,
  topics      TEXT NOT NULL,            -- JSON 陣列
  keywords    TEXT NOT NULL,            -- JSON 陣列
  archived_at TEXT NOT NULL,
  run_id      INTEGER NOT NULL REFERENCES runs(id)
);

CREATE TABLE images (
  hash      TEXT PRIMARY KEY,           -- 圖檔 SHA-256
  src_url   TEXT NOT NULL,
  file_path TEXT NOT NULL,
  width     INTEGER,
  height    INTEGER
);

CREATE TABLE archive_images (
  archive_id INTEGER NOT NULL REFERENCES archives(id),
  image_hash TEXT NOT NULL REFERENCES images(hash),
  PRIMARY KEY (archive_id, image_hash)
);

CREATE TABLE runs (
  id          INTEGER PRIMARY KEY,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  end_reason  TEXT,                     -- completed | limit_reached | aborted
  log_file    TEXT,
  stats_json  TEXT                      -- F8 摘要
);

-- 第四階段：全文搜尋
CREATE VIRTUAL TABLE archive_fts USING fts5(title, body, tokenize='trigram');
```

時間欄位一律存 RFC 3339 字串並帶時區（+08:00）。

## 目錄結構與模組

每個處理步驟是一個獨立套件，以介面串接，方便單獨測試與日後替換（例如把正文擷取改成呼叫 Python）。

```
gleaner24/
├── cmd/gleaner/main.go     # CLI 進入點
├── internal/
│   ├── config/     # 載入、預設值、驗證
│   ├── query/      # 布林表達式解析與求值
│   ├── source/     # sites、feeds、google_news；產生待抓網址
│   ├── crawler/    # 佇列、邊界、robots、限速、HTTP、條件請求
│   ├── render/     # chromedp 渲染（render_js 時才用）
│   ├── extract/    # 正文擷取、標題與日期、轉 Markdown
│   ├── lang/       # 語言判定、繁簡轉換
│   ├── normalize/  # 網址正規化、文字正規化
│   ├── store/      # SQLite 存取與資料表遷移
│   ├── archive/    # 寫入 Markdown 與版本處理
│   ├── images/     # 圖片篩選與下載
│   └── runlog/     # 本次 log 與統計
├── testdata/       # 離線測試用的 HTML、feed、sitemap
├── docs/SPEC.md
├── config.example.toml
├── Makefile        # build、test、交叉編譯 windows/amd64
├── CLAUDE.md
└── README.md
```

建議套件（皆為免費開源）：

| 用途 | 套件 |
| --- | --- |
| TOML | `github.com/BurntSushi/toml` |
| CLI | `github.com/spf13/cobra` 或標準庫 `flag` |
| HTML 解析 | `github.com/PuerkitoBio/goquery` |
| 正文擷取 | `github.com/go-shiori/go-readability` |
| HTML 轉 Markdown | `github.com/JohannesKaufmann/html-to-markdown` |
| RSS / Atom | `github.com/mmcdole/gofeed` |
| robots.txt | `github.com/temoto/robotstxt` |
| 限速 | `golang.org/x/time/rate` |
| 語言偵測 | `github.com/pemistahl/lingua-go` |
| 繁簡轉換 | OpenCC 的 Go 移植版（實作前確認維護狀態） |
| Unicode 正規化 | `golang.org/x/text/unicode/norm` |
| SQLite | `modernc.org/sqlite` |
| JS 渲染 | `github.com/chromedp/chromedp` |
| 圖檔尺寸 | 標準庫 `image` 加 `golang.org/x/image/webp` |

爬蟲本體建議自寫（佇列加限速器），不用 colly：邊界、條件請求與資料庫去重都要客製，自寫反而清楚。

## CLI、排程與錯誤處理

程式只有一個執行檔 `gleaner.exe`，排程由 Windows 工作排程器負責，程式本身不常駐。

| 指令 | 用途 |
| --- | --- |
| `gleaner run -c config.toml` | 執行一次收集 |
| `gleaner run --dry-run` | 照常抓取與比對，但不寫入歸檔與資料庫，只輸出會存哪些 |
| `gleaner run --site example-jp` | 只跑指定網站，方便調整單站設定 |
| `gleaner validate -c config.toml` | 只檢查 config 與關鍵字表達式語法 |
| `gleaner test-query "<表達式>" <檔案或網址>` | 對單一頁面測試命中結果 |
| `gleaner stats` | 列出最近幾次執行的摘要 |

結束代碼：0 = 成功（含部分頁面失敗）、1 = config 錯誤、2 = 執行中斷（例如資料庫無法開啟）。

排程：工作排程器設定每日固定時間執行 `run`，起始位置設為程式目錄。同一時間只允許一個執行個體，以資料目錄內的鎖定檔防止重疊。

錯誤處理：

- 單一頁面失敗（逾時、4xx、5xx、解析錯誤）只記錄並繼續；5xx 與逾時依 `retries` 重試，間隔遞增。
- 收到 429 或 503 時，該網站暫停到本次執行結束，避免被封鎖。
- 中途被中止（Ctrl+C、關機）時，已完成的歸檔與資料庫記錄保留，log 標記為 aborted。

爬蟲禮儀：遵守 robots.txt、固定的 User-Agent、每站限速、預設不跟進外部連結、不嘗試繞過登入或 CAPTCHA。歸檔僅供個人閱讀。

## 開發階段與驗收條件

分四個階段交付，每階段結束時都能實際執行並產出歸檔；下一階段開始前先在本機以真實網站實測。

### 階段 1：MVP（RSS 加關鍵字加歸檔）

- [x] config 載入、預設值、驗證；`validate` 指令可用
- [x] 布林表達式解析與求值，含 NOT 與括號；`test-query` 可用
- [x] feeds 來源與全文抓取、正文擷取、Markdown 存檔含 front matter
- [x] 網址正規化、內容指紋、SQLite 去重
- [x] 本次 log 與 F8 摘要

驗收：同一份 config 連續執行兩次，第二次有效筆數為 0；關鍵字解析器單元測試覆蓋 AND、OR、NOT、括號、引號詞組與語法錯誤。

### 階段 2：指定網站爬行與語言

- [ ] sites 來源：sitemap、站內連結、深度與頁數上限、路徑規則、時間窗
- [ ] robots.txt、每站限速、重試、429 / 503 處理
- [ ] ETag / Last-Modified 條件請求、`recheck_days`
- [ ] 三層語言判定、繁簡互通比對
- [ ] 更新判定與 `keep_versions` / `overwrite`
- [ ] `--dry-run`、`--site`、`stats`

驗收：以 `testdata/` 的離線網站測試邊界不越界；內容改變的頁面產生 v2；日文頁面不會被歸入只收 zh-TW 的 topic；log 正確標示「範圍內完成」或「達到上限」。

### 階段 3：圖片

- [ ] 正文區塊內圖片篩選與下載、尺寸過濾、雜湊去重
- [ ] front matter 與 `archive_images` 關聯

驗收：測試頁的 logo 與追蹤像素不被下載；同一張圖出現在兩篇文章只存一份。

### 階段 4：閱讀與選配功能

- [ ] FTS5 全文搜尋指令 `gleaner search <詞>`（中日文使用 `trigram` 分詞器）
- [ ] 產生 `archive/index.html` 依日期與 topic 瀏覽
- [ ] chromedp 渲染（`render_js`）
- [ ] google_news 來源

驗收：搜尋中日文詞彙都能找到對應歸檔；index.html 可用瀏覽器直接開啟。
