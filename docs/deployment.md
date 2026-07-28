# 部署指南

本專案以發佈到 GHCR 的 app image 部署：MariaDB、Go app、Caddy reverse proxy，以及內建監控棧（Grafana + Loki + Alloy + Prometheus）。Caddy 可在本機提供 HTTP，也可在正式網域自動申請 HTTPS。

app image 由 GitHub Actions 自動 build 並推送到 GHCR，`config.yaml` 已烤進 image；Caddyfile 與 MariaDB 初始化 SQL 以 inline configs 內嵌於 `docker-compose.yml`，監控棧設定放在 `monitoring/` 目錄。因此**正式機只需要 `docker-compose.yml`、`.env` 與 `monitoring/` 目錄**，不需要完整原始碼。

> 本機開發不需要 Docker Compose，改用 `go run`，見 [development.md](development.md)。

## 準備環境

在一台裝好 Docker（含 compose plugin，需 v2.23+）的機器上取得部署所需的檔案（以 repo tarball 一次取得 compose + monitoring/）：

```bash
mkdir -p stockbot && cd stockbot
curl -fsSL https://codeload.github.com/Jason0411202/stockbot-long-backend/tar.gz/refs/heads/main \
  | tar -xz --strip-components=1 stockbot-long-backend-main/docker-compose.yml stockbot-long-backend-main/monitoring
curl -fsSL https://raw.githubusercontent.com/Jason0411202/stockbot-long-backend/main/.env.example -o .env
```

> 💡 **記憶體需求**：完整棧（app + MariaDB + Caddy + 監控四容器）建議 2 GiB RAM 以上。1 GiB 的小 VM（如 Azure B2ats v2）務必搭配 2G swap（自動 CD 的 bootstrap 會自動建立；手動部署見下方「小記憶體 VM」一節）。監控棧四個容器皆已設 256M 記憶體上限，保證不會擠壓交易主程式。

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
| `GRAFANA_ADMIN_USER` | Grafana 登入帳號（預設 `admin`） |
| `GRAFANA_ADMIN_PASSWORD` | Grafana 登入密碼（預設 `admin`，正式環境必改） |
| `LOG_LEVEL` | app log 最低輸出等級（`debug`/`info`/`warn`/`error`，預設 `info`；容器內 `LOG_FORMAT` 已固定為 `json`） |

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

## 監控（Grafana + Loki + Alloy + Prometheus）

啟動後即內建完整可觀測性，日常維運不需要 SSH 進 server：

| 元件 | 角色 |
| --- | --- |
| **Grafana** | 視覺化入口，經 Caddy 以 `https://<你的網域>/grafana` 對外（自帶登入頁） |
| **Loki** | log 儲存與查詢（保留 31 天，filesystem 儲存） |
| **Alloy** | 收集器：所有容器 stdout/stderr log、主機 systemd journal、主機指標（內嵌 node exporter）、容器指標（內嵌 cAdvisor） |
| **Prometheus** | 指標儲存（保留 15 天），抓取 app 的 `/metrics` 與監控棧自身，並接收 Alloy 的 remote_write |

登入 Grafana（帳密見 `.env` 的 `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD`）後，開啟內建的「Stockbot 總覽」dashboard（Dashboards → Stockbot 資料夾），涵蓋：交易機器人狀態（權益 / 現金 / 成交 / 水位線 / TWSE 回補失敗）、HTTP API（速率 / 延遲分位數 / 錯誤率）、主機資源（CPU / RAM / 磁碟 / 網路）、各容器資源、與三個集中式 log 面板（app 結構化 log、全容器 error/warning、systemd journal）。

log 皆為結構化 JSON（app 由 `LOG_FORMAT=json` 輸出；access log 附 `request_id`），在 Grafana 的 Loki 查詢中可用欄位過濾，例如：

```
{service="app"} | json | level="error"
{service="app"} | json | stock_id="00631L"
{job="systemd-journal"} |= "docker"
```

Prometheus 與 Loki 不對外開放 port，只能經 Grafana 查詢；Grafana 本身有登入保護。

### 小記憶體 VM（1 GiB）注意事項

監控棧四容器合計約 400–600 MB。在 1 GiB RAM 的 VM（如 Azure B2ats v2）手動部署時請先建立 swap：

```bash
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile
sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

自動 CD（`deploy-ssh`）的 bootstrap 已內建這一步（無 swap 時自動建立，idempotent）。所有容器已設定 log rotation（每檔 10MB × 3 檔），不會因容器 log 吃滿磁碟。

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

不想每次手動部署的話，可啟用 `deploy-ssh` job 讓 main 更新**全自動**部署：CI 會 SSH 進 server，一次完成「裝 Docker → 建 swap（無 swap 時）→ 取得 `docker-compose.yml` + `monitoring/` → 寫 `.env` → `pull && up -d`」。可直接對一台**全新未架過環境的 server** 跑，對已架好的 server 則自動略過安裝走滾動更新。

只需在 backend repo 設 `vars.STOCKBOT_LONG_DEPLOY_ENABLED=true`、secrets `STOCKBOT_LONG_DEPLOY_HOST` / `_DEPLOY_USER` / `_DEPLOY_PASSWORD`（一律密碼登入），以及把整份 `.env` 貼進 secret `STOCKBOT_LONG_ENV_FILE`（不貼則 `.env` 為空、compose 用內建預設值，僅適合測試）。登入帳號需能執行免密碼 sudo。完整說明見 [cicd-k8s.md](cicd-k8s.md)。此機制對 fork 者同樣適用，填自己的值即可，不需改任何檔案。

## 維運命令

| 命令 | 用途 |
| --- | --- |
| `docker compose ps` | 查看容器狀態 |
| `docker compose logs -f app` | 查看 app log（日常建議直接用 Grafana 的 Loki 面板查） |
| `docker compose logs -f caddy` | 查看 Caddy 與憑證 log |
| `docker compose logs -f mariadb` | 查看 DB log |
| `docker compose logs -f alloy` | 查看收集器 log（監控資料沒進來時先看這裡） |
| `docker compose restart app` | 重啟 app |
| `docker compose pull && docker compose up -d` | 升級到最新 image |
| `docker compose down` | 停止服務並保留 volume |
| `docker compose down -v` | 停止服務並刪除所有 volume（含 DB 與監控歷史資料） |

## Volume

- `mariadb_data` 保存 MariaDB 資料。
- `caddy_data` 保存 Let's Encrypt 憑證。
- `caddy_config` 保存 Caddy runtime 設定。
- `loki_data` 保存 log 歷史（31 天保留）。
- `prometheus_data` 保存指標歷史（15 天保留）。
- `grafana_data` 保存 Grafana 使用者設定與自建 dashboard。
- `alloy_data` 保存收集器讀取進度（避免重啟後重複收 log）。

正式環境避免使用 `docker compose down -v`，除非確定要刪除資料。
