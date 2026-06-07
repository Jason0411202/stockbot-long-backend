# 專案總覽

`stockbot-long-backend` 是一個以 Go 撰寫的台股 ETF 長線與波段交易後端。它負責取得 TWSE 歷史價量、整理成策略可用的時間序列、執行交易引擎、保存投資組合狀態，並把資料提供給前端與監控系統。

## 系統責任

本專案主要處理下列工作：

- 回補與更新 `00631L`、`00830` 等追蹤標的的歷史 OHLCV 資料。
- 依 `config.yaml` 執行牛熊 regime 感知的買賣策略。
- 保存未實現持倉、已實現損益、bot 現金與每日處理 watermark。
- 提供 REST API 給前端查詢投資組合、歷史價格與統計資料。
- 提供 `/health`、`/ready`、`/metrics` 給部署與監控使用。
- 提供 CLI 工具重現回測、離線評估與資料抓取。

## 執行架構

正式服務入口是 [cmd/server/main.go](../cmd/server/main.go)。啟動流程如下：

1. 載入 `.env` 與 `config.yaml`。
2. 建立 MariaDB 連線池並初始化 schema。
3. 建立 TWSE、Discord、repository、service、controller。
4. 回補初始歷史資料或執行日常資料更新。
5. 啟動 Echo HTTP server。
6. 進入交易服務的每日處理迴圈。

## 套件分層

| 路徑 | 責任 |
| --- | --- |
| `cmd/server` | 正式 HTTP server 與線上交易 loop 入口 |
| `cmd/fetch_data` | 下載 TWSE 歷史資料到本機 CSV |
| `cmd/eval_csv` | 用本機 CSV 離線評估策略與回測 |
| `cmd/evaluate` | 使用 DB 資料執行 walk-forward 評估 |
| `cmd/research_run` | 執行單一長區間研究回測 |
| `cmd/db_probe` | 檢查 MariaDB schema 與資料筆數 |
| `internal/config` | 讀取 `config.yaml` 與 per-stock override |
| `internal/client` | 外部服務 client，例如 TWSE 與 Discord |
| `internal/repository` | MariaDB CRUD 與查詢 |
| `internal/service` | 商業邏輯、資料回補、投資組合、交易服務 |
| `internal/service/trading` | 純記憶體交易引擎與買賣決策 |
| `internal/service/backtest` | 回測、walk-forward、績效指標與 CSV 載入 |
| `internal/controller` | Echo handler 的業務入口 |
| `internal/server` | Echo middleware、routes、health、metrics |
| `internal/platform/mariadb` | DB 連線池與 schema 初始化 |

## 核心資料流

TWSE API 回傳的月資料會先轉成 `entity.Bar`，再寫入 `StockHistory`。交易與回測讀取 `StockHistory` 或 CSV 後，轉成 `trading.StockSeries`，由純記憶體引擎處理每日買賣。線上模式會透過 executor 將成交寫回 ledger 表，回測模式則使用 no-op executor，只收集權益曲線與交易統計。

```text
TWSE API / CSV
  -> StockHistory / StockSeries
  -> trading.Engine
  -> Portfolio ledger / Backtest metrics
  -> REST API / CLI report / Prometheus metrics
```

## 設定來源

不私密的策略與資料回補參數放在 `config.yaml`，可 commit。私密資訊放在 `.env`，例如 `DB_DSN`、`DISCORD_BOT_TOKEN`、`DISCORD_BOT_CHANNELID`。

`config.yaml` 支援 `stock_overrides`。目前共用設定套用 `00830`，`00631L` 覆寫 `regime_ma_window: 60`，此覆寫需以 IS/OOS 樣本內外驗證確認後才可採用。
