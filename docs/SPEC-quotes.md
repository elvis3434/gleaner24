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

不在範圍內：盤中即時報價、全市場收集、技術指標計算、下單。

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

## 處理流程

1. 載入並驗證 config，有錯列出欄位後以代碼 1 結束。
2. 決定日期區間：預設 `[今天 − lookback_days, 今天]`（Asia/Taipei）；`--from`、`--to` 可指定歷史區間（補資料）。
3. 股票：每檔、每個涵蓋到的月份一次請求，整月資料全部寫入。
4. 期貨、選擇權：每個商品、每個涵蓋到的月份一次請求（期交所限制單次最多一個月），依 config 篩選月份、時段、履約價後寫入。
5. 寫入以主鍵 upsert；與既有資料逐列比對，分為新增、更新（同主鍵但數值改變，例如盤中重抓後的修正）、未變。
6. 寫 log 與摘要，摘要也存入 `runs` 表。

單一請求失敗只記錄並繼續；格式不符（欄位缺少、列長度不對）時記錄錯誤並把原始回應存到 `logs/raw/`，方便回報修正，不寫入任何半解析的資料。

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
```

Q2 的籌碼資料表在 Q2 開始時再定案並以遷移加入。

## CLI

| 指令 | 用途 |
| --- | --- |
| `quotes run -c quotes.toml` | 收集最近 `lookback_days` 天 |
| `quotes run --from 2026-09-01 --to 2026-09-30` | 補指定區間 |
| `quotes run --dry-run` | 照常抓取與解析，只輸出筆數，不寫資料庫 |
| `quotes validate -c quotes.toml` | 只檢查 config |

結束代碼與 gleaner 相同：0 成功（含部分請求失敗）、1 config 錯誤、2 執行中斷。

排程：每個交易日 15:30 以後執行一次即可（日盤收盤後資料公布；當天的盤後資料已在前一晚的夜盤產生）。期交所期貨與選擇權的結算價、未平倉約在收盤後公布，若執行時尚未公布，下一次執行會在 `lookback_days` 內補上並記為「更新」。

## 開發階段與驗收條件

### 階段 Q1：每日行情

- [ ] config 載入、驗證；`validate` 指令
- [ ] 上市、上櫃個股日成交（含單位換算、民國日期、漲跌符號）
- [ ] 期貨每日行情（月份、價差、時段篩選）
- [ ] 選擇權每日行情（到期、週選、價平範圍、時段篩選）
- [ ] SQLite upsert 與新增 / 更新 / 未變統計；log 與 `runs` 摘要
- [ ] `--from`、`--to`、`--dry-run`；格式不符時保存原始回應

驗收：以官方回應實例離線測試解析結果正確；同一區間執行兩次，第二次新增與更新皆為 0；夜盤資料以 `after_hours` 存入且不覆蓋一般時段。

### 階段 Q2：籌碼

- [ ] 個股三大法人買賣超（上市 T86、上櫃 dailyTrade）
- [ ] 個股融資融券（上市 MI_MARGN、上櫃 margin/balance）
- [ ] 期貨、選擇權三大法人（交易口數與未平倉）
- [ ] 期貨、選擇權大額交易人未沖銷部位
- [ ] 臺指選擇權 Put/Call 比

驗收：先由使用者在本機取得各端點的實際回應放入 testdata，再據以完成解析與測試。
