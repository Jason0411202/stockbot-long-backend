# stockbot-long-backend

台股 ETF 長線與波段交易後端。系統會回補 TWSE 歷史價量、執行牛熊 regime 感知的加減碼策略，並提供 REST API 與 Prometheus metrics 給前端與監控使用。

目前追蹤標的預設為 `00631L` 與 `00830`。正式機以 Docker Compose 拉取 GHCR 上的 app image 部署（Go app + MariaDB + Caddy）；本機開發則直接 `go run`，不需 Compose（見 [docs/development.md](docs/development.md)）。

## 快速啟動

**1. 準備一台 Linux server**，記下 IP、登入帳號與密碼。

**2. 在 backend repo 設定下列值**（Settings → Secrets and variables → Actions）：

| 類型 | key | 說明 | value 範例 |
| --- | --- | --- | --- |
| Variable | `STOCKBOT_LONG_DEPLOY_ENABLED` | 啟用自動部署的開關 | `true` |
| Secret | `STOCKBOT_LONG_DEPLOY_HOST` | server IP 或網域 | `203.0.113.50` |
| Secret | `STOCKBOT_LONG_DEPLOY_USER` | SSH 登入帳號 | `root` |
| Secret | `STOCKBOT_LONG_DEPLOY_PASSWORD` | SSH 登入密碼 | `My$tr0ngP@ssw0rd` |
| Secret | `STOCKBOT_LONG_ENV_FILE` | 整份 `.env` 內容 | 見下方範例 |

`STOCKBOT_LONG_ENV_FILE` 範例：

```env
# MariaDB root 密碼（強烈建議修改）
MARIADB_ROOT_PASSWORD=change-me-root

# app 連線 DB 用的帳號與密碼（強烈建議修改）
MARIADB_USER=stockbot
MARIADB_PASSWORD=change-me-app

# 資料庫名稱（可選修改）
MARIADB_DATABASE=StockLongData

# Discord 通知 bot 的 token（必改）
DISCORD_BOT_TOKEN=MTA5xxxxxxxxxxxxxxxxxxxx.Gxxxxx.xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Discord 通知頻道 ID（必改）
DISCORD_BOT_CHANNELID=1234567890123456789

# 網域。供 Caddy 自動申請 HTTPS 用（必改）
SITE_ADDRESS=stockbot.example.com

# 信箱。供 Caddy 自動申請 HTTPS 用（必改）
ACME_EMAIL=you@example.com

# 對外 HTTP / HTTPS port（可選修改）
CADDY_HTTP_PORT=80
CADDY_HTTPS_PORT=443
```

**3. 手動觸發一次部署**：到 repo 的 **Actions → CI/CD → Run workflow** 執行一次（等價於 push 到 main；全新 server 直接架起、已架好則滾動更新）。之後每次 push 到 main 也會自動部署。

### 啟動後

第一次啟動會依烤進 image 的 `config.yaml` 回補約 5 年 TWSE 歷史資料，需要數分鐘；
回補完成前 Caddy 可能短暫回 502。完成後對外（經 Caddy 80 port）檢查：

```bash
curl http://stockbot.example.com/health     # 換成你的 server 位址或網域
curl http://stockbot.example.com/ready
curl http://stockbot.example.com/metrics
```

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
