# gleaner24

gleaner 是一個 Go 撰寫的命令列工具：依 `config.toml` 設定，從指定網站與 RSS 收集符合關鍵字條件（AND / OR / NOT）的內容，存成附有來源網址與收集日期的 Markdown 歸檔，並在每次執行後寫入收集 log。

- 規格：[docs/SPEC.md](docs/SPEC.md)
- 開發狀態：階段 1（RSS / Atom + 關鍵字 + 歸檔）完成；sites 爬行、語言判定、圖片在後續階段。

## 建置

需要 Go 1.26 以上，不需要 C 編譯器。

```sh
# 在 Windows 上直接建置
go build -o gleaner.exe ./cmd/gleaner

# 在 Linux / macOS 交叉編譯 Windows 版
make windows        # 等同 CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o gleaner.exe ./cmd/gleaner

make test           # go test ./...
make vet            # go vet ./...
```

## 使用

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
