package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// main_test.go 以 sqlmock 覆蓋 probe 的各分支 (schema 不存在 / table 不存在 / 完整列出)。

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
