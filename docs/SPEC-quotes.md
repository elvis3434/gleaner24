# quotes 開發規格

台灣股票、期貨、選擇權行情與籌碼收集工具。規格日期：2026-10-24。

## 目標與範圍

`quotes` 是與 gleaner 同一個 repo 的第二個執行檔（`cmd/quotes`），依 config 指定的商品，從證交所、櫃買中心、期交所的免費公開資料收集每日行情與籌碼，存入 SQLite。

已確定的決策（使用者 2026-10-24 確認）：

| 項目 | 決定 |
| --- | --- |
| 位置 | 與 gleaner 同 repo，獨立執行檔 `quotes.exe`、獨立 config 與資料庫 |
| 商品分類 | 股票、期貨、選擇權各自獨立的資料表與解析套件 |
| 收集範圍 | config 指定商品，可同時多個；不收全市場 |
| 夜盤 | 要收 |
| 籌碼 | 要收（三大法人、融資融券、大額交易人、Put/Call 比） |
| 輸出 | 只存 SQLite |
| 付費 API | 不使用；只用交易所公開的免費資料 |
| K 線 | 只做日 K，不做盤中；產生可離線開啟的網頁（2026-10-24 確認） |
| 期貨連續月 | 近月、次近月兩條；結算日當天仍算到期月份，次一交易日換月 |
| 夜盤 K 線 | 日盤 / 全日盤（前一晚夜盤 + 當日日盤）可切換，預設日盤 |
| 除權息還原 | 需要；資料來源與籌碼同樣尚無官方回應實例，併入階段 Q2 |

不在範圍內：盤中即時報價、分鐘 K、全市場收集、技術指標計算、下單。

與 gleaner 共用：`internal/crawler`（禮貌抓取、限速、重試）、`internal/runlog`（log 檔）。其餘不共用。

## 資料來源

| 資料 | 來源與端點 | 一次請求的範圍 | 格式依據 |
| --- | --- | --- | --- |
| 上市個股日成交 | 證交所 `exchangeReport/STOCK_DAY?response=json&date=YYYYMM01&stockNo=<代號>` | 一檔一個月 | 官方回應實例（2026-10-06 取得，見 testdata） |
| 上櫃個股日成交 | 櫃買 `www/zh-tw/afterTrading/tradingStock?date=YYYY/MM/01&code=<代號>&response=json` | 一檔一個月 | 官方回應實例（同上） |
| 期貨每日交易行情 | 期交所 `cht/3/futDataDown`（POST，`down_type=1`、`commodity_id=<商品>`、起訖日） | 一個商品最多一個月 | 官方 CSV 實例（同上） |
| 選擇權每日交易行情 | 期交所 `cht/3/optDataDown`（同上） | 一個商品最多一個月 | 官方 CSV 實例（同上） |
| 籌碼（階段 Q2） | 證交所 `fund/T86`、`marginTrading/MI_MARGN`；櫃買 `insti/dailyTrade`、`margin/balance`；期交所 `futContractsDateDown`、`callsAndPutsDateDown`、`largeTraderFutDown`、`largeTraderOptDown`、`pcRatioDown` | 每日一次 | 開源套件的欄位位置，尚無官方回應實例，需實測 |

格式依據說明：開發用的雲端環境無法連到交易所網站。Q1 的四個來源都有官方回應實例（取自 MIT 授權的開源套件 twmarket 的測試資料，內含原始網址、SHA-256 與取得時間，複製於 `testdata/quotes/`）；Q2 的來源只有其他開源套件解析時用的欄位位置，必須在本機實測後才能確認。

### 夜盤的日期歸屬

期交所的資料中，`交易時段 = 盤後` 的列與同日 `一般` 的列有相同的交易日期，兩者的漲跌都以前一交易日結算價為基準（例：2026/10/05 TX 202610，一般 49949−1280 = 盤後 49346−677 = 48669）。也就是說，日期 T 的盤後資料是 T 的前一個營業日 15:00 開始、T 當天清晨結束的夜盤，歸屬於交易日 T。本工具照期交所的交易日期存放，`session` 欄分為 `regular`（一般）與 `after_hours`（盤後）。

## Config 規格

獨立的 TOML 檔（預設 `quotes.toml`），範例檔為 `quotes.example.toml`。

```toml
[output]
db      = "./data/quotes.db"
log_dir = "./logs"               # logs/quotes_YYYYMMDD_HHMM.log

[crawl]
user_agent     = "Gleaner/1.0 (personal archive)"
per_host_delay = "3s"            # 證交所對短時間大量請求會暫時封鎖，不建議調低
timeout        = "30s"
retries        = 2
lookback_days  = 5               # 每次執行重抓最近 N 個日曆日，補上漏抓或晚公布的資料

[stocks]
twse = ["2330", "2317"]          # 上市代號
tpex = ["6223"]                  # 上櫃代號

[[futures]]
product         = "TX"           # 期交所商品代號：TX 臺股期貨、MTX 小型臺指……
months          = 2              # 只收最近 N 個到期月份；0 = 全部
include_spreads = false          # 價差委託（202610/202611）
sessions        = ["regular", "after_hours"]

[chart]
dir  = "./charts"                # K 線網頁輸出目錄，開啟 charts/index.html
auto = true                      # 每次 quotes run 完成後自動重新產生

[[options]]
product      = "TXO"
expiries     = 2                 # 只收最近 N 個到期（依契約到期日排序）；0 = 全部
weekly       = true              # 是否包含週選擇權
strike_range = 0.05              # 只收價平上下 5% 的履約價；0 = 全部
sessions     = ["regular", "after_hours"]
```

- 至少要指定一種商品；代號只允許英數字。
- 價平的判定：同一交易日、同一到期，取一般時段買權與賣權結算價差距最小的履約價（買賣權平價關係），不需另外抓現貨指數。找不到時該到期全收。
- 每個 `[[futures]]`、`[[options]]` 的 `product` 不可重複。
- 期貨的 `months` 只計算月契約（`YYYYMM`）；週契約等非月契約不收，避免擠掉月契約，也不會進入連續月 K 線。

## 處理流程

1. 載入並驗證 config，有錯列出欄位後以代碼 1 結束。
2. 決定日期區間：預設 `[今天 − lookback_days, 今天]`（Asia/Taipei）；`--from`、`--to` 可指定歷史區間（補資料）。
3. 股票：每檔、每個涵蓋到的月份一次請求，整月資料全部寫入。
4. 期貨、選擇權：每個商品、每個涵蓋到的月份一次請求（期交所限制單次最多一個月），依 config 篩選月份、時段、履約價後寫入。
5. 寫入以主鍵 upsert；與既有資料逐列比對，分為新增、更新（同主鍵但數值改變，例如盤中重抓後的修正）、未變。
6. 寫 log 與摘要，摘要也存入 `runs` 表。

單一請求失敗只記錄並繼續；格式不符（欄位缺少、列長度不對）時記錄錯誤並把原始回應存到 `logs/raw/`（dry run 也會存，這是診斷用，不是資料），方便回報修正，不寫入任何半解析的資料。

某個項目整段期間每次請求都回「無資料」時，摘要列出警告：通常是代號錯誤，或上市股票放到 `tpex`（反之亦然），而不是真的沒有交易。

## 資料庫結構

單一 SQLite 檔（`data/quotes.db`），`modernc.org/sqlite`。日期欄為 `YYYY-MM-DD`，時間欄為 RFC 3339（+08:00）。數值欄缺值（`-`、`--`、`NULL`）存 NULL。

```sql
CREATE TABLE stock_daily (
  date         TEXT NOT NULL,
  symbol       TEXT NOT NULL,
  market       TEXT NOT NULL,      -- twse | tpex
  volume       INTEGER,            -- 股（櫃買的「張」已換算為股）
  turnover     INTEGER,            -- 元（櫃買的「仟元」已換算為元）
  open REAL, high REAL, low REAL, close REAL,
  change       REAL,               -- 漲跌價差；證交所標示 X（不比價）時為 NULL
  transactions INTEGER,            -- 成交筆數
  note         TEXT,
  row_hash     TEXT NOT NULL,
  fetched_at   TEXT NOT NULL,
  PRIMARY KEY (date, symbol)
);

CREATE TABLE futures_daily (
  date           TEXT NOT NULL,
  product        TEXT NOT NULL,
  contract_month TEXT NOT NULL,    -- 202610、202610W1、202610/202611（價差）
  session        TEXT NOT NULL,    -- regular | after_hours
  open REAL, high REAL, low REAL, close REAL, change REAL,
  volume         INTEGER,
  settlement     REAL,             -- 盤後時段無結算價
  open_interest  INTEGER,          -- 盤後時段無未平倉
  best_bid REAL, best_ask REAL,
  row_hash       TEXT NOT NULL,
  fetched_at     TEXT NOT NULL,
  PRIMARY KEY (date, product, contract_month, session)
);

CREATE TABLE options_daily (
  date           TEXT NOT NULL,
  product        TEXT NOT NULL,
  contract_month TEXT NOT NULL,
  strike         REAL NOT NULL,
  call_put       TEXT NOT NULL,    -- call | put
  session        TEXT NOT NULL,
  expiry_date    TEXT,             -- 契約到期日
  open REAL, high REAL, low REAL, close REAL, change REAL,
  volume         INTEGER,
  settlement     REAL,
  open_interest  INTEGER,
  best_bid REAL, best_ask REAL,
  row_hash       TEXT NOT NULL,
  fetched_at     TEXT NOT NULL,
  PRIMARY KEY (date, product, contract_month, strike, call_put, session)
);

CREATE TABLE runs (
  id INTEGER PRIMARY KEY, started_at TEXT NOT NULL, finished_at TEXT,
  end_reason TEXT, log_file TEXT, stats_json TEXT
);

-- 階段 Q3：股票名稱（K 線網頁顯示用；不計入 stock_daily 的變更比對）
CREATE TABLE stock_names (
  symbol     TEXT PRIMARY KEY,
  market     TEXT NOT NULL,
  name       TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

Q2 的籌碼資料表在 Q2 開始時再定案並以遷移加入。

## CLI

| 指令 | 用途 |
| --- | --- |
| `quotes run -c quotes.toml` | 收集最近 `lookback_days` 天 |
| `quotes run --from 2026-09-01 --to 2026-09-30` | 補指定區間 |
| `quotes run --dry-run` | 照常抓取與解析，只輸出筆數，不寫資料庫 |
| `quotes validate -c quotes.toml` | 只檢查 config |
| `quotes chart -c quotes.toml` | 依資料庫重新產生 K 線網頁（`chart.auto = true` 時 `run` 完成後也會自動產生） |

結束代碼與 gleaner 相同：0 成功（含部分請求失敗）、1 config 錯誤、2 執行中斷。

排程：每個交易日 15:30 以後執行一次即可（日盤收盤後資料公布；當天的盤後資料已在前一晚的夜盤產生）。期交所期貨與選擇權的結算價、未平倉約在收盤後公布，若執行時尚未公布，下一次執行會在 `lookback_days` 內補上並記為「更新」。

## K 線網頁

`charts/index.html` 用瀏覽器直接開啟（`file://`），不需要網路也不需要伺服器。

### 檔案

```
charts/
├── index.html                  # 頁面與操作邏輯
├── lightweight-charts.js       # TradingView Lightweight Charts™ 5.2.1（Apache-2.0），內嵌於執行檔
├── LICENSE-lightweight-charts.txt
└── data/
    ├── catalog.js              # 可選的項目清單
    ├── stock_2330.js           # 每檔股票一個檔
    ├── fut_TX.js               # 每個期貨商品一個檔（近月、次近月連續）
    └── opt_TXO_202610W1.js     # 每個選擇權商品的每個到期一個檔（含所有履約價與買賣權）
```

資料檔以 `<script>` 載入（`file://` 下瀏覽器不允許 `fetch`），需要時才載入，不會因為歷史資料變多而讓首頁變慢。每次產生時刪除不再需要的舊資料檔。

### 內容

- 蠟燭圖加成交量，台灣慣例紅漲綠跌；滑鼠移動時顯示該日開高低收量；可縮放、拖曳。
- 股票：成交量以「張」顯示（資料庫存股數）；價格為原始成交價，除權息還原於 Q2 加入切換。
- 期貨：近月連續、次近月連續兩條。每個交易日以當天一般時段有資料的月契約由近到遠排序，第一個是近月、第二個是次近月；結算日當天到期契約仍在交易，所以仍算當月，次一交易日自動換到下個月。換月日在圖上標示新月份。
- 選擇權：選商品、到期、履約價、買權 / 賣權，看單一合約；沒有成交的日子沒有 K 棒。
- 期貨與選擇權可切換「日盤」與「全日盤」。全日盤 = 同一交易日的盤後（前一晚夜盤）加一般時段：開盤取夜盤開盤（夜盤無成交時取日盤）、最高最低取兩者極值、收盤取日盤收盤、成交量相加。只有一般時段有資料的交易日才畫。
- 頁尾標示 TradingView 為圖表元件作者並附連結，圖上顯示 TradingView 標誌（授權要求）。
- 支援淺色與深色模式；選擇的項目記在網址 `#` 後，重新整理不會跑掉。

## 開發階段與驗收條件

### 階段 Q1：每日行情

- [x] config 載入、驗證；`validate` 指令
- [x] 上市、上櫃個股日成交（含單位換算、民國日期、漲跌符號）
- [x] 期貨每日行情（月份、價差、時段篩選）
- [x] 選擇權每日行情（到期、週選、價平範圍、時段篩選）
- [x] SQLite upsert 與新增 / 更新 / 未變統計；log 與 `runs` 摘要
- [x] `--from`、`--to`、`--dry-run`；格式不符時保存原始回應

驗收：以官方回應實例離線測試解析結果正確；同一區間執行兩次，第二次新增與更新皆為 0；夜盤資料以 `after_hours` 存入且不覆蓋一般時段。

### 階段 Q2：籌碼

- [ ] 個股三大法人買賣超（上市 T86、上櫃 dailyTrade）
- [ ] 個股融資融券（上市 MI_MARGN、上櫃 margin/balance）
- [ ] 期貨、選擇權三大法人（交易口數與未平倉）
- [ ] 期貨、選擇權大額交易人未沖銷部位
- [ ] 臺指選擇權 Put/Call 比

- [ ] 除權除息資料（上市、上櫃），K 線網頁加入「原始 / 還原」切換

驗收：先由使用者在本機取得各端點的實際回應放入 testdata，再據以完成解析與測試。

### 階段 Q3：K 線網頁

- [ ] `quotes chart` 指令與 `run` 後自動產生；`[chart]` 設定
- [ ] 股票日 K（含名稱、成交量換算為張）
- [ ] 期貨近月 / 次近月連續 K 線（結算日算當月）、換月標示
- [ ] 選擇權單一合約日 K
- [ ] 日盤 / 全日盤切換
- [ ] 離線開啟、授權標示、淺色 / 深色

驗收：以測試資料產生的網頁在瀏覽器以 `file://` 開啟無錯誤且畫出 K 線；連續月在結算日仍為到期月份、次一交易日換月；全日盤的開高低收量依上述規則合併。
