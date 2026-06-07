# API 文件

本專案使用 Echo 提供 HTTP API。正式 server 預設監聽 `:8080`，HTTPS 由 Caddy 或外部 Ingress 處理。

## 維運端點

| Method | Path | 說明 |
| --- | --- | --- |
| `GET` | `/health` | liveness probe，服務程序可回應即回 `{"status":"ok"}` |
| `GET` | `/ready` | readiness probe，會確認 DB 可 ping |
| `GET` | `/metrics` | Prometheus metrics |

## 業務端點

| Method | Path | 說明 |
| --- | --- | --- |
| `GET` | `/` | 簡易首頁訊息 |
| `GET` | `/api/get_unrealized_gains_losses` | 取得目前未實現持倉與損益 |
| `GET` | `/api/get_realized_gains_losses` | 取得已實現損益紀錄 |
| `GET` | `/api/get_stock_statistic_data` | 取得追蹤標的統計資料 |
| `GET` | `/api/get_stock_history_data?stock_id=00631L` | 取得指定股票歷史收盤資料 |

## 回應資料

API 回應由 `internal/dto` 定義。`portfolio.go` 對應投資組合損益，`market.go` 對應價格統計與歷史價格點。

## 錯誤處理

目前 controller 對部分查詢錯誤會回傳空陣列，維持前端既有行為。若未來要改成標準錯誤格式，需同步調整前端與測試。
