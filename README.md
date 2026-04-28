# stockbot-long-backend

台股 ETF (006208, 00830) 長線 + 波段交易 backend。
排程拉 TWSE 歷史價量、跑 baseline 加減碼策略、提供 REST API + Prometheus metrics 給前端與監控使用。
打包成 docker compose (MariaDB + Go app + Caddy 自動 HTTPS)，本機 / 正式機共用同一份 yaml。

## 文件導覽

| 文件 | 內容 |
| --- | --- |
| [docs/deployment.md](docs/deployment.md) | docker compose 一鍵部署、本機 / 正式機 HTTPS、常用維運指令 |
| [docs/cicd-k8s.md](docs/cicd-k8s.md) | GitHub Actions Secret 設定、k8s Secret 匯入流程 |
| [docs/database-schema.md](docs/database-schema.md) | 資料庫 (`StockLongData`) 四張 table 的欄位、PK、寫入路徑說明 |
| [docs/strategy.md](docs/strategy.md) | 買賣邏輯、Baseline 加減碼策略、no-borrow 不變量 |
| [docs/backtest.md](docs/backtest.md) | 真實資料回測結果與重跑指令 |

## 快速啟動

```bash
cp .env.example .env
docker compose up -d --build
```

預設 `.env.example` 是「本機 HTTP only」設定，可直接跑。第一次啟動會等 5–10 分鐘回補 TWSE 歷史資料，
完成後 `curl http://localhost:8080/health` 應回 `{"status":"ok"}`。

完整步驟、正式機 HTTPS、維運指令請見 [docs/deployment.md](docs/deployment.md)。

## 前端

請參考 https://github.com/Jason0411202/stockbot-long-frontend
