# stockbot-long-backend
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