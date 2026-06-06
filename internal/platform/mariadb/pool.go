// Package mariadb 提供 MariaDB/MySQL 連線池建立與 schema 初始化的封裝。
package mariadb

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var schemaSQL string

// OpenPool 以 dsn 建立一個設定好連線池參數的 *sql.DB,並在回傳前以 Ping 驗證連線。
// dsn 為空字串時直接回傳錯誤,不嘗試開啟連線。
func OpenPool(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("OpenPool: dsn 不可為空字串")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("OpenPool: sql.Open 失敗: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("OpenPool: db.Ping 失敗: %w", err)
	}

	return db, nil
}

// InitSchema 對既有連線執行內嵌的 schema.sql,建立資料庫與相關 table。
//
// DSN must target the StockLongData schema and set multiStatements=true so the
// multi-statement schema executes in one call.
func InitSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("InitSchema: 執行 schema.sql 失敗: %w", err)
	}
	return nil
}

// SchemaSQL 回傳內嵌的 schema SQL 內容 (供測試使用)。
func SchemaSQL() string { return schemaSQL }
