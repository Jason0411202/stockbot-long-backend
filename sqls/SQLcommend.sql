CREATE DATABASE StockLongData;
USE StockLongData;
CREATE TABLE StockHistory (
    stock_id VARCHAR(10) NOT NULL,
    stock_name VARCHAR(50),
    date VARCHAR(50),
    volume INT,
    value BIGINT,
    open_price DECIMAL(10, 2),
    high_price DECIMAL(10, 2),
    low_price DECIMAL(10, 2),
    close_price DECIMAL(10, 2),
    price_change DECIMAL(10, 2),
    transactions INT,
    PRIMARY KEY (stock_id, date)
);
