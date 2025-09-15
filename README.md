# stockbot-long-backend
## 執行
1. 在伺服器中安裝 mariadb
  ```
  sudo apt update
  sudo apt upgrade
  sudo apt install software-properties-common
  sudo apt-key adv --recv-keys --keyserver keyserver.ubuntu.com F1656F24C74CD1D8
  sudo add-apt-repository 'deb [arch=amd64,arm64,ppc64el] http://mirrors.aliyun.com/mariadb/repo/10.5/ubuntu bionic main'
  sudo apt update
  sudo apt install mariadb-server --fix-missing
  ```
  並使用以下指令初始化資料庫設定
  ```
  sudo mysql_secure_installation
  ```
  修改配置檔，使得容器能夠連接到資料庫
  ```
  /etc/mysql/mariadb.conf.d/50-server.cnf 中的 bind-address 改成 0.0.0.0
  sudo systemctl restart mariadb
  ```
  修改防火牆設定
  ```
  sudo ufw allow in 3306
  ```
  登入 maria db
  ```
  sudo mysql -h localhost -u root -p
  ```
  並創建一組帳密供容器連接 (帳號: exampleuser, 密碼: examplepassword)
  ```
  sudo mysql -h localhost -u root -p
  CREATE USER 'exampleuser'@'%' IDENTIFIED BY 'examplepassword';
  GRANT ALL PRIVILEGES ON *.* TO 'exampleuser'@'%';
  FLUSH PRIVILEGES;
  ```
2. 獲取 https 憑證，並把憑證貼到專案根目錄下 (`jason-server.eastus2.cloudapp.azure.com` 是自己伺服器的 DNS 名稱，剛生成的憑證會存在 `/etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/` 中，這些指令在專案根目錄下執行)
  ```
  sudo apt-get update
  sudo apt-get install certbot python3-certbot-nginx --fix-missing
  sudo certbot --nginx -d jason-server.eastus2.cloudapp.azure.com
  sudo cp /etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/cert.pem .
  sudo cp /etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/privkey.pem .
  ```
3. 安裝並配置 nginx
  ```
  sudo apt update (剛創虛擬機時一定要打，不然可能會裝到舊版)
  sudo apt install nginx
  ```
  編輯 `sudo nano /etc/nginx/nginx.conf`
  加入這段，jason-server.eastus2.cloudapp.azure.com 換成你的網域
  ```
  server {
      listen 443 ssl;
      server_name jason-server.eastus2.cloudapp.azure.com;

      ssl_certificate /etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/cert.pem;
      ssl_certificate_key /etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/privkey.pem;

      location / {
          proxy_pass https://localhost:8000;
          proxy_set_header Host $host;
          proxy_set_header X-Real-IP $remote_addr;
          proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
          proxy_set_header X-Forwarded-Proto $scheme;
      }
  }
  ```
  重啟 nginx
  ```
  sudo service nginx restart
  ```

4. 切換當前工作目錄至 `backend` 資料夾，安裝 docker 跟 docker-compose
    ```shell
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
    sudo apt install docker-compose
    ```

5. 配置 .env 檔案
```
MariadbUser=exampleuser (剛剛創建的帳號)
MariadbPassword=examplepassword (剛剛創建的密碼)
MariadbHost=10.0.0.4 (伺服器 ip，可以由 ip a 查看)
MariadbPort=3306 (資料庫所在的 port，預設為 3306)
DISCORD_BOT_TOKEN=ahsofihoaishfoiahfioawhwnf (你的 discord bot token, 允許不給, 表示不使用 discord bot 功能)
DISCORD_BOT_CHANNELID=185616854896156165 (你的 discord bot 頻道 id, 允許不給, 表示不發送訊息至任何 discord bot chennel)
TrackStocks_Market=006208 (追蹤的市值型股票)
TrackStocks_HighDividend=00929&0056 (追蹤的配息型股票)
Scaling_Strategy=Pyramid (加減碼策略，可以為 Pyramid)
BuyAndSell_Multiplier=2.0 (隨著本金增多，買賣金額也會透過自行設定的參數，成倍數增長, 未指定時預設為 1.0)
BackTesting=1800 (回測天數，-1 代表不回測)
```
* 註: 若是使用 container 運行本專案，使用 `ip addr show docker0` 來獲取 `MariadbHost` 的值
6. 建立映像檔
```
sudo docker build -t "stockbot-long-backend" .
```
7. 執行容器
```
sudo docker run -p 8000:8000 --env-file .env --restart=always -d --name stockbot-long-backend stockbot-long-backend
```

## 關於前端伺服器的部署
請參考 `https://github.com/Jason0411202/stockbot-long-frontend`

## 買賣邏輯
* 主攻市值型 ETF 長線 + 波段交易
* 台股市值型 (00631L) 與美股市值型 (00830, 00662) ETF 計數器分開計算
* 當股價來到時 20 MA 以下時執行買入操作，買入金額參考加減碼邏輯，操作後半個月內不再進行任何關於該型的購買操作
* 當某支追蹤的股票，其最低購買價的獲利超過 100% 時，固定進行賣出操作，賣出金額參考加減碼邏輯，本操作沒有冷卻

### 加減碼邏輯
#### 金字塔策略 (Pyramid)
* 買入金額按照當前股價相對於過去購買時的最高股價之價差比例，越低買越多
  * -0% 共買 500
  * -10% 共買 750
  * -20% 共買 1300
  * -30% 共買 2000
  * -40% 共買 3000
* 賣出金額固定為 10000

## 績效
* 從 2024/08/05 回測過去五年的績效
  * 最大投入金額約為 70000
  * 總淨損益約為 84000 (已實現損益 71858.14, 未實現損益 7067.04)

## ToDo
* [ ] 把 mariadb 跟 nginx 的安裝過程 + 專案的執行寫成 docker-compose
* [ ] 加入針對 RSI 指標的的加碼賣出邏輯，並回測效果
* [ ] 加入 ELK 來管理 log
* [ ] 加入 Kubernetes 來管理容器
* [ ] 暴搜找出最佳參數 (如買入賣出金額、間隔等等)