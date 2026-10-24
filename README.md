# gleaner24

本 repo 有兩個 Go 命令列工具：

- **gleaner**：依 `config.toml` 設定，從指定網站與 RSS 收集符合關鍵字條件（AND / OR / NOT）的內容，存成附有來源網址與收集日期的 Markdown 歸檔，並在每次執行後寫入收集 log。規格：[docs/SPEC.md](docs/SPEC.md)。階段 1（RSS / Atom + 關鍵字 + 歸檔）完成。
- **quotes**：依 `quotes.toml` 指定的商品，從證交所、櫃買中心、期交所的免費公開資料收集台股、期貨、選擇權（含夜盤）的每日行情，存入 SQLite，並產生可離線開啟的日 K 線網頁。規格：[docs/SPEC-quotes.md](docs/SPEC-quotes.md)。階段 Q1（每日行情）、Q3（K 線網頁）完成；籌碼與除權息還原為階段 Q2。

## 建置

需要 Go 1.26 以上，不需要 C 編譯器。

```sh
# 在 Windows 上直接建置
go build -o gleaner.exe ./cmd/gleaner
go build -o quotes.exe ./cmd/quotes

# 在 Linux / macOS 交叉編譯 Windows 版
make windows        # 產生 gleaner.exe 與 quotes.exe

make test           # go test ./...
make vet            # go vet ./...
```

## 使用 gleaner

```sh
copy config.example.toml config.toml      # 然後修改 feeds 與 topics
gleaner validate -c config.toml           # 檢查設定與關鍵字語法
gleaner run --dry-run -c config.toml      # 照常抓取與比對，但不寫入任何檔案
gleaner run -c config.toml                # 正式收集
gleaner test-query "台積電 AND 營收 NOT 傳聞" page.html   # 對單一檔案或網址測試命中
```

產出：

- `archive/YYYY/MM/DD/<topic>/<slug>.md`：歸檔，含 front matter
- `logs/collect_YYYYMMDD_HHMM.log`：每次執行的 log 與摘要
- `data/index.db`：SQLite 索引（去重、版本、執行紀錄）

結束代碼：0 成功（含部分頁面失敗）、1 設定錯誤、2 執行中斷。

## 使用 quotes

```sh
copy quotes.example.toml quotes.toml      # 然後修改股票代號與期貨、選擇權商品
quotes validate -c quotes.toml            # 檢查設定
quotes run --dry-run -c quotes.toml       # 照常抓取與解析，不寫入資料庫
quotes run -c quotes.toml                 # 收集最近 lookback_days 天（預設 5 天）
quotes run -c quotes.toml --from 2026-09-01 --to 2026-09-30   # 補一段歷史資料
quotes chart -c quotes.toml               # 依資料庫重新產生 K 線網頁（run 完成後也會自動產生）
```

- `charts/index.html`：日 K 線網頁，用瀏覽器直接開啟。股票、期貨近月 / 次近月連續、選擇權單一合約；期貨與選擇權可切換日盤 / 全日盤。圖表元件為 [TradingView](https://www.tradingview.com/) Lightweight Charts™（Apache-2.0）

- `data/quotes.db`：SQLite，資料表 `stock_daily`、`futures_daily`、`options_daily`、`runs`
- `logs/quotes_YYYYMMDD_HHMM.log`：每次執行的 log 與摘要
- `logs/raw/`：交易所回應格式不符時保存的原始回應，回報問題時請附上

排程建議：每個交易日 15:30 以後執行一次 `quotes run`。
