# stockbot-long-backend
## 執行
1. 在伺服器中安裝 mariadb
  ```
  sudo apt update
  sudo apt upgrade
  sudo apt install software-properties-common
  sudo add-apt-repository 'deb [arch=amd64,arm64,ppc64el] http://mirrors.aliyun.com/mariadb/repo/10.5/ubuntu bionic main'
  sudo apt update
  sudo apt install mariadb-server
  ```
  並使用以下指令初始化資料庫設定
  ```
  sudo mysql_secure_installation
  ```
  修改配置檔，使得容器能夠連接到資料庫
  ```
  /etc/mysql/mariadb.conf.d/50-server.cnf 中的 bind-address 改成 0.0.0.0
  ```
  修改防火牆設定
  ```
  sudo ufw allow in 3306
  ```
  創建一組帳密供容器連接 (帳號: exampleuser, 密碼: examplepassword)
  ```
  CREATE USER 'exampleuser'@'%' IDENTIFIED BY 'examplepassword';
  GRANT ALL PRIVILEGES ON *.* TO 'externaluser'@'%';
  FLUSH PRIVILEGES;
  ```
2. 獲取 https 憑證，並把憑證貼到專案根目錄下 (`jason-server.eastus2.cloudapp.azure.com` 是自己伺服器的 DNS 名稱，剛生成的憑證會存在 `/etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/` 中，這些指令在專案根目錄下執行)
  ```
  sudo apt-get update
  sudo apt-get install certbot python3-certbot-nginx
  sudo certbot --nginx -d jason-server.eastus2.cloudapp.azure.com
  sudo cp /etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/cert.pem .
  sudo cp /etc/letsencrypt/live/jason-server.eastus2.cloudapp.azure.com/privkey.pem .
  ```
1. 安裝並配置 nginx
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

      location /api/get_realized_gains_losses {
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
  
1. 配置 .env 檔案
```
MariadbUser=exampleuser (剛剛創建的帳號)
MariadbPassword=examplepassword (剛剛創建的密碼)
MariadbHost=10.0.0.4 (伺服器 ip，可以由 ip a 查看)
MariadbPort=3306 (資料庫所在的 port，預設為 3306)
TrackStocks_Market=006208 (追蹤的市值型股票)
TrackStocks_HighDividend=00929&0056 (追蹤的配息型股票)
```
1. 建立映像檔
```
sudo docker build -t "stockbot-long-backend" .
```
1. 執行容器
```
sudo docker run -p 8000:8000 --env-file .env --restart=always -d --name stockbot-long-backend stockbot-long-backend
```
## 交易邏輯
* 主攻 ETF 長線交易
* 市值型 (006208) 與配息型(0056, 00878, 00919, 00929) ETF 計數器分開計算
* 基本買賣邏輯:
  * 當股價來到一個月內最低點時固定買入 5000，操作後一個月內不再進行任何關於該型的買賣操作
  * 當股價來到三個月內最高點時固定賣出 5000，操作後一個月內不再進行任何關於該型的買賣操作
* 加減碼邏輯:
  * 當買入時，價格位於 180 日平均股價之上，少買 2000
  * 當買入時，價格位於 180 日平均股價之下，多買 3000
  * 當買入時，價格位於 360 日平均股價之下，再多買 2000
  * 當賣出時，價格位於 180 日平均股價之下，少賣 2000