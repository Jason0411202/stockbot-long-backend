# stockbot-long-backend

台股 ETF 長線與波段交易後端。系統會回補 TWSE 歷史價量、執行牛熊 regime 感知的加減碼策略，並提供 REST API 與 Prometheus metrics 給前端與監控使用。

目前追蹤標的預設為 `00631L` 與 `00830`。正式機以 Docker Compose 拉取 GHCR 上的 app image 部署（Go app + MariaDB + Caddy）；本機開發則直接 `go run`，不需 Compose（見 [docs/development.md](docs/development.md)）。

## 快速啟動（在一台全空的 Linux 正式機部署）

app image 由 GitHub Actions 自動 build 並推送到 GHCR，`config.yaml` 已烤進 image。
因此正式機**不需要原始碼**，只要 `docker-compose.yml` 與 `.env` 兩個檔案即可部署。

```bash
# 1) 安裝 Docker（含 compose plugin，需 v2.23+）
curl -fsSL https://get.docker.com | sh

# 2) 取得部署所需的兩個檔案
mkdir -p stockbot && cd stockbot
curl -fsSLO https://raw.githubusercontent.com/Jason0411202/stockbot-long-backend/main/docker-compose.yml
curl -fsSL  https://raw.githubusercontent.com/Jason0411202/stockbot-long-backend/main/.env.example -o .env

# 3) 編輯 .env：改掉 DB 密碼，填入網域與 Discord（沒有就留空）
#    SITE_ADDRESS=your-domain.com → Caddy 自動申請 HTTPS；本機/無網域測試填 :80
vi .env

# 4) 拉 image 並啟動
docker compose pull
docker compose up -d
```

第一次啟動會依烤進 image 的 `config.yaml` 回補約 5 年 TWSE 歷史資料，需要數分鐘；
回補完成前 Caddy 可能短暫回 502。可用 `docker compose logs -f app` 觀察進度，完成後檢查：

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/metrics
```

> **網域 + HTTPS**：先把 DNS A record 指到本機並對外開放 80/443，於 `.env` 設
> `SITE_ADDRESS=your-domain.com` 與 `ACME_EMAIL`，Caddy 會自動向 Let's Encrypt 申請憑證。
>
> 完整部署、維運命令與 HTTPS 細節見 [docs/deployment.md](docs/deployment.md)。

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
