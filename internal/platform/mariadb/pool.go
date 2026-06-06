// Package mariadb 提供 MariaDB/MySQL 連線池建立與 schema 初始化的封裝。
package mariadb

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var schemaSQL string

// defaultSchema 為向後相容的預設資料庫名稱:舊的 DB_DSN 未包含 db name,
// 舊程式以每次呼叫 `USE StockLongData` 切換 schema。
const defaultSchema = "StockLongData"

// OpenPool 以 dsn 建立一個設定好連線池參數的 *sql.DB,並在回傳前以 Ping 驗證連線。
// dsn 為空字串時直接回傳錯誤,不嘗試開啟連線。
//
// 為向後相容,OpenPool 會 augment 傳入的 dsn (舊 DB_DSN 不含 db name):
//   - 若未指定 DBName,補上 "StockLongData" → 連線池一律 scope 到該 schema,
//     消除舊程式每次呼叫的 `USE StockLongData`。
//   - 設定 multiStatements=true → 讓內嵌的多語句 schema.sql 能一次執行。
//
// 刻意「不」設定 ParseTime:舊程式以 raw sql.Open 讀取,DATE/DATETIME 欄位以原始字串
// 回傳 (repository 全部掃描成 string)。若開啟 ParseTime,DATE 欄位會被轉成 time.Time 再
// 格式化為 RFC3339 ("2024-01-02T00:00:00Z"),會改變 /api/get_realized_gains_losses 的
// buy_date/sell_date wire 格式 —— 故維持與舊行為一致,不開啟。
//
// 既有的 DB_DSN 值 (不論是否已含這些參數) 都會被保留並正確化。
func OpenPool(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("OpenPool: dsn 不可為空字串")
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("OpenPool: 解析 dsn 失敗: %w", err)
	}
	if cfg.DBName == "" {
		cfg.DBName = defaultSchema
	}
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	cfg.Params["multiStatements"] = "true"
	dsn = cfg.FormatDSN()

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
