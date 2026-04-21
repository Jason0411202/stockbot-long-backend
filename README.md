# stockbot-long-backend

## 本地部署

### 1. 安裝 MariaDB

```bash
sudo apt update && sudo apt upgrade
sudo apt install mariadb-server
sudo mysql_secure_installation
```

修改配置檔，使容器能連接到資料庫：

```bash
# /etc/mysql/mariadb.conf.d/50-server.cnf 中的 bind-address 改成 0.0.0.0
sudo systemctl restart mariadb
sudo ufw allow in 3306
```

建立帳號供 app 使用：

```sql
sudo mysql -h localhost -u root -p

CREATE USER 'exampleuser'@'%' IDENTIFIED BY 'examplepassword';
GRANT ALL PRIVILEGES ON *.* TO 'exampleuser'@'%';
FLUSH PRIVILEGES;
```

### 2. 安裝 Docker

```bash
# Add Docker's official GPG key:
sudo apt-get update
sudo apt-get install ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

# Add the repository to Apt sources:
echo \
"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
$(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}") stable" | \
sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update

sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

### 3. 配置 .env 與 config.yaml

私密設定放在 `.env` (不進 git)：

```bash
cp .env.example .env
```

編輯 `.env`：

```env
DB_DSN=exampleuser:examplepassword@tcp(172.17.0.1:3306)/?multiStatements=true
DISCORD_BOT_TOKEN=你的_discord_bot_token（可不填，表示不使用 Discord bot）
DISCORD_BOT_CHANNELID=你的_discord_channel_id（可不填）
```

**.env Keys (私密)**
1. `DB_DSN`: MariaDB connection string.
2. `DISCORD_BOT_TOKEN`: Discord bot token.
3. `DISCORD_BOT_CHANNELID`: Discord channel ID.

非私密的超參數一律寫在專案根目錄的 `config.yaml` (會被 commit 進 git)：

```yaml
track_stocks:
  - "006208"
  - "00830"
scaling_strategy: Pyramid     # 目前僅支援 Pyramid
buy_and_sell_multiplier: 2.0
max_back_months: 1
init_db_back_months: 60
back_testing_days: 3600       # -1 表示關閉回測
cooldown_days: 14             # 每檔股票各自的買入冷卻天數
pyramid_buy_tiers:
  - { above: -0.1, amount: 500 }
  - { above: -0.2, amount: 750 }
  - { above: -0.3, amount: 1300 }
  - { above: -0.4, amount: 2000 }
pyramid_buy_fallback_amount: 3000
pyramid_sell_threshold: 1.0   # 最低買入價獲利 >100% 才賣
pyramid_sell_amount: 10000
initial_cash: 1000000         # 回測起始現金
```

> **DB_DSN 中的 host**：容器連接宿主機的 MariaDB，使用 `ip addr show docker0` 取得 Docker bridge IP（通常是 `172.17.0.1`）。
> 如需更換 config 路徑，可透過環境變數 `CONFIG_PATH` 指定。

### 4. 建立並啟動容器

```bash
sudo docker build -t stockbot-long-backend .
sudo docker run -p 8080:8080 --env-file .env --restart=always -d --name stockbot-long-backend stockbot-long-backend
```

### 5. 設定 Nginx 反向代理（對外 HTTPS）

App 以 HTTP 運行在 port 8080，對外 HTTPS 由 Nginx 負責 TLS termination。

安裝 Nginx 和 Certbot：

```bash
sudo apt update
sudo apt install nginx
sudo apt install certbot python3-certbot-nginx
```

取得 TLS 憑證（將 `your-domain.com` 換成你的域名）：

```bash
sudo certbot --nginx -d your-domain.com
```

編輯 Nginx 設定 `sudo nano /etc/nginx/sites-available/default`：

```nginx
server {
    listen 443 ssl;
    server_name your-domain.com;

    ssl_certificate /etc/letsencrypt/live/your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

```bash
sudo nginx -t && sudo systemctl restart nginx
```

### 6. 驗證

```bash
curl http://localhost:8080/health    # → {"status":"ok"}
curl http://localhost:8080/ready     # → {"status":"ready"}
curl http://localhost:8080/metrics   # → Prometheus 格式指標
curl https://your-domain.com/health  # → 透過 Nginx 的 HTTPS
```

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
* 當股價來到 20 MA 以下時執行買入操作，買入股數由金字塔加減碼邏輯 + 目前單價計算得出，該股票買入後 `cooldown_days` 天內不再買入
* 當某支追蹤的股票，其最低購買價的獲利超過 `pyramid_sell_threshold` (100%) 時，執行賣出操作，賣出股數由賣出金額換算得出，本操作沒有冷卻

### 加減碼邏輯

#### 金字塔策略 (Pyramid)

* 買入金額按照當前股價相對於持有中最高買入價的比例，越低買越多 (預設 tiers 見 `config.yaml`)：
  * -10% 內：500
  * -20% 內：750
  * -30% 內：1300
  * -40% 內：2000
  * 超過 -40%：3000 (`pyramid_buy_fallback_amount`)
* 觸發賣出時的目標金額為 `pyramid_sell_amount` (預設 10000)，實際會乘以 `buy_and_sell_multiplier`
* 所有金額最後都會除以當天股價並四捨五入得到「實際買/賣股數」

## 績效

* 從 2024/08/05 回測過去五年的績效
  * 最大投入金額約為 70000
  * 總淨損益約為 84000 (已實現損益 71858.14, 未實現損益 7067.04)

## ToDo

* [ ] 把 MariaDB 跟 Nginx 的安裝過程 + 專案的執行寫成 docker-compose
* [ ] 加入針對 RSI 指標的加碼賣出邏輯，並回測效果
* [x] 加入 ELK 來管理 log（見 k8s-infra repo）
* [x] 加入 Kubernetes 來管理容器（見 k8s-manifests + k8s-infra repo）
* [ ] 暴搜找出最佳參數（如買入賣出金額、間隔等等）
