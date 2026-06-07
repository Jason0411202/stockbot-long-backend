// internal/platform/mariadb/pool_test.go 驗證 OpenPool 的輸入驗證及 schema.sql 內嵌正確性。
package mariadb

import (
	"strings"
	"testing"
)

// TestOpenPool_EmptyDSN 確認傳入空字串 dsn 時 OpenPool 回傳錯誤 (不嘗試連線)。
func TestOpenPool_EmptyDSN(t *testing.T) {
	db, err := OpenPool("")
	if err == nil {
		t.Fatal("OpenPool(\"\") 應回傳錯誤,但 err 為 nil")
	}
	if db != nil {
		t.Errorf("OpenPool(\"\") 失敗時應回傳 nil *sql.DB,卻得到 %v", db)
	}
}

// TestSchemaEmbedded 確認 schema.sql 已被正確內嵌,且包含預期的關鍵 DDL 字串。
func TestSchemaEmbedded(t *testing.T) {
	schema := SchemaSQL()
	if strings.TrimSpace(schema) == "" {
		t.Fatal("SchemaSQL() 為空,schema.sql 可能未正確內嵌")
	}

	wantSubstrings := []string{"CREATE DATABASE", "StockHistory"}
	for _, want := range wantSubstrings {
		if !strings.Contains(schema, want) {
			t.Errorf("SchemaSQL() 應包含 %q,但找不到", want)
		}
	}
}
