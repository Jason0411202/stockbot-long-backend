-- Grant the application user full privileges so it can run
-- `CREATE DATABASE IF NOT EXISTS StockLongData;` (executed from sqls/SQLcommend.sql)
-- and operate on any database the app needs.
GRANT ALL PRIVILEGES ON *.* TO 'exampleuser'@'%';
FLUSH PRIVILEGES;
