package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

// db_probe：只做連線 + 查詢 StockHistory 筆數，不做任何寫入/網路抓取。
func main() {
	_ = godotenv.Load(".env")
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DB_DSN not set")
		os.Exit(2)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(2)
	}
	db.SetConnMaxLifetime(time.Minute)
	if err := db.Ping(); err != nil {
		fmt.Fprintln(os.Stderr, "ping:", err)
		os.Exit(3)
	}

	if err := probe(db, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// probe 查詢 StockLongData / StockHistory 是否存在並印出各股票的資料筆數與日期範圍。
// 抽離 main()(連線/exit 邏輯)讓核心查詢可用 sqlmock 測試;輸出寫入 out。
func probe(db *sql.DB, out io.Writer) error {
	fmt.Fprintln(out, "ping OK")

	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='StockLongData'`).Scan(&exists); err != nil {
		return fmt.Errorf("schema query: %w", err)
	}
	fmt.Fprintf(out, "StockLongData exists: %d\n", exists)
	if exists == 0 {
		return nil
	}

	if _, err := db.Exec("USE StockLongData"); err != nil {
		return fmt.Errorf("USE: %w", err)
	}

	var tbl int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='StockLongData' AND table_name='StockHistory'`).Scan(&tbl); err != nil {
		return fmt.Errorf("table query: %w", err)
	}
	fmt.Fprintf(out, "StockHistory table exists: %d\n", tbl)
	if tbl == 0 {
		return nil
	}

	rows, err := db.Query(`SELECT stock_id, COUNT(*), MIN(date), MAX(date) FROM StockHistory GROUP BY stock_id`)
	if err != nil {
		return fmt.Errorf("count query: %w", err)
	}
	defer rows.Close()
	fmt.Fprintln(out, "stock_id | rows | min_date | max_date")
	for rows.Next() {
		var id, mind, maxd string
		var cnt int
		if err := rows.Scan(&id, &cnt, &mind, &maxd); err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err) // 錯誤走 stderr,不污染 out 的資料輸出
			continue
		}
		fmt.Fprintf(out, "%-8s | %-4d | %s | %s\n", id, cnt, mind, maxd)
	}
	return rows.Err()
}
