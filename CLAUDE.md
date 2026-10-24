# CLAUDE.md

本 repo 有兩個 Go CLI 工具：

- gleaner：依 config 設定，從指定網站與 RSS 收集符合關鍵字的內容並歸檔成 Markdown。規格見 `docs/SPEC.md`。
- quotes：依 config 指定的商品，收集台灣股票、期貨、選擇權的每日行情與籌碼存入 SQLite。規格見 `docs/SPEC-quotes.md`。

## 規則

- 規格以 docs/SPEC.md 為準；與規格衝突時先提出，不自行改變設計。
- Go module 為 `github.com/elvis3434/gleaner24`，執行檔為 `gleaner`（`cmd/gleaner`）與 `quotes`（`cmd/quotes`）。兩者只共用 `internal/crawler`、`internal/runlog` 與 `internal/lock`（防止同時執行的檔案鎖）；行情專用的程式放在 `internal/market/`。
- Config 使用 TOML（`github.com/BurntSushi/toml`），範例檔為 `config.example.toml` 與 `quotes.example.toml`。
- Go 1.26 以上（使用者決定採用各依賴的最新版，最新版 `modernc.org/sqlite` 與 `golang.org/x/*` 需要 Go 1.26）；只用純 Go 套件（不得依賴 cgo），必須能交叉編譯 `GOOS=windows GOARCH=amd64`。
- 一次只實作 SPEC 的一個階段，完成後更新 docs/SPEC.md 中該階段的勾選清單。
- 所有網路相關測試使用 testdata/ 與 httptest，不在測試中連外。
- 每個 internal/ 套件都要有單元測試；`go vet ./...` 與 `go test ./...` 必須通過。
- 路徑一律用 filepath 處理，檔名避開 Windows 保留字元與保留名稱（CON、NUL 等）。
- 時間一律帶時區（Asia/Taipei），輸出 RFC 3339。
- 不加入任何需要 API key 或付費的服務。
- 依賴套件與資安追蹤見 `docs/DEPENDENCIES.md`：新增或升級依賴後更新該文件的清單；新版發布滿 14 天才採用，安全修正例外；`go.mod` 的 `toolchain` 指定已修補的 Go 版本。分析工具（govulncheck、staticcheck）放在獨立的 `tools/go.mod`。
- 提交前執行 `make check`（vet、test、staticcheck、govulncheck、Windows 交叉編譯）；雲端無法連到 vuln.go.dev 時用離線資料庫（`VULNDB=file:///...`，可由 `golang.org/x/vulndb` 模組以其 `cmd/gendb` 產生）。
- 使用者以繁體中文溝通；程式碼註解與 commit 訊息可用英文。

## 開發環境注意事項

- 雲端環境的網路存取可能受限（證交所、櫃買、期交所目前被擋），開發與測試以離線的 `testdata/` 為主；對真實網站的實測由使用者在本機 Windows 執行 `gleaner run --dry-run` 或 `quotes run --dry-run`。
- K 線網頁的前端是 `internal/market/chart/assets/index.html`（純 HTML + JS，無建置步驟），圖表元件為內嵌的 TradingView Lightweight Charts 5.2.1（Apache-2.0）；頁面必須保留 TradingView 標示與連結（授權要求）。改前端後用 Playwright 在 Chromium 以 `file://` 開啟檢查 console 無錯誤。
- 行情解析只能以官方回應實例為依據，不可自行編造回應格式；沒有實例的端點先請使用者在本機取得。`testdata/quotes/README.md` 記錄每個檔案的來源。
- 每個階段完成後，提供 Windows 版 `gleaner.exe` 的建置方式（`make windows` 或對應的 `go build` 指令），並更新 `config.example.toml`。

## 目前進度

- [x] 階段 1：MVP（RSS 加關鍵字加歸檔）— 實作與離線測試完成，待使用者本機以真實網站實測
- [ ] 階段 2：指定網站爬行與語言
- [ ] 階段 3：圖片
- [ ] 階段 4：閱讀與選配功能

quotes：

- [x] 階段 Q1：每日行情（股票、期貨、選擇權含夜盤）— 以官方回應實例離線測試完成，待使用者本機實測
- [ ] 階段 Q2：籌碼（三大法人、融資融券、大額交易人、Put/Call 比）與除權息還原 — 需先在本機取得各端點實際回應
- [x] 階段 Q3：K 線網頁（日 K；期貨近月 / 次近月連續、選擇權單一合約、日盤 / 全日盤）— 已在 Chromium 以 file:// 驗證
