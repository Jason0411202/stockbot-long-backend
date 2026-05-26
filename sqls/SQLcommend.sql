CREATE DATABASE IF NOT EXISTS StockLongData;
USE StockLongData;
CREATE TABLE IF NOT EXISTS StockHistory (
    stock_id VARCHAR(10) NOT NULL, -- 股票代號
    stock_name VARCHAR(50), -- 股票名稱
    date VARCHAR(50), -- 日期
    volume INT, -- 成交量
    value BIGINT, -- 成交值
    open_price DECIMAL(10, 2), -- 開盤價
    high_price DECIMAL(10, 2), -- 最高價
    low_price DECIMAL(10, 2), -- 最低價
    close_price DECIMAL(10, 2), -- 收盤價
    price_change DECIMAL(10, 2), -- 漲跌價差
    transactions INT, -- 成交筆數
    PRIMARY KEY (stock_id, date)
);

CREATE TABLE IF NOT EXISTS UnrealizedGainsLosses (
    transaction_date VARCHAR(50) NOT NULL, -- 交易日期
    stock_id VARCHAR(10) NOT NULL, -- 股票代號
    stock_name VARCHAR(50) NOT NULL, -- 股票名稱
    transaction_price DECIMAL(10, 2) NOT NULL, -- 交易價格
    investment_cost DECIMAL(12, 2) NOT NULL, -- 投資成本
    shares INT NOT NULL DEFAULT 0, -- 股數
    PRIMARY KEY (transaction_date, stock_id)
);
CREATE TABLE IF NOT EXISTS RealizedGainsLosses (
    buy_date DATE NOT NULL, -- 買入日期
    sell_date DATE NOT NULL, -- 賣出日期
    stock_id VARCHAR(10) NOT NULL, -- 股票代號
    stock_name VARCHAR(50) NOT NULL, -- 股票名稱
    purchase_price DECIMAL(10, 2) NOT NULL, -- 買入價格
    sell_price DECIMAL(10, 2) NOT NULL, -- 賣出價格
    investment_cost DECIMAL(12, 2) NOT NULL, -- 投資成本
    revenue DECIMAL(12, 2) NOT NULL, -- 總收益
    profit_loss DECIMAL(12, 2) NOT NULL, -- 損益
    profit_rate DECIMAL(10, 2) NOT NULL, -- 損益率
    shares INT NOT NULL DEFAULT 0, -- 賣出股數
    PRIMARY KEY (stock_id, buy_date, sell_date)
);

-- 向後相容：若資料表已存在但缺少 shares 欄位，則補上 (MariaDB 10.0.2+ / MySQL 8.0.29+)
ALTER TABLE UnrealizedGainsLosses ADD COLUMN IF NOT EXISTS shares INT NOT NULL DEFAULT 0;
ALTER TABLE RealizedGainsLosses ADD COLUMN IF NOT EXISTS shares INT NOT NULL DEFAULT 0;

-- BackfillStatus: 記錄某 (stock_id, month) 是否已「完整」抓取完畢。
-- 規則:整月 INSERT 全數成功才寫入；只記錄「已過完」的月份 (不會記錄 currentMonth)。
-- init/daily 流程在開抓前先查此表決定該月是否可跳過 TWSE API。
CREATE TABLE IF NOT EXISTS BackfillStatus (
    stock_id VARCHAR(10) NOT NULL, -- 股票代號
    month VARCHAR(7) NOT NULL, -- "YYYY-MM"
    completed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (stock_id, month)
);

-- BotState: 上線模式跨重啟用的 key/value 狀態。
-- 目前儲存:
--   last_processed_date   YYYY-MM-DD,引擎已處理過的最後一天 (decision watermark)
--   current_cash          字串化的 float64,引擎當前現金
-- 設計理由:上線模式啟動會 catch-up 回放 [watermark+1, latest] 的所有交易日,
-- 因此 watermark 必須跨重啟持久化以避免重複下單;cash 也必須持久化才能讓現金約束
-- 在多次重啟之間維持一致。
CREATE TABLE IF NOT EXISTS BotState (
    state_key VARCHAR(64) NOT NULL,
    state_value VARCHAR(256) NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (state_key)
);
