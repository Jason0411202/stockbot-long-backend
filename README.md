# stockbot-long-backend

## 一鍵部署 (docker compose)

`docker-compose.yml` 把 **MariaDB (含持久化 volume) + Go app + Caddy (reverse proxy + 自動 HTTPS)** 三個服務打包成一組，
**本機開發** 跟 **正式機 (有網域、要 HTTPS)** 共用同一份 yaml，只差在 `.env` 的設定值。
Windows / Linux 都用同一份指令啟動。

### 0. 前置需求

- Docker Engine 24+ / Docker Desktop（已內含 `docker compose` v2 子命令）
- Windows 用戶請用 PowerShell 或 Git Bash；Linux 直接用任何 shell。

### 1. 準備 .env

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

### 2. 啟動

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

### 3. 驗證

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

### 4. 常用維運指令

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

### 5. 正式機部署到 HTTPS：完整步驟

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

---

## CI/CD + k8s 部署 (本地部署可跳過)
### 設定與 CD pipeline 串接的 GitHub Actions Secret
需要在 GitHub repo Settings → Secrets → Actions 設定：

| Secret | 說明 |
|--------|------|
| `MANIFEST_REPO_PAT` | 有 manifests repo push 權限的 Personal Access Token |

`GITHUB_TOKEN` 自動提供，不需手動設定。

#### 如何建立 MANIFEST_REPO_PAT

1. 前往 GitHub → 右上角頭像 → **Settings** → 左側最下方 **Developer settings**
2. 選擇 **Personal access tokens** → **Fine-grained tokens** → **Generate new token**
3. 設定：
   - **Token name**：`manifest-repo-ci`（自訂）
   - **Expiration**：依需求選擇
   - **Repository access**：選 **Only select repositories** → 選擇 `stockbot-long-backend-k8s-manifests`
   - **Permissions** → **Repository permissions** → **Contents**：設為 **Read and write**
4. 點擊 **Generate token**，複製產生的 token
5. 前往 `stockbot-long-backend` repo → **Settings** → **Secrets and variables** → **Actions** → **New repository secret**
   - **Name**：`MANIFEST_REPO_PAT`
   - **Secret**：貼上剛才複製的 token

---

### 匯入本 App 環境變數到 K8s
App 本身所需的環境變數（.env）需要匯入為 K8s Secret，供 Pod 使用。

#### 步驟

1. 切換到目標 `.env` 的所在目錄
2. 執行以下指令：

```bash
kubectl -n myapp create secret generic myapp-env \
  --from-env-file=.env \
  --dry-run=client -o yaml | kubectl apply -f -
```

#### 驗證

```bash
kubectl -n myapp get secret myapp-env -o yaml   # 確認 Secret 已建立
kubectl -n myapp rollout restart deployment myapp  # 重啟 Pod 以套用新的環境變數
```

#### 更新

修改 `.env` 後重新執行上面的 `kubectl create secret` 指令即可，`--dry-run=client -o yaml | kubectl apply -f -` 會自動覆蓋舊值。


## 前端

請參考 https://github.com/Jason0411202/stockbot-long-frontend

---

## 買賣邏輯

* 主攻台股 ETF (006208, 00830) 長線 + 波段交易
* 每檔股票各自有自己的冷卻期，彼此獨立計算
* 買入與賣出皆以「股數」為基本交易單位：將目標金額換算成最接近的股數 (四捨五入) 後實際下單
* 當股價來到 20 MA 以下時執行買入操作，買入股數由 baseline 加減碼邏輯 + 目前單價計算得出，該股票買入後 `cooldown_days` 天內不再買入
* 當某支追蹤的股票，其最低購買價的獲利超過 `baseline_sell_threshold` (100%) 時，執行賣出操作，賣出股數由賣出金額換算得出，本操作沒有冷卻

### 加減碼邏輯

#### Baseline method (原金字塔策略)

目前專案中唯一的交易策略。`scaling_strategy` 只接受 `Baseline`；此策略在舊版本中稱為「金字塔策略 (Pyramid)」。

* 買入金額按照當前股價相對於持有中最高買入價的比例，越低買越多 (預設 tiers 見 `config.yaml`)：
  * -10% 內：500
  * -20% 內：750
  * -30% 內：1300
  * -40% 內：2000
  * 超過 -40%：3000 (`baseline_buy_fallback_amount`)
* 觸發賣出時的目標金額為 `baseline_sell_amount` (預設 10000)，實際會乘以 `buy_and_sell_multiplier`
* 所有金額最後都會除以當天股價並四捨五入得到「實際買/賣股數」

### 資金安全 (no-borrow 不變量)

回測 / 模擬交易的可利用資金**僅等於當前持有現金** (起始現金 + 已實現賣出收入 − 已支出買入成本)，
不得借錢、也不允許透支。實作上在 `runBacktestOnSeries` 中以下列方式防守：

1. 每次買入前計算 `maxAffordable = floor(cash / price)`。若策略目標股數超過 `maxAffordable`，則夾取到 `maxAffordable`。
2. 若夾取後仍為 0，則跳過該次買進並累計到 `SkippedBuys`。
3. 任何時刻若 `cash < 0`，回測立即以錯誤中止 (不變量違反)。

## 績效

### 真實資料回測 (006208, 00830, 過去 60 個月)

以 `config.yaml` 預設設定 (`initial_cash=100000`, `buy_and_sell_multiplier=2.0`, `cooldown_days=14`, `init_db_back_months=60`, `back_testing_months=60`) 執行 `go run ./cmd/research_run`。

> 歷史紀錄：早期 config 用的是 `back_testing_days: 3600`，但因為 `init_db_back_months=60` 只提供 ~5 年資料，
> 實際被靜默截尾成 ~1231 天的回測結果。後續改成 `back_testing_months` 為單位後，跟 `init_db_back_months` 同單位、可精確比對，
> sanity check (見 `config/config_test.go`) 不再需要靠「每月平均交易日 × 22」做近似。
> 下方數值需要重跑才會更新；重跑指令：`go run ./cmd/research_run` (本機需要 DB 連線) 或 `docker compose exec app …`。

| 指標 | 值 (來自舊版 3600 days→1231 天靜默截尾的 run，僅供參考) |
| --- | --- |
| TrackStocks | 006208, 00830 |
| BackTestingMonths (current default) | 60 |
| InitialCash | 100,000.00 |
| FinalCash | 213,903.40 |
| FinalHoldingValue | 68,416.45 |
| FinalTotal | 282,319.85 |
| TotalBuys | 168 |
| TotalSells | 149 |
| SkippedBuys | 252 |
| PnL vs Initial | +182,319.85 (+182.32%) |

`SkippedBuys=252` 直接印證了「禁止借錢」夾取在真實資料上真的會被觸發 — 若沒有這條不變量，當前 initial_cash 下的 baseline 策略會在數百次買進時把現金夾到負數、產生隱性槓桿，讓歷史績效被高估。

## ToDo

* [x] 把 MariaDB 跟 Nginx 的安裝過程 + 專案的執行寫成 docker-compose
* [ ] 加入針對 RSI 指標的加碼賣出邏輯，並回測效果
* [x] 加入 ELK 來管理 log（見 k8s-infra repo）
* [x] 加入 Kubernetes 來管理容器（見 k8s-manifests + k8s-infra repo）
* [ ] 暴搜找出最佳參數（如買入賣出金額、間隔等等）
