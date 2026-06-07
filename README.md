# stockbot-long-backend

台股 ETF 長線與波段交易後端。系統會回補 TWSE 歷史價量、執行牛熊 regime 感知的加減碼策略，並提供 REST API 與 Prometheus metrics 給前端與監控使用。

目前追蹤標的預設為 `00631L` 與 `00830`，部署組合為 Go app、MariaDB 與 Caddy，可用同一份 `docker-compose.yml` 在本機或正式機啟動。

## 快速啟動

```bash
cp .env.example .env
docker compose up -d --build
```

第一次啟動會依 `config.yaml` 回補 TWSE 歷史資料，可能需要數分鐘。完成後可用下列指令檢查：

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/metrics
```

## 文件導覽

| 文件 | 內容 |
| --- | --- |
| [docs/overview.md](docs/overview.md) | 專案架構、資料流、主要套件責任 |
| [docs/development.md](docs/development.md) | 本機開發、常用命令、測試方式 |
| [docs/api.md](docs/api.md) | REST API 與維運端點 |
| [docs/strategy.md](docs/strategy.md) | 目前交易演算法、參數語意、資金安全規則 |
| [docs/backtest.md](docs/backtest.md) | 回測方法、重現指令、目前績效結果 |
| [docs/database-schema.md](docs/database-schema.md) | MariaDB schema、資料表欄位與寫入路徑 |
| [docs/deployment.md](docs/deployment.md) | Docker Compose、本機與正式機部署 |
| [docs/cicd-k8s.md](docs/cicd-k8s.md) | GitHub Actions 與 Kubernetes Secret 維護 |

## 前端

前端專案請參考 <https://github.com/Jason0411202/stockbot-long-frontend>。

## 免責聲明

本專案內容與回測結果僅供工程與研究參考，不構成投資建議。歷史績效不代表未來績效。
