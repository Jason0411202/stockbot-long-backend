# stockbot-long-backend
## 執行
1. 確保伺服器中有 mariadb
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

2. 配置 .env 檔案
```
MariadbUser=使用者名稱
MariadbPassword=使用者密碼
MariadbHost=資料庫所在的 host
MariadbPort=資料庫所在的 port
TrackStocks_Market=006208 (追蹤的市值型股票)
TrackStocks_HighDividend=00929&0056 (追蹤的配息型股票)
```
1. 建立映像檔
```
sudo docker build -t "stockbot-long-backend" .
```
1. 執行容器
```
sudo docker run --env-file .env --restart=always -d --name stockbot-long-backend stockbot-long-backend
```
## 長線
* 主攻 ETF 交易
* 市值型 (006208) 與配息型(0056, 00878, 00919, 00929) ETF 計數器分開計算
* 基本買賣邏輯:
  * 當股價來到一個月內最低點時固定買入 5000，操作後一個月內不再進行任何關於該型的買賣操作
  * 當股價來到三個月內最高點時固定賣出 5000，操作後一個月內不再進行任何關於該型的買賣操作
* 加減碼邏輯:
  * 當買入時，價格位於 180 日平均股價之上，少買 2000
  * 當買入時，價格位於 180 日平均股價之下，多買 3000
  * 當買入時，價格位於 360 日平均股價之下，再多買 2000
  * 當賣出時，價格位於 180 日平均股價之下，少賣 2000