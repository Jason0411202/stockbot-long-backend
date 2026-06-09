# 資料庫 Schema

應用程式使用 MariaDB，預設 database 名稱為 `StockLongData`。schema 來源是 [internal/platform/mariadb/schema.sql](../internal/platform/mariadb/schema.sql)，啟動時由 `mariadb.InitSchema` 執行。

## 資料表總覽

| Table | 用途 | 主要寫入者 |
| --- | --- | --- |
| `StockHistory` | 保存 TWSE 每日 OHLCV 歷史資料 | `MarketDataService` |
| `UnrealizedGainsLosses` | 保存目前仍持有的 lot | `PortfolioService.BuyShares` |
| `RealizedGainsLosses` | 保存已賣出的損益紀錄 | `PortfolioService.SellShares` |
| `BackfillStatus` | 記錄某檔股票某月份是否已完整回補 | `MarketDataService` |
| `BotState` | 保存線上交易跨重啟狀態 | `TradingService` |
| `EquityHistory` | 保存每個交易日的真實帳戶權益快照（供歷史權益折線圖） | `TradingService` |

## `StockHistory`

`StockHistory` 保存 TWSE `STOCK_DAY` API 回傳的日資料。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `stock_id` | `VARCHAR(10)` | 股票代號，PK |
| `stock_name` | `VARCHAR(50)` | 股票名稱 |
| `date` | `VARCHAR(50)` | 交易日期，格式為 `YYYY-MM-DD`，PK |
| `volume` | `INT` | 成交量 |
| `value` | `BIGINT` | 成交值 |
| `open_price` | `DECIMAL(10,2)` | 開盤價 |
| `high_price` | `DECIMAL(10,2)` | 最高價 |
| `low_price` | `DECIMAL(10,2)` | 最低價 |
| `close_price` | `DECIMAL(10,2)` | 收盤價 |
| `price_change` | `DECIMAL(10,2)` | 漲跌價差 |
| `transactions` | `INT` | 成交筆數 |

Primary key 是 `(stock_id, date)`。寫入使用 `INSERT IGNORE`，同一天同一檔重複抓取不會覆蓋既有資料。

## `UnrealizedGainsLosses`

`UnrealizedGainsLosses` 保存目前仍持有的每一筆買進 lot。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `transaction_date` | `VARCHAR(50)` | 買進日期，PK |
| `stock_id` | `VARCHAR(10)` | 股票代號，PK |
| `stock_name` | `VARCHAR(50)` | 股票名稱 |
| `transaction_price` | `DECIMAL(10,2)` | 買進價格 |
| `investment_cost` | `DECIMAL(12,2)` | 投入成本 |
| `shares` | `INT` | 持有股數 |

Primary key 是 `(transaction_date, stock_id)`。賣出時會從成本最低的 lot 開始扣股，與交易引擎的記憶體排序一致。

## `RealizedGainsLosses`

`RealizedGainsLosses` 保存已賣出的交易紀錄。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `buy_date` | `DATE` | 對應 lot 的買進日期，PK |
| `sell_date` | `DATE` | 賣出日期，PK |
| `stock_id` | `VARCHAR(10)` | 股票代號，PK |
| `stock_name` | `VARCHAR(50)` | 股票名稱 |
| `purchase_price` | `DECIMAL(10,2)` | 買進價格 |
| `sell_price` | `DECIMAL(10,2)` | 賣出價格 |
| `investment_cost` | `DECIMAL(12,2)` | 賣出股數對應的成本 |
| `revenue` | `DECIMAL(12,2)` | 賣出收入 |
| `profit_loss` | `DECIMAL(12,2)` | 已實現損益 |
| `profit_rate` | `DECIMAL(10,2)` | 損益率百分比 |
| `shares` | `INT` | 賣出股數 |

Primary key 是 `(stock_id, buy_date, sell_date)`。

## `BackfillStatus`

`BackfillStatus` 記錄某檔股票某月份是否已完整抓取。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `stock_id` | `VARCHAR(10)` | 股票代號，PK |
| `month` | `VARCHAR(7)` | 月份，格式 `YYYY-MM`，PK |
| `completed_at` | `DATETIME` | 完成時間 |

只會標記已過完的月份，當月資料不標記完成，讓每日更新可以繼續補最新交易日。

## `BotState`

`BotState` 是線上模式跨重啟用的 key/value 狀態表。

| key | value 說明 |
| --- | --- |
| `last_processed_date` | 引擎已處理過的最後交易日，格式 `YYYY-MM-DD` |
| `current_cash` | 引擎目前現金，字串化 float64 |
| `total_contributed` | 累計每月注資總額（不含期初），字串化 float64 |

這些值讓服務重啟後能從正確日期接續 catch-up、維持 no-borrow 現金約束，並提供 API 本金明細。

## `EquityHistory`

`EquityHistory` 保存線上引擎逐日寫入的真實帳戶權益快照，供 `/api/get_equity_history` 繪製歷史權益折線圖。

| 欄位 | 型別 | 說明 |
| --- | --- | --- |
| `date` | `VARCHAR(10)` | 交易日，格式 `YYYY-MM-DD`，PK |
| `cash` | `DECIMAL(14,2)` | 當日閒置現金（未投入股市的預備現金） |
| `holding_value` | `DECIMAL(14,2)` | 當日持股市值 |
| `total_equity` | `DECIMAL(14,2)` | 當日總權益 = 現金 + 持股市值 |
| `cost_basis` | `DECIMAL(14,2)` | 當日持倉總成本（供 `/api/get_performance_history` 還原未實現損益 = 持股市值 − 成本基礎） |
| `updated_at` | `DATETIME` | 最後更新時間 |

Primary key 是 `date`。catch-up 回放與每日 loop 皆以 `date` upsert（`RecordEquity`），故同一天重覆處理會覆寫而非重複插入。
此表為**真實帳本走勢**，與回測 `equity_curve` 不同：全新部署（清空 BotState + 帳本）首跑會從 common issuance 回放補齊全期；
既有部署升級後從升級點起累積（過往日期不回填，與 `total_contributed` 的「只累計新月份」一致）。
