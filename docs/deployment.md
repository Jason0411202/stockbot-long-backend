# 部署指南

本專案以發佈到 GHCR 的 app image 部署：MariaDB、Go app 與 Caddy reverse proxy。Caddy 可在本機提供 HTTP，也可在正式網域自動申請 HTTPS。

app image 由 GitHub Actions 自動 build 並推送到 GHCR，`config.yaml` 已烤進 image；Caddyfile 與 MariaDB 初始化 SQL 以 inline configs 內嵌於 `docker-compose.yml`。因此**正式機只需要 `docker-compose.yml` 與 `.env` 兩個檔案**，不需要原始碼。

> 本機開發不需要 Docker Compose，改用 `go run`，見 [development.md](development.md)。

## 準備環境

在一台裝好 Docker（含 compose plugin，需 v2.23+）的機器上取得部署所需的兩個檔案：

```bash
mkdir -p stockbot && cd stockbot
curl -fsSLO https://raw.githubusercontent.com/Jason0411202/stockbot-long-backend/main/docker-compose.yml
curl -fsSL  https://raw.githubusercontent.com/Jason0411202/stockbot-long-backend/main/.env.example -o .env
```

`.env.example` 預設可在本機直接使用。正式環境請至少調整 DB 密碼、Discord token、`SITE_ADDRESS` 與 Caddy port。

> ⚠️ **`MARIADB_USER` / `MARIADB_PASSWORD` 只在 `mariadb_data` volume「首次初始化」時生效。** volume 已存在後再改 `.env` 密碼，MariaDB 會直接忽略（仍用舊密碼），但 app 會改用新密碼，導致 `Error 1045 ... Access denied for user ... (using password: YES)` 連不上 DB。要在初始化後更換 DB 帳密，必須二選一：(a) `docker compose down -v` 砍掉 volume 重新初始化（本專案 DB 可由開機回補 + catch-up 自動重建，資料不會永久遺失）；或 (b) 以 root 進 MariaDB 手動 `ALTER USER '<MARIADB_USER>'@'%' IDENTIFIED BY '<新密碼>';` 對齊。單純改 `.env` 重新 `up -d` 無效。

> 策略參數（`config.yaml`）已隨 image 版本固定；要調整請改 repo 的 `config.yaml` 重新發版，或在 `app` service 掛載一份覆寫。

## 主要環境變數

| 變數 | 說明 |
| --- | --- |
| `APP_IMAGE` | app 要拉的 image；留空用上游公開 image，部署自己 fork 的版本時填 `ghcr.io/<your-account>/stockbot-long-backend:latest` |
| `MARIADB_ROOT_PASSWORD` | MariaDB root 密碼 |
| `MARIADB_USER` | app 使用的 DB 帳號；compose 用它組 `DB_DSN` 並於初始化時授權 |
| `MARIADB_PASSWORD` | app 使用的 DB 密碼 |
| `MARIADB_DATABASE` | 預設 `StockLongData` |
| `DB_DSN` | 僅本機 `go run`（不走 compose）時使用；compose 會自行以帳密組出 DSN |
| `DISCORD_BOT_TOKEN` | Discord bot token，可留空 |
| `DISCORD_BOT_CHANNELID` | Discord channel id，可留空 |
| `SITE_ADDRESS` | Caddy 站台位址，`:80` 表示 HTTP only，填網域則自動 HTTPS |
| `ACME_EMAIL` | Let's Encrypt 通知信箱 |
| `CADDY_HTTP_PORT` | 對外 HTTP port |
| `CADDY_HTTPS_PORT` | 對外 HTTPS port |

## 啟動

```bash
docker compose pull
docker compose up -d
```

第一次啟動時 app 會初始化 schema 並依烤進 image 的 `config.yaml` 回補歷史資料，可能需要數分鐘。回補完成前，Caddy 可能暫時回 502；可用下列指令看 app log：

```bash
docker compose logs -f app
```

## 健康檢查

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/metrics
```

若透過 Caddy 預設 80 port 對外：

```bash
curl http://localhost/health
```

## 正式機 HTTPS

1. 將 DNS A/AAAA record 指向主機。
2. 開放 80 與 443。
3. 在 `.env` 設定：

```env
SITE_ADDRESS=your-domain.com
ACME_EMAIL=you@example.com
CADDY_HTTP_PORT=80
CADDY_HTTPS_PORT=443
```

4. 重新啟動服務：

```bash
docker compose up -d
```

Caddy 會自動申請 Let's Encrypt 憑證，並把 HTTP redirect 到 HTTPS。

## 升級到新版

CI 在 main 更新後會推送新的 `latest` image，正式機重新拉取並滾動更新：

```bash
docker compose pull
docker compose up -d
```

### 自動 CD（選用）

不想每次手動部署的話，可啟用 `deploy-ssh` job 讓 main 更新**全自動**部署：CI 會 SSH 進 server，一次完成「裝 Docker → 取得 `docker-compose.yml` → 寫 `.env` → `pull && up -d`」。可直接對一台**全新未架過環境的 server** 跑，對已架好的 server 則自動略過安裝走滾動更新。

只需在 backend repo 設 `vars.STOCKBOT_LONG_DEPLOY_ENABLED=true`、secrets `STOCKBOT_LONG_DEPLOY_HOST` / `_DEPLOY_USER` / `_DEPLOY_PASSWORD`（一律密碼登入），以及把整份 `.env` 貼進 secret `STOCKBOT_LONG_ENV_FILE`（不貼則 `.env` 為空、compose 用內建預設值，僅適合測試）。登入帳號需能執行免密碼 sudo。完整說明見 [cicd-k8s.md](cicd-k8s.md)。此機制對 fork 者同樣適用，填自己的值即可，不需改任何檔案。

## 維運命令

| 命令 | 用途 |
| --- | --- |
| `docker compose ps` | 查看容器狀態 |
| `docker compose logs -f app` | 查看 app log |
| `docker compose logs -f caddy` | 查看 Caddy 與憑證 log |
| `docker compose logs -f mariadb` | 查看 DB log |
| `docker compose restart app` | 重啟 app |
| `docker compose pull && docker compose up -d` | 升級到最新 image |
| `docker compose down` | 停止服務並保留 volume |
| `docker compose down -v` | 停止服務並刪除 DB 與 Caddy volume |

## Volume

- `mariadb_data` 保存 MariaDB 資料。
- `caddy_data` 保存 Let's Encrypt 憑證。
- `caddy_config` 保存 Caddy runtime 設定。

正式環境避免使用 `docker compose down -v`，除非確定要刪除資料。
