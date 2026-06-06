package main

import (
	"os"
	"path/filepath"
	"testing"
)

// --- parseF: 字串轉 float (去逗號/X、處理 "--"/空) ---

func TestParseF(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"123.45", 123.45},
		{"1000", 1000},
		{"", 0},
		{"--", 0},
		{"12.5X", 12.5}, // 含 "X" 後綴 (除權息標記) 須剝除
		{"abc", 0},      // 無法解析 → 0
		{"0", 0},
	}
	for _, c := range cases {
		if got := parseF(c.in); got != c.want {
			t.Errorf("parseF(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// --- writeCSV: 依日期升冪寫出含表頭的 CSV ---

func TestWriteCSV(t *testing.T) {
	// Arrange: 故意亂序的 bars,驗證輸出有排序
	dir := t.TempDir()
	path := filepath.Join(dir, "TEST.csv")
	bars := map[string]bar{
		"2024-03-02": {date: "2024-03-02", open: 2, high: 3, low: 1, close: 2.5, volume: 200},
		"2024-03-01": {date: "2024-03-01", open: 1, high: 2, low: 0.5, close: 1.5, volume: 100},
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

// --- parseStockDay: TWSE STOCK_DAY JSON body 解析 (從 fetchMonth 抽出,純函式可離線測) ---

func TestParseStockDay_OK(t *testing.T) {
	// Arrange: TWSE 欄位序 = [日期, 成交量(idx1), 成交金額, 開(idx3), 高(idx4), 低(idx5), 收(idx6), 漲跌, 筆數]
	// 一筆有效 + 一筆收盤無效(應跳過) + 一筆欄位不足(應跳過) + 一筆 ROC 日期壞掉(應跳過)
	body := []byte(`{"stat":"OK","data":[
		["113/03/01","1,000","9999","10.00","11.00","9.00","10.50","+0.5","100"],
		["113/03/02","2,000","9999","--","--","--","--","X0.00","100"],
		["short"],
		["bad-date","1","9999","1","1","1","1","+0","1"]
	]}`)

	// Act
	bars, err := parseStockDay(body)
	if err != nil {
		t.Fatalf("parseStockDay 失敗: %v", err)
	}

	// Assert: 只有第一筆有效
	if len(bars) != 1 {
		t.Fatalf("應只剩 1 筆有效資料, got %d: %+v", len(bars), bars)
	}
	b := bars[0]
	if b.date != "2024-03-01" || b.close != 10.50 || b.high != 11.00 || b.volume != 1000 {
		t.Errorf("解析欄位錯誤: %+v", b)
	}
}

func TestParseStockDay_Errors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"非OK狀態", `{"stat":"無資料"}`},
		{"無data欄位", `{"stat":"OK"}`},
		{"壞JSON", `{not json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseStockDay([]byte(c.body)); err == nil {
				t.Errorf("%s: 預期 error 但得 nil", c.name)
			}
		})
	}
}

// --- bar 結構基本欄位 ---

func TestBarStruct(t *testing.T) {
	b := bar{date: "2024-01-01", open: 1, high: 2, low: 0.5, close: 1.5, volume: 100}
	if b.date != "2024-01-01" || b.close != 1.5 || b.volume != 100 {
		t.Errorf("bar 欄位賦值異常: %+v", b)
	}
}
