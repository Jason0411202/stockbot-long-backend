// internal/service/stockhistory_service_test.go 驗證 StockHistoryService 將資料庫歷史記錄正確轉換為日期-收盤價資料點序列。
package service

import (
	"context"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// TestStockHistoryData_MapsDateAndClose 驗證 StockHistoryData 將歷史記錄正確對應為日期與收盤價資料點。
func TestStockHistoryData_MapsDateAndClose(t *testing.T) {
	stock := newFakeStock()
	stock.history = map[string][]entity.StockHistory{
		"00631L": {
			{Date: "2024-01-01", ClosePrice: 100.5},
			{Date: "2024-01-02", ClosePrice: 101.0},
		},
	}
	svc := NewStockHistoryService(stock, newTestLogger())

	got, err := svc.StockHistoryData(context.Background(), "00631L")
	if err != nil {
		t.Fatalf("StockHistoryData: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Date != "2024-01-01" || got[0].Price != 100.5 {
		t.Fatalf("point[0] = %+v, want {2024-01-01 100.5}", got[0])
	}
	if got[1].Price != 101.0 {
		t.Fatalf("point[1].Price = %v, want 101.0", got[1].Price)
	}
}

// TestStockHistoryData_Empty 驗證查詢不存在的股票代碼時回傳空切片且不回傳錯誤。
func TestStockHistoryData_Empty(t *testing.T) {
	svc := NewStockHistoryService(newFakeStock(), newTestLogger())
	got, err := svc.StockHistoryData(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
