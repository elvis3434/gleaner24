# 依賴套件與資安檢查

本文件列出 gleaner 與 quotes 用到的外部套件，以及如何檢查它們的安全漏洞、如何升級。最後檢核：2026-11-02。

## 怎麼檢查漏洞（最常用）

兩個程式都內建漏洞檢查，需要時自己執行即可，不需要原始碼、也不需要安裝 Go：

```
gleaner vulncheck
quotes vulncheck
```

- 檢查的是「這個執行檔裡實際編入的套件與 Go 標準函式庫」，並且只回報程式真的會呼叫到的漏洞函式。
- 資料來源是 Go 官方漏洞資料庫 https://vuln.go.dev（免費、不需帳號），需要網路。
- 結果：最後一行會寫「未發現已知漏洞」或「發現已知漏洞」；發現時，報告中的 `Fixed in` 是要升級到的版本。
- 結束代碼：0 未發現、3 發現漏洞、2 無法檢查（例如沒有網路）、1 參數錯誤。
- 選項：`-v` 也列出有編入但沒被呼叫到的漏洞；`-db <網址或資料夾>` 改用其他資料庫（例如離線副本）。

建議頻率：每月一次，以及每次重新建置 `.exe` 之後。也可以用 Windows 工作排程器設定每月執行，並把輸出寫到檔案（例如 `quotes vulncheck > logs\vulncheck.txt`）。

發現漏洞時：依下方「升級步驟」升級對應套件，重新建置後再執行一次 `vulncheck` 確認。

## 依賴清單

### 編進執行檔的 Go 套件（`go.mod`）

| 套件 | 版本 | 用途 | 授權 |
| --- | --- | --- | --- |
| `github.com/BurntSushi/toml` | v1.6.0 | 讀取 TOML 設定 | MIT |
| `github.com/mmcdole/gofeed` | v1.5.0 | RSS / Atom 解析（gleaner） | MIT |
| `github.com/go-shiori/go-readability` | 2025-12-05 版（無正式版號） | 正文擷取（gleaner） | MIT |
| `github.com/JohannesKaufmann/html-to-markdown/v2` | v2.5.2 | HTML 轉 Markdown（gleaner） | MIT |
| `golang.org/x/net` | v0.62.0 | HTML 解析、字元集偵測 | BSD-3-Clause |
| `golang.org/x/text` | v0.43.0 | Unicode 正規化、Big5 解碼 | BSD-3-Clause |
| `golang.org/x/sys` | v0.49.0 | Windows 檔案鎖 | BSD-3-Clause |
| `golang.org/x/vuln` | v1.9.0 | `vulncheck` 指令（官方 govulncheck，執行檔約增加 5.5 MB） | BSD-3-Clause |
| `modernc.org/sqlite` | v1.60.1 | 純 Go 的 SQLite（不需 cgo） | BSD-3-Clause |

間接依賴由 `go.mod` 的第二個 `require` 區塊與 `go.sum` 記錄，`go.sum` 保證每次下載的內容與第一次相同。`golang.org/x/vuln` 使用的 Go telemetry 套件在本程式中不會啟動，不會在電腦上寫入任何統計檔。

### Go 工具鏈

`go.mod` 的 `toolchain go1.26.10` 指定建置用的 Go 版本。本機安裝的 Go 比這個舊時，`go build` 會自動下載 go1.26.10（經 Go 官方 checksum 資料庫驗證）再建置，所以執行檔一定帶著已修補的標準函式庫。`vulncheck` 也會檢查標準函式庫，Go 發布新的修補版時會在這裡反映出來。

### 開發用工具（`tools/go.mod`，不會編進執行檔）

| 工具 | 版本 | 用途 |
| --- | --- | --- |
| `honnef.co/go/tools/cmd/staticcheck` | v0.8.2 | 靜態分析，找出 `go vet` 抓不到的錯誤 |

開發時要對原始碼做的漏洞檢查（`make vulncheck`）使用與程式內建相同版本的 govulncheck（`go.mod` 的 `golang.org/x/vuln`）。

### K 線網頁的前端元件（`internal/market/chart/assets/`）

| 套件 | 版本 | 用途 | 授權 |
| --- | --- | --- | --- |
| `lightweight-charts`（TradingView） | 5.2.1 | K 線圖 | Apache-2.0，需標示 TradingView 並附連結 |
| `fancy-canvas`（隨上者打包） | 2.1.0 | 畫布工具 | MIT |

這個 JS 檔是「複製進 repo」的，`vulncheck` 不會檢查它。它只在本機離線開啟的 K 線網頁裡畫圖，資料也是本機產生的，風險很低；升級時用 `package.json`、`package-lock.json` 記錄版本，授權全文在 `LICENSE-lightweight-charts.txt`。需要檢查時，在該目錄執行 `npm audit --package-lock-only`（需安裝 Node.js），推送程式碼時 CI 也會執行。

### GitHub Actions

`actions/checkout` v7.0.1、`actions/setup-go` v7.0.0，在 `.github/workflows/ci.yml` 以 commit SHA 固定，避免標籤被改指向其他程式碼。

## 自動化的範圍

依使用者決定（2026-11-02），不設定任何定期自動執行：

- 沒有每週排程的 CI，也沒有 Dependabot 的定期版本更新。
- CI（`.github/workflows/ci.yml`）只在推送程式碼時執行，或在 GitHub 的 Actions 分頁手動按「Run workflow」。內容：`go vet`、`go test`、staticcheck、`go mod tidy` 檢查、Windows 交叉編譯、govulncheck、npm audit。
- 若想在漏洞公布時收到通知但不產生 PR，可在 GitHub 的 Settings → Code security 只開啟 **Dependabot alerts**（被動通知，不會執行任何工作）。

## 版本政策

- 一般新版：發布滿 14 天才採用，讓有問題或被入侵的版本先被其他人發現。
- 安全修正：不等待，立即升級。Go 工具鏈的安全修補版也一樣。
- 新增依賴：只能用純 Go 套件（不得依賴 cgo）、不需 API key、授權允許散布；加入後更新本文件的清單。

## 升級步驟

### 升級 Go 套件

```sh
go get example.com/pkg@v1.2.3   # 指定 vulncheck 報告中 Fixed in 的版本
go mod tidy
make check                      # vet、test、lint、vulncheck、Windows 交叉編譯
```

注意：`go get` 可能連帶調整其他套件的版本（包括降版）。升級後用 `git diff go.mod` 確認變動都在預期內。沒有 make 時，依序執行 `go vet ./...`、`go test ./...`，再重新建置並執行 `gleaner vulncheck`、`quotes vulncheck`。

### 升級 Go 工具鏈

`vulncheck` 回報標準函式庫（`stdlib`）的漏洞時：

```sh
go mod edit -toolchain=go1.26.N
go mod edit -C tools -toolchain=go1.26.N
make check
```

### 升級 lightweight-charts

1. 修改 `internal/market/chart/assets/package.json` 的版本。
2. 執行 `make update-lwc`（需要 Node.js）：下載並驗證檔案、更新授權說明與 `package-lock.json`。
3. `go test ./...`（`TestVendoredChartLibraryMatchesManifest` 會確認版本一致），再用瀏覽器開 `charts/index.html` 確認圖表正常。

### 開發用檢查指令

| 目的 | make | 沒有 make（Windows） |
| --- | --- | --- |
| 建置開發工具 | `make tools` | `go build -C tools -o ../bin/ honnef.co/go/tools/cmd/staticcheck` 與 `go build -o bin/ golang.org/x/vuln/cmd/govulncheck` |
| 原始碼漏洞檢查 | `make vulncheck` | `bin\govulncheck ./...` |
| 靜態分析 | `make lint` | `bin\staticcheck ./...` |
| 列出可升級的套件 | `make outdated` | `go list -m -u all` |
| 全部檢查 | `make check` | 依序執行 `go vet ./...`、`go test ./...` 與上面三項 |

無法連線到 `vuln.go.dev` 時（例如開發用的雲端環境），可以用離線資料庫：`make vulncheck VULNDB=file:///path/to/db`、`gleaner vulncheck -db C:\path\to\db`。

## 處理紀錄

| 日期 | 來源 | 內容 | 處理 |
| --- | --- | --- | --- |
| 2026-10-24 | 首次檢核 | go1.26.0 有 28 個程式會呼叫到的標準函式庫漏洞 | 指定 `toolchain go1.26.10` |
| 2026-11-02 | 每週 CI 的 govulncheck 失敗（寄信通知） | `golang.org/x/net` v0.61.0 的 HTML 解析器有 GO-2026-6866、GO-2026-6860，gleaner 擷取正文時會呼叫到 | 升級到 v0.62.0（安全修正，不等 14 天） |
| 2026-11-02 | 使用者決定 | 改為程式內建 `vulncheck` 指令、需要時手動執行；取消每週 CI 排程與 Dependabot | 新增 `internal/selfcheck`，`golang.org/x/vuln` v1.9.0 編入執行檔；staticcheck 升級到 v0.8.2 |

## 2026-11-02 檢核結果

- `gleaner vulncheck`、`quotes vulncheck`（漏洞資料庫 2026-10-27 版的離線副本，含 2026-11-02 公布的兩筆 x/net 漏洞）：**未發現已知漏洞**。同一份資料庫對升級前（x/net v0.61.0）的 gleaner 正確回報 GO-2026-6866、GO-2026-6860。
- `npm audit`：lightweight-charts 5.2.1 與 fancy-canvas 2.1.0 **無已知漏洞**。
- 有新版但暫不升級：`modernc.org/sqlite` v1.61.0（2026-10-21 發布，11-04 滿 14 天；目前版本無已知漏洞）。
