# gleaner24

gleaner 是一個 Go 撰寫的命令列工具：依 `config.toml` 設定，從指定網站與 RSS 收集符合關鍵字條件（AND / OR / NOT）的內容，存成附有來源網址與收集日期的 Markdown 歸檔，並在每次執行後寫入收集 log。

- 規格：[docs/SPEC.md](docs/SPEC.md)
- 開發狀態：規格完成，準備進入階段 1（MVP）
