// cmd/db_probe/main_test.go 以 sqlmock 覆蓋 probe 函式的各執行分支 (schema 不存在 / table 不存在 / 完整列出 / 查詢失敗)。
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestProbe_SchemaMissing 驗證 StockLongData schema 不存在時 probe 提早結束並輸出正確訊息。
func TestProbe_SchemaMissing(t *testing.T) {
	// Arrange — StockLongData 不存在 → 提早返回。
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.schemata").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))

	// Act
	var out bytes.Buffer
	if err := probe(db, &out); err != nil {
		t.Fatalf("probe: %v", err)
	}

	// Assert
	if !strings.Contains(out.String(), "StockLongData exists: 0") {
		t.Fatalf("output = %q", out.String())
	}
}

// TestProbe_TableMissing 驗證 schema 存在但 StockHistory table 不存在時 probe 輸出正確訊息。
func TestProbe_TableMissing(t *testing.T) {
	// Arrange — schema 存在但 StockHistory table 不存在。
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.schemata").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	mock.ExpectExec("USE StockLongData").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("information_schema.tables").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))

	// Act
	var out bytes.Buffer
	if err := probe(db, &out); err != nil {
		t.Fatalf("probe: %v", err)
	}

	// Assert
	if !strings.Contains(out.String(), "StockHistory table exists: 0") {
		t.Fatalf("output = %q", out.String())
	}
}

// TestProbe_FullListing 驗證 schema 與 table 皆存在時 probe 正確列出所有股票統計。
func TestProbe_FullListing(t *testing.T) {
	// Arrange — schema + table 存在,列出兩檔統計。
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.schemata").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	mock.ExpectExec("USE StockLongData").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("information_schema.tables").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	mock.ExpectQuery("FROM StockHistory GROUP BY stock_id").
		WillReturnRows(sqlmock.NewRows([]string{"stock_id", "cnt", "mind", "maxd"}).
			AddRow("00631L", 1500, "2019-05-03", "2026-06-06").
			AddRow("00830", 1600, "2018-01-02", "2026-06-06"))

	// Act
	var out bytes.Buffer
	if err := probe(db, &out); err != nil {
		t.Fatalf("probe: %v", err)
	}

	// Assert
	s := out.String()
	if !strings.Contains(s, "00631L") || !strings.Contains(s, "00830") {
		t.Fatalf("listing missing stocks: %q", s)
	}
}

// TestProbe_SchemaQueryError 驗證 schema 查詢失敗時 probe 回傳錯誤。
func TestProbe_SchemaQueryError(t *testing.T) {
	// Arrange — schema 查詢失敗 → 回錯。
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("information_schema.schemata").WillReturnError(sqlmock.ErrCancelled)

	// Act + Assert
	if err := probe(db, &bytes.Buffer{}); err == nil {
		t.Fatalf("expected error on schema query failure")
	}
}
