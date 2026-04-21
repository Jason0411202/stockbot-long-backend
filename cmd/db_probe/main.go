package main

import (
	"database/sql"
	"fmt"
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
	fmt.Println("ping OK")

	// Does StockLongData exist?
	var exists int
	err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='StockLongData'`).Scan(&exists)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema query:", err)
		os.Exit(4)
	}
	fmt.Printf("StockLongData exists: %d\n", exists)

	if exists == 0 {
		return
	}

	if _, err := db.Exec("USE StockLongData"); err != nil {
		fmt.Fprintln(os.Stderr, "USE:", err)
		os.Exit(5)
	}

	var tbl int
	err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='StockLongData' AND table_name='StockHistory'`).Scan(&tbl)
	if err != nil {
		fmt.Fprintln(os.Stderr, "table query:", err)
		os.Exit(6)
	}
	fmt.Printf("StockHistory table exists: %d\n", tbl)

	if tbl == 0 {
		return
	}

	rows, err := db.Query(`SELECT stock_id, COUNT(*), MIN(date), MAX(date) FROM StockHistory GROUP BY stock_id`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "count query:", err)
		os.Exit(7)
	}
	defer rows.Close()
	fmt.Println("stock_id | rows | min_date | max_date")
	for rows.Next() {
		var id, mind, maxd string
		var cnt int
		if err := rows.Scan(&id, &cnt, &mind, &maxd); err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err)
			continue
		}
		fmt.Printf("%-8s | %-4d | %s | %s\n", id, cnt, mind, maxd)
	}
}
