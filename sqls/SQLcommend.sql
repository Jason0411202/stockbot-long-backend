CREATE DATABASE StockLongData;
USE StockLongData;
CREATE TABLE StockHistory (
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
CREATE TABLE UnrealizedGainsLosses (
    transaction_date VARCHAR(50) NOT NULL, -- 交易日期
    stock_id VARCHAR(10) NOT NULL, -- 股票代號
    stock_name VARCHAR(50) NOT NULL, -- 股票名稱
    transaction_price DECIMAL(10, 2) NOT NULL, -- 交易價格
    investment_cost DECIMAL(10, 2) NOT NULL, -- 投資成本
    PRIMARY KEY (transaction_date, stock_id)
);
CREATE TABLE RealizedGainsLosses (
    buy_date DATE NOT NULL, -- 買入日期
    sell_date DATE NOT NULL, -- 賣出日期
    stock_id VARCHAR(10) NOT NULL, -- 股票代號
    stock_name VARCHAR(50) NOT NULL, -- 股票名稱
    purchase_price DECIMAL(10, 2) NOT NULL, -- 買入價格
    sell_price DECIMAL(10, 2) NOT NULL, -- 賣出價格
    investment_cost DECIMAL(10, 2) NOT NULL, -- 投資成本
    revenue DECIMAL(10, 2) NOT NULL, -- 總收益
    profit_loss DECIMAL(10, 2) NOT NULL, -- 損益
    profit_rate DECIMAL(5, 2) NOT NULL, -- 損益率
    PRIMARY KEY (stock_id, buy_date, sell_date)
);


