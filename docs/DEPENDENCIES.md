# 依賴套件與資安追蹤

本文件列出 gleaner 與 quotes 用到的外部套件，以及如何追蹤它們的安全漏洞、如何升級。最後檢核：2026-10-24。

## 依賴清單

### 編進執行檔的 Go 套件（`go.mod`）

| 套件 | 版本 | 用途 | 授權 |
| --- | --- | --- | --- |
| `github.com/BurntSushi/toml` | v1.6.0 | 讀取 TOML 設定 | MIT |
| `github.com/mmcdole/gofeed` | v1.5.0 | RSS / Atom 解析（gleaner） | MIT |
| `github.com/go-shiori/go-readability` | 2025-12-05 版（無正式版號） | 正文擷取（gleaner） | MIT |
| `github.com/JohannesKaufmann/html-to-markdown/v2` | v2.5.2 | HTML 轉 Markdown（gleaner） | MIT |
| `golang.org/x/net` | v0.61.0 | HTML 解析、字元集偵測 | BSD-3-Clause |
| `golang.org/x/text` | v0.43.0 | Unicode 正規化、Big5 解碼 | BSD-3-Clause |
| `golang.org/x/sys` | v0.49.0 | Windows 檔案鎖 | BSD-3-Clause |
| `modernc.org/sqlite` | v1.60.1 | 純 Go 的 SQLite（不需 cgo） | BSD-3-Clause |

間接依賴由 `go.mod` 的第二個 `require` 區塊與 `go.sum` 記錄，`go.sum` 保證每次下載的內容與第一次相同。

### Go 工具鏈

`go.mod` 的 `toolchain go1.26.10` 指定建置用的 Go 版本。本機安裝的 Go 比這個舊時，`go build` 會自動下載 go1.26.10（經 Go 官方 checksum 資料庫驗證）再建置，所以執行檔一定帶著已修補的標準函式庫。

為什麼要指定：2026-10-24 檢核時，用 go1.26.0 建置的程式有 47 個標準函式庫已知漏洞，其中 28 個是程式實際會呼叫到的（TLS、憑證驗證、網址解析等，與連線到交易所與新聞網站直接相關）；改用 go1.26.10 後為 0。

### 分析工具（`tools/go.mod`，不會編進執行檔）

| 工具 | 版本 | 用途 |
| --- | --- | --- |
| `golang.org/x/vuln/cmd/govulncheck` | v1.8.0 | 依 Go 官方漏洞資料庫，檢查程式實際呼叫到的已知漏洞 |
| `honnef.co/go/tools/cmd/staticcheck` | v0.8.1 | 靜態分析，找出 `go vet` 抓不到的錯誤 |

放在獨立的 `tools/` 模組，工具本身的依賴不會影響 `gleaner.exe`、`quotes.exe` 用到的套件版本。

### K 線網頁的前端元件（`internal/market/chart/assets/`）

| 套件 | 版本 | 用途 | 授權 |
| --- | --- | --- | --- |
| `lightweight-charts`（TradingView） | 5.2.1 | K 線圖 | Apache-2.0，需標示 TradingView 並附連結 |
| `fancy-canvas`（隨上者打包） | 2.1.0 | 畫布工具 | MIT |

這個 JS 檔是「複製進 repo」而不是安裝的，所以另外放了 `package.json` 與 `package-lock.json` 讓 Dependabot 追蹤版本；授權全文在 `LICENSE-lightweight-charts.txt`。

### GitHub Actions

`actions/checkout` v7.0.1、`actions/setup-go` v7.0.0，在 `.github/workflows/ci.yml` 以 commit SHA 固定，避免標籤被改指向其他程式碼。

## 追蹤機制

| 機制 | 檢查什麼 | 何時 | 結果在哪 |
| --- | --- | --- | --- |
| Dependabot 版本更新（`.github/dependabot.yml`） | Go 套件、分析工具、lightweight-charts、GitHub Actions 的新版 | 每週一 08:00（Actions 每月） | 自動開 PR |
| Dependabot 安全性警示與安全更新 | 依賴套件的已知漏洞 | GitHub 收到漏洞公告時 | Security 分頁警示、自動開 PR |
| CI：`govulncheck` | 程式實際呼叫到的漏洞，含 Go 標準函式庫（工具鏈） | 每次 push，以及每週一 08:17 | Actions 失敗會寄通知信 |
| CI：`npm audit` | lightweight-charts 及其依賴的已知漏洞 | 同上 | 同上 |
| CI：`staticcheck`、`go vet`、`go test`、`go mod tidy -diff`、Windows 交叉編譯 | 升級後程式是否仍正確 | 每次 push | 同上 |
| 測試 `TestVendoredChartLibraryMatchesManifest` | 複製進來的 JS 檔與 `package.json` 宣告的版本一致 | 每次 `go test` | 不一致時測試失敗並提示 `make update-lwc` |

每週排程的意義：就算程式碼沒有變動，新公布的漏洞也會讓 CI 失敗並通知你。

### 需要在 GitHub 開啟的設定（一次性）

1. 把含 `.github/` 的分支合併到 `main`：Dependabot 設定與每週排程只在預設分支生效。
2. Settings → Code security：開啟 **Dependency graph**、**Dependabot alerts**、**Dependabot security updates**。
3. Settings → Actions → General：允許 Actions 執行。

## 版本政策

- 一般新版：發布滿 14 天才採用（Dependabot 的 cooldown 設定），讓有問題或被入侵的版本先被其他人發現。
- 安全修正：不等待，立即升級。Go 工具鏈的安全修補版也一樣（例如 go1.26.10 發布 5 天就採用，因為它修正了程式會用到的憑證驗證漏洞）。
- 新增依賴：只能用純 Go 套件（不得依賴 cgo）、不需 API key、授權允許散布；加入後更新本文件的清單。

## 升級步驟

### Dependabot 開的 PR

1. 看 CI 是否全綠。
2. lightweight-charts 的 PR 一定會先失敗（版本不一致的測試），在 PR 分支執行 `make update-lwc`，用瀏覽器開 `charts/index.html` 確認圖表正常後提交。
3. 合併。

### 手動升級 Go 套件

```sh
go get example.com/pkg@v1.2.3   # 指定版本
go mod tidy
make check                      # vet、test、lint、vulncheck、Windows 交叉編譯
```

注意：`go get` 可能連帶調整其他套件的版本（包括降版）。升級後用 `git diff go.mod` 確認變動都在預期內。

### 升級 Go 工具鏈

Go 每月會發布修補版。`govulncheck` 回報標準函式庫的漏洞時：

```sh
go mod edit -toolchain=go1.26.N
go mod edit -C tools -toolchain=go1.26.N
make check
```

### 本機檢查指令

| 目的 | make | 沒有 make（Windows） |
| --- | --- | --- |
| 建置分析工具 | `make tools` | `go build -C tools -o ../bin/ golang.org/x/vuln/cmd/govulncheck honnef.co/go/tools/cmd/staticcheck` |
| 漏洞檢查 | `make vulncheck` | `bin\govulncheck ./...` |
| 靜態分析 | `make lint` | `bin\staticcheck ./...` |
| 列出可升級的套件 | `make outdated` | `go list -m -u all` |
| 全部檢查 | `make check` | 依序執行 `go vet ./...`、`go test ./...` 與上面三項 |

無法連線到 `vuln.go.dev` 時（例如開發用的雲端環境），可以用離線資料庫：`make vulncheck VULNDB=file:///path/to/db`。

## 2026-10-24 檢核結果

- `govulncheck`（漏洞資料庫 2026-10-21）：go1.26.10 下 **無已知漏洞**；分析工具執行檔本身也無。
- `npm audit`：lightweight-charts 5.2.1 與 fancy-canvas 2.1.0 **無已知漏洞**。
- 有新版但暫不升級（未滿 14 天、且目前版本無已知漏洞）：`modernc.org/sqlite` v1.61.0（2026-10-21）、`govulncheck` v1.9.0（2026-10-11）、`staticcheck` v0.8.2（2026-10-12）。Dependabot 會在期滿後提出。
