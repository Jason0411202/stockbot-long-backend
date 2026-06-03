# 資料庫 Schema

App 啟動時 (`InitDatabase`) 會自動執行 [`sqls/SQLcommend.sql`](../sqls/SQLcommend.sql) 建立資料庫與資料表，
所以不需要手動 run migration；本文件純粹是給人讀的 schema 說明。

## 資料庫總覽

- **Database name**：`StockLongData`
- **Engine / Charset**：跟著 MariaDB 容器預設 (InnoDB / utf8mb4)
- **共四張 table**：

| Table | 用途 | 寫入時機 |
| --- | --- | --- |
| `StockHistory` | 每檔股票的每日 OHLCV 歷史價量 | 啟動 + 每日從 TWSE API 回補 |
| `UnrealizedGainsLosses` | 尚未賣出的「持倉 lot」 | 觸發買入時 (`SQLBuyStock`) |
| `RealizedGainsLosses` | 已實現損益的成交紀錄 | 觸發賣出時 (`SQLSellStock`) |
| `BackfillStatus` | 標記 (stock, 月份) 是否「整月」回補完成 | 整月 INSERT 全數成功才寫入 |

下圖為四張 table 的關係 (邏輯關係，無實體 FK)：

```
                ┌─────────────────────┐
                │     StockHistory     │  ← TWSE 每日股價
                │  PK (stock_id, date) │
                └─────────────────────┘
                         ▲
                         │ stock_id
                         │
   ┌──────────────────────────────────┐
   │                                   │
   ▼                                   ▼
┌──────────────────────────┐   ┌──────────────────────────┐
│   UnrealizedGainsLosses   │   │    RealizedGainsLosses    │
│    (持倉中的 lot)          │   │    (已賣出的損益紀錄)        │
│  PK (transaction_date,    │   │  PK (stock_id,             │
│      stock_id)            │   │      buy_date, sell_date)  │
└──────────────────────────┘   └──────────────────────────┘
            │                              ▲
            └─────── 賣出 (SQLSellStock) ────┘

┌─────────────────────┐
│    BackfillStatus    │  ← 控制 TWSE API 的回補節奏，跟交易紀錄無關
│  PK (stock_id, month)│
└─────────────────────┘
```

---

## `StockHistory` — 股票歷史價量

每檔股票每個交易日一筆，由 TWSE `STOCK_DAY` API 回補。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `stock_id` | `VARCHAR(10)` | 股票代號 (如 `006208`、`00830`)，**PK 一部分** |
| `stock_name` | `VARCHAR(50)` | 股票名稱 (中文) |
| `date` | `VARCHAR(50)` | 交易日 (格式 `YYYY-MM-DD`，由 ROC 民國年轉成西元)，**PK 一部分** |
| `volume` | `INT` | 成交量 (股) |
| `value` | `BIGINT` | 成交值 (元) |
| `open_price` | `DECIMAL(10,2)` | 開盤價 |
| `high_price` | `DECIMAL(10,2)` | 最高價 |
| `low_price` | `DECIMAL(10,2)` | 最低價 |
| `close_price` | `DECIMAL(10,2)` | 收盤價，**回測 / 交易計算的基準** |
| `price_change` | `DECIMAL(10,2)` | 漲跌價差 |
| `transactions` | `INT` | 成交筆數 |

**Primary Key**：`(stock_id, date)` — 同一股票同一天只能有一筆，重複 INSERT 會被 `INSERT IGNORE` 吞掉。

**寫入路徑**：`fetchAndInsertMonth()` → 對 TWSE 拉一個月的 daily 資料 → 逐筆 `INSERT IGNORE INTO StockHistory ...`。

---

## `UnrealizedGainsLosses` — 持倉中的 lot

每次買入會產生一筆，**直到該 lot 全數賣出才會被刪除**。賣出時走 FIFO-by-cost (從成本最低開始賣)。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `transaction_date` | `VARCHAR(50)` | 買入日 (`YYYY-MM-DD`)，**PK 一部分** |
| `stock_id` | `VARCHAR(10)` | 股票代號，**PK 一部分** |
| `stock_name` | `VARCHAR(50)` | 股票名稱 (從 `StockHistory` 查最新一筆而來) |
| `transaction_price` | `DECIMAL(10,2)` | 買入當天收盤價 |
| `investment_cost` | `DECIMAL(12,2)` | 投資成本 = `transaction_price × shares` |
| `shares` | `INT` (default 0) | 持有股數 (賣一部分時會被 `UPDATE` 扣減) |

**Primary Key**：`(transaction_date, stock_id)` — 同一支股票同一天只允許一筆 lot。若 baseline 策略同日重複觸發買入，第二次會因 PK 衝突而失敗。

**讀取路徑**：
- `GetLowestUnrealizedGainsLossesRecord` 取「成本最低的 lot」用來決定下一筆要賣什麼
- `GetTransactionPriceOfUnrealizedGainsLosses` 取持倉中的 max/min 買入價，用於 baseline 加減碼判斷

---

## `RealizedGainsLosses` — 已實現損益

每次賣出 (整筆 / 部分) 會產生一筆，append-only，永不刪除。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `buy_date` | `DATE` | 對應 lot 的買入日，**PK 一部分** |
| `sell_date` | `DATE` | 賣出日，**PK 一部分** |
| `stock_id` | `VARCHAR(10)` | 股票代號，**PK 一部分** |
| `stock_name` | `VARCHAR(50)` | 股票名稱 |
| `purchase_price` | `DECIMAL(10,2)` | 該 lot 當初的買入價 |
| `sell_price` | `DECIMAL(10,2)` | 賣出當天收盤價 |
| `investment_cost` | `DECIMAL(12,2)` | 此次賣出對應的成本 (= `purchase_price × 賣出股數`) |
| `revenue` | `DECIMAL(12,2)` | 賣出收入 (= `sell_price × 賣出股數`) |
| `profit_loss` | `DECIMAL(12,2)` | 損益 = `revenue − investment_cost` |
| `profit_rate` | `DECIMAL(10,2)` | 損益率 (%) = `profit_loss / investment_cost × 100` |
| `shares` | `INT` (default 0) | 此次賣出的股數 |

**Primary Key**：`(stock_id, buy_date, sell_date)` — 「同一 lot 同一天分多次賣出」會 PK 衝突，目前流程不會發生 (一筆 sell 操作會把目標股數一次處理完)。

**讀取路徑**：`GetAllRealizedGainsLosses` 提供前端顯示損益清單。

---

## `BackfillStatus` — TWSE 回補完成標記

**用途**：避免每次 app 重啟都重跑整段 TWSE 歷史 (60 個月 × N 檔，~5–10 分鐘 + API rate limit 風險)。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `stock_id` | `VARCHAR(10)` | 股票代號，**PK 一部分** |
| `month` | `VARCHAR(7)` | 月份字串 `YYYY-MM`，**PK 一部分** |
| `completed_at` | `DATETIME` (default `CURRENT_TIMESTAMP`) | 標記時間 |

**Primary Key**：`(stock_id, month)`。

**寫入規則** (見 `fetchAndInsertMonth` / `markBackfillMonthComplete`)：

1. 整個月份的所有 daily 資料**全數** `INSERT IGNORE` 成功後，才寫入 `BackfillStatus`。
2. **當月 (current month) 永遠不寫入**，因為當月仍有未到的交易日。每次 init / daily 都會重抓當月以補上最新交易日。
3. 中途若任一筆 INSERT 失敗，整月不會被標記，下次 init 會重抓該月。

**讀取規則** (見 `updateDatabaseWithMonths`)：

- 啟動時呼叫 `getCompletedBackfillMonths(stockID)` 撈出該股票所有已完成月份。
- 對每個要回補的月份：若不是當月、且已在 `BackfillStatus` 內，就**跳過 TWSE API 呼叫**。
- 結果：第一次 init 會打完 60 月 × N 檔 的 API；之後重啟只會打「當月 + 任何上次中斷未完成的月份」。

> ⚠️ **手動修改注意**：若你 `DELETE FROM BackfillStatus WHERE ...`，下次 init 會重新去 TWSE 抓那幾個月。
> 反之若 TWSE 給的月份資料缺了一兩天，但整個月份 INSERT 都成功了，仍會被標記為完成 — 之後不會再補抓。
> 真要「強制重抓某月」就刪掉 `BackfillStatus` 對應 row 即可。

---

## 向後相容欄位

`SQLcommend.sql` 結尾兩段 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS shares ...` 是給「升級舊資料庫」用的：

```sql
ALTER TABLE UnrealizedGainsLosses ADD COLUMN IF NOT EXISTS shares INT NOT NULL DEFAULT 0;
ALTER TABLE RealizedGainsLosses   ADD COLUMN IF NOT EXISTS shares INT NOT NULL DEFAULT 0;
```

舊版本 (金額制) 的紀錄 `shares=0`，程式中 `GetAllUnrealizedGainsLosses` 會用 `transaction_price` 做相容換算；
新資料 `shares` 一律 > 0。需要 `MariaDB 10.0.2+` 或 `MySQL 8.0.29+` 才支援 `IF NOT EXISTS`。
