// cmd/db_probe/main.go 連線 MariaDB 並印出 StockHistory 各股票的資料筆數與日期範圍。
package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"

	"github.com/Jason0411202/stockbot-long-backend/internal/platform/mariadb"
)

// db_probe：只做連線 + 查詢 StockHistory 筆數，不做任何寫入/網路抓取。
func main() {
	_ = godotenv.Load(".env")
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DB_DSN not set")
		os.Exit(2)
	}

	db, err := mariadb.OpenPool(dsn) // 連線池 (含 Ping) 取代手刻 sql.Open
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(2)
	}
	defer db.Close()

	if err := probe(db, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// probe 查詢 StockLongData / StockHistory 是否存在並印出各股票的資料筆數與日期範圍。
// 抽離 main()(連線/exit 邏輯)讓核心查詢可用 sqlmock 測試;輸出寫入 out。
func probe(db *sql.DB, out io.Writer) error {
	fmt.Fprintln(out, "ping OK")

	// 確認 StockLongData schema 是否存在;不存在則提早結束。
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

	// 確認 StockHistory 資料表是否存在;不存在則提早結束。
	var tbl int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='StockLongData' AND table_name='StockHistory'`).Scan(&tbl); err != nil {
		return fmt.Errorf("table query: %w", err)
	}
	fmt.Fprintf(out, "StockHistory table exists: %d\n", tbl)
	if tbl == 0 {
		return nil
	}

	// 以 GROUP BY 查詢各股票的筆數與日期範圍並逐行印出。
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
