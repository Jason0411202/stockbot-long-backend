// internal/service/statistic_service_test.go 驗證 StatisticService 的低點天數、高點天數計算及股票統計資料彙整邏輯。
package service

import (
	"context"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
)

// TestLowerPointDays_Cases 以表格測試驗證 lowerPointDays 在各種收盤價序列下回傳正確的天數或哨兵值。
func TestLowerPointDays_Cases(t *testing.T) {
	tests := []struct {
		name   string
		prices []float64
		want   int
	}{
		{"empty", nil, 0},
		{"none lower", []float64{10, 10, 12, 15}, noPointSentinel},
		{"found at index 2", []float64{10, 10, 8, 12}, 2},
		{"found immediately", []float64{10, 9}, 1},
		{"all equal", []float64{10, 10, 10}, noPointSentinel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerPointDays(tt.prices); got != tt.want {
				t.Fatalf("lowerPointDays(%v) = %d, want %d", tt.prices, got, tt.want)
			}
		})
	}
}

// TestUpperPointDays_Cases 以表格測試驗證 upperPointDays 在各種收盤價序列下回傳正確的天數或哨兵值。
func TestUpperPointDays_Cases(t *testing.T) {
	tests := []struct {
		name   string
		prices []float64
		want   int
	}{
		{"empty", nil, 0},
		{"none higher", []float64{15, 15, 12, 10}, noPointSentinel},
		{"found at index 3", []float64{10, 10, 10, 12}, 3},
		{"found immediately", []float64{10, 11}, 1},
		{"all equal", []float64{10, 10, 10}, noPointSentinel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upperPointDays(tt.prices); got != tt.want {
				t.Fatalf("upperPointDays(%v) = %d, want %d", tt.prices, got, tt.want)
			}
		})
	}
}

// TestStockStatisticData 驗證 StockStatisticData 正確彙整股票代碼、名稱、今日收盤及高低點天數。
func TestStockStatisticData(t *testing.T) {
	stock := newFakeStock()
	stock.names["00631L"] = "元大台灣50正2"
	stock.prices["00631L"] = 30
	// newest-first: today=30; first below at index 2; first above at index 1
	stock.closesDesc["00631L"] = []float64{30, 31, 28, 25}

	cfg := &config.Config{TrackStocks: []string{"00631L"}}
	svc := NewStatisticService(stock, cfg, newTestLogger())

	rows, err := svc.StockStatisticData(context.Background())
	if err != nil {
		t.Fatalf("StockStatisticData: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	r := rows[0]
	if r.StockID != "00631L" || r.StockName != "元大台灣50正2" || r.TodayPrice != 30 {
		t.Fatalf("identity/price fields wrong: %+v", r)
	}
	if r.LowerPointDays != 2 {
		t.Fatalf("lower_point_days wrong: got %d want 2", r.LowerPointDays)
	}
	if r.UpperPointDays != 1 {
		t.Fatalf("upper_point_days wrong: got %d want 1", r.UpperPointDays)
	}
}

// TestStockStatisticData_EmptySeriesSentinels 驗證收盤價序列為空時高低點天數皆回傳 0。
func TestStockStatisticData_EmptySeriesSentinels(t *testing.T) {
	stock := newFakeStock()
	stock.names["X"] = "n"
	stock.prices["X"] = 0
	stock.closesDesc["X"] = nil // empty -> both day counts 0

	cfg := &config.Config{TrackStocks: []string{"X"}}
	svc := NewStatisticService(stock, cfg, newTestLogger())

	rows, err := svc.StockStatisticData(context.Background())
	if err != nil {
		t.Fatalf("StockStatisticData: %v", err)
	}
	if rows[0].LowerPointDays != 0 || rows[0].UpperPointDays != 0 {
		t.Fatalf("empty series should give 0/0, got lower=%d upper=%d", rows[0].LowerPointDays, rows[0].UpperPointDays)
	}
}

// TestStockStatisticData_NoneFoundSentinels 驗證收盤價全部相等時高低點天數皆回傳 noPointSentinel。
func TestStockStatisticData_NoneFoundSentinels(t *testing.T) {
	stock := newFakeStock()
	stock.names["X"] = "n"
	stock.prices["X"] = 10
	stock.closesDesc["X"] = []float64{10, 10, 10} // never crosses -> 36500 both

	cfg := &config.Config{TrackStocks: []string{"X"}}
	svc := NewStatisticService(stock, cfg, newTestLogger())

	rows, err := svc.StockStatisticData(context.Background())
	if err != nil {
		t.Fatalf("StockStatisticData: %v", err)
	}
	if rows[0].LowerPointDays != noPointSentinel || rows[0].UpperPointDays != noPointSentinel {
		t.Fatalf("flat series should give 36500/36500, got lower=%d upper=%d", rows[0].LowerPointDays, rows[0].UpperPointDays)
	}
}

// TestStockStatisticData_PropagatesNameError 驗證取得股票名稱失敗時錯誤向上傳遞。
func TestStockStatisticData_PropagatesNameError(t *testing.T) {
	stock := newFakeStock()
	stock.nameErr = errFake
	cfg := &config.Config{TrackStocks: []string{"X"}}
	svc := NewStatisticService(stock, cfg, newTestLogger())

	if _, err := svc.StockStatisticData(context.Background()); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}
