# quotes 測試資料來源

本目錄的檔案是交易所官方回應的原始內容，複製自 MIT 授權的開源套件
[twmarket](https://pypi.org/project/twmarket/) 0.1.0 的 `tests/fixtures/`，
取得日期 2026-10-06。內容、編碼（期交所 CSV 為 cp950、CRLF）與換行均未修改。

| 檔案 | 原檔名 | 來源 |
| --- | --- | --- |
| `twse_stock_day_2330_202609.json` | `twse.json` | `https://www.twse.com.tw/exchangeReport/STOCK_DAY?response=json&date=20260901&stockNo=2330`，SHA-256 `c556fb917676d81615b5c8e133c5f36dae52fba2efcfc8b9ab196c86ef1506ec` |
| `tpex_trading_stock_6223_202609.json` | `tpex.json` | `https://www.tpex.org.tw/www/zh-tw/afterTrading/tradingStock?date=2026%2F09%2F01&code=6223&response=json`，SHA-256 `b92097ee73f2e746a281466055eae2e1d359bf7348b10a316a4aa1d6dc9d72a4` |
| `taifex_fut_TX_20261005.csv` | `taifex-history.csv` | 期交所 `futDataDown`，2026/10/05，商品 TX（原套件只保留前段資料列） |
| `taifex_opt_TXO_20261005.csv` | `taifex-option-history.csv` | 期交所 `optDataDown`，2026/10/05，商品 TXO（原套件只保留前段資料列） |

前兩個檔案的 SHA-256 與原套件 `source-manifest.json` 記錄的完整回應相符。

到期、履約價篩選等邏輯的測試另以程式內組出的資料列進行（使用上述 CSV 的實際表頭），不在本目錄。

---

twmarket 授權條款：

MIT License

Copyright (c) 2026 Henrik (cmostw)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
