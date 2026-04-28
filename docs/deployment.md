# 一鍵部署 (docker compose)

`docker-compose.yml` 把 **MariaDB (含持久化 volume) + Go app + Caddy (reverse proxy + 自動 HTTPS)** 三個服務打包成一組，
**本機開發** 跟 **正式機 (有網域、要 HTTPS)** 共用同一份 yaml，只差在 `.env` 的設定值。
Windows / Linux 都用同一份指令啟動。

## 0. 前置需求

- Docker Engine 24+ / Docker Desktop（已內含 `docker compose` v2 子命令）
- Windows 用戶請用 PowerShell 或 Git Bash；Linux 直接用任何 shell。

## 1. 準備 .env

```bash
cp .env.example .env
```

預設值（`.env.example`）就是「本機 HTTP only」的設定，可直接跑。重點欄位：

| Key | 說明 |
| --- | --- |
| `MARIADB_ROOT_PASSWORD` / `MARIADB_USER` / `MARIADB_PASSWORD` / `MARIADB_DATABASE` | MariaDB 容器的初始化參數 |
| `DB_DSN` | App 連 MariaDB 的 DSN，預設 `exampleuser:examplepassword@tcp(mariadb:3306)/...`，host 走 compose 內部 service name |
| `DISCORD_BOT_TOKEN` / `DISCORD_BOT_CHANNELID` | 留空表示不使用 Discord bot |
| `SITE_ADDRESS` | Caddy 的對外站點。`:80` = 純 HTTP（本機）；`localhost` = HTTP + 自簽 HTTPS；`your-domain.com` = HTTP + 自動 Let's Encrypt（正式機） |
| `ACME_EMAIL` | Let's Encrypt 聯絡信箱（正式機填，本機留空） |
| `CADDY_HTTP_PORT` / `CADDY_HTTPS_PORT` | 對外 port。正式機 80/443；本機若被 HTTP.sys / 其它服務佔用可改 8080/8443 |

非私密超參數（追蹤股票、回測天數、策略 tiers 等）放在 `config.yaml`，會 bind-mount 進 app 容器，
改完 `config.yaml` 重啟 app 容器即生效。

## 2. 啟動

```bash
docker compose up -d --build
```

第一次會：

1. build app image（multi-stage，產純靜態 Go binary）。
2. 啟動 `mariadb`，自動執行 `deploy/mariadb/init.sql` 授權 app user。
3. App 等 mariadb healthcheck 通過後啟動，跑完 `sqls/SQLcommend.sql` 建表，再開始 TWSE 回補資料。
4. Caddy 依 `SITE_ADDRESS` 啟動：本機就純 HTTP；正式機自動跟 Let's Encrypt 簽證書並 redirect 80→443。

> ⚠️ **首次啟動需要等回補完成才會開 HTTP port**：app 在 `Init` 內會先抓 `init_db_back_months` 個月的 TWSE
> 歷史資料寫入 MariaDB，之後才 `go EchoInit()` 啟動 HTTP server。預設 60 個月 × N 檔股票，約 5–10 分鐘。
> 期間打 `/health` 會收到 502 (Caddy 連不到還沒起來的 app)，屬正常。可用 `docker compose logs -f app` 觀察進度。

## 3. 驗證

**本機 (`SITE_ADDRESS=:80`、`CADDY_HTTP_PORT=8080`)：**

```bash
curl http://localhost:8080/health    # → {"status":"ok"}
curl http://localhost:8080/ready     # → {"status":"ready"}    (代表 app 連得上 MariaDB)
curl http://localhost:8080/metrics   # → Prometheus 格式指標
```

**正式機 (`SITE_ADDRESS=your-domain.com`)：**

```bash
curl https://your-domain.com/health  # 自動拿 LE 憑證
curl http://your-domain.com/health   # 自動 301 redirect 到 https://
```

## 4. 常用維運指令

```bash
docker compose ps                  # 看狀態
docker compose logs -f app         # 跟 app log
docker compose logs -f caddy       # 跟 caddy log（看 ACME 簽證書過程）
docker compose logs -f mariadb     # 跟 db log
docker compose restart app         # 改完 config.yaml 後重啟 app
docker compose down                # 停掉服務（保留 volume，下次起來資料還在）
docker compose down -v             # 連 volume 一起砍 (下次重新回補資料 + 重簽 LE 憑證)
```

MariaDB 資料持久化在 named volume `mariadb_data`；
Caddy 的 LE 憑證持久化在 named volume `caddy_data`（**重要**，沒持久化的話每次重啟會重簽，會被 LE 限速）。
只要不下 `down -v`，重啟、重 build 都不會掉資料 / 掉憑證。

## 5. 正式機部署到 HTTPS：完整步驟

1. 把網域的 DNS A record 指向 server 的公網 IP（或 AAAA 指向 IPv6）
2. 確認 server 防火牆 / 雲端 Security Group 對外開放 **80** 和 **443**：
   - 80 是 Let's Encrypt 走 HTTP-01 challenge 必經，過了之後 Caddy 自動 redirect 到 443
   - 443 是真正的 HTTPS 服務 port
3. 在 server 上 `cp .env.example .env`，編輯：
   ```env
   SITE_ADDRESS=your-domain.com
   ACME_EMAIL=you@example.com
   CADDY_HTTP_PORT=80
   CADDY_HTTPS_PORT=443
   # MARIADB_PASSWORD / MARIADB_ROOT_PASSWORD 請改強密碼
   ```
4. `docker compose up -d --build`
5. `docker compose logs -f caddy` 看到 `certificate obtained successfully` 就是簽完了，瀏覽器打 `https://your-domain.com` 即可

**換網域 / 重簽憑證**：改完 `.env` 的 `SITE_ADDRESS`，`docker compose up -d` 即可，Caddy 會自動處理。

**LE 簽證 rate limit**：每個註冊網域每週 5 張正式憑證。本機開發請務必維持 `SITE_ADDRESS=:80` 或 `localhost`，**不要拿正式網域反覆 up/down**，否則會被擋 7 天。
