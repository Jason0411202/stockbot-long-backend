# 部署指南

本專案提供 Docker Compose 部署：MariaDB、Go app 與 Caddy reverse proxy。Caddy 可在本機提供 HTTP，也可在正式網域自動申請 HTTPS。

## 準備環境

```bash
cp .env.example .env
```

`.env.example` 預設可在本機直接使用。正式環境請至少調整 DB 密碼、Discord token、`SITE_ADDRESS` 與 Caddy port。

## 主要環境變數

| 變數 | 說明 |
| --- | --- |
| `MARIADB_ROOT_PASSWORD` | MariaDB root 密碼 |
| `MARIADB_USER` | app 使用的 DB 帳號 |
| `MARIADB_PASSWORD` | app 使用的 DB 密碼 |
| `MARIADB_DATABASE` | 預設 `StockLongData` |
| `DB_DSN` | 非 compose 執行時使用的 DB DSN |
| `DISCORD_BOT_TOKEN` | Discord bot token，可留空 |
| `DISCORD_BOT_CHANNELID` | Discord channel id，可留空 |
| `SITE_ADDRESS` | Caddy 站台位址，`:80` 表示 HTTP only |
| `ACME_EMAIL` | Let's Encrypt 通知信箱 |
| `CADDY_HTTP_PORT` | 對外 HTTP port |
| `CADDY_HTTPS_PORT` | 對外 HTTPS port |

## 本機啟動

```bash
docker compose up -d --build
```

第一次啟動時 app 會初始化 schema 並回補歷史資料。回補完成前，Caddy 可能暫時回 502；可用下列指令看 app log：

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

4. 啟動服務：

```bash
docker compose up -d --build
```

Caddy 會自動申請 Let's Encrypt 憑證，並把 HTTP redirect 到 HTTPS。

## 維運命令

| 命令 | 用途 |
| --- | --- |
| `docker compose ps` | 查看容器狀態 |
| `docker compose logs -f app` | 查看 app log |
| `docker compose logs -f caddy` | 查看 Caddy 與憑證 log |
| `docker compose logs -f mariadb` | 查看 DB log |
| `docker compose restart app` | 重啟 app |
| `docker compose down` | 停止服務並保留 volume |
| `docker compose down -v` | 停止服務並刪除 DB 與 Caddy volume |

## Volume

- `mariadb_data` 保存 MariaDB 資料。
- `caddy_data` 保存 Let's Encrypt 憑證。
- `caddy_config` 保存 Caddy runtime 設定。

正式環境避免使用 `docker compose down -v`，除非確定要刪除資料。
