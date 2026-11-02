# 測試用的假漏洞資料庫

`internal/selfcheck` 的測試以 `-db file://...` 指向這裡，不連外。

- `affected/`：一筆**虛構**的漏洞 `GO-0000-0001`，標記 `github.com/BurntSushi/toml` 的 `Decode`（gleaner 讀設定檔會用到）在 v99.0.0 以前有漏洞，用來確認掃描能找到問題。不是真實的安全公告。
- `clean/`：沒有任何漏洞的資料庫。

格式與 Go 官方漏洞資料庫（https://vuln.go.dev）相同：`index/db.json`、`index/modules.json`、`ID/<id>.json`（OSV 1.3.1）。
