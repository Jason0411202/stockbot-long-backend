package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// TWSE 抓取/解析 (fetchMonth/parseStockDay/parseF) 已統一在 internal/client/twse,
// 其解析行為由 internal/client/twse/twse_test.go 完整覆蓋。本檔僅測 fetch_data 自有的
// CSV 寫出邏輯。

// --- writeCSV: 依日期升冪寫出含表頭的 CSV ---

func TestWriteCSV(t *testing.T) {
	// Arrange: 故意亂序的 bars,驗證輸出有排序
	dir := t.TempDir()
	path := filepath.Join(dir, "TEST.csv")
	bars := map[string]entity.Bar{
		"2024-03-02": {Date: "2024-03-02", Open: 2, High: 3, Low: 1, Close: 2.5, Volume: 200},
		"2024-03-01": {Date: "2024-03-01", Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 100},
	}

	// Act
	if err := writeCSV(path, bars); err != nil {
		t.Fatalf("writeCSV 失敗: %v", err)
	}

	// Assert: 表頭 + 兩列且日期升冪
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("讀回 CSV 失敗: %v", err)
	}
	want := "date,open,high,low,close,volume\n" +
		"2024-03-01,1.0000,2.0000,0.5000,1.5000,100\n" +
		"2024-03-02,2.0000,3.0000,1.0000,2.5000,200\n"
	if string(data) != want {
		t.Errorf("CSV 內容不符\n got:\n%s\nwant:\n%s", data, want)
	}
}
