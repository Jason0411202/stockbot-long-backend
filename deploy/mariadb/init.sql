-- deploy/mariadb/init.sql 在 MariaDB container 第一次初始化時執行。
-- 這段授權 app 使用者建立與操作 StockLongData schema。
GRANT ALL PRIVILEGES ON *.* TO 'exampleuser'@'%';
FLUSH PRIVILEGES;
