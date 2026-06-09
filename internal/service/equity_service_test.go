// internal/service/equity_service_test.go 驗證實盤每日權益歷史的映射、等距取樣與錯誤傳遞。
package service

import (
	"context"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// TestEquityHistoryService_MapsAll 驗證點數未超過上限時逐筆映射並保留順序與首末值。
func TestEquityHistoryService_MapsAll(t *testing.T) {
	eq := newFakeEquity()
	eq.list = []entity.EquitySnapshot{
		{Date: "2024-01-02", Cash: 1000, HoldingValue: 2000, TotalEquity: 3000},
		{Date: "2024-01-03", Cash: 900, HoldingValue: 2200, TotalEquity: 3100},
	}
	svc := NewEquityHistoryService(eq, newTestLogger())

	got, err := svc.EquityHistory(context.Background())
	if err != nil {
		t.Fatalf("EquityHistory: %v", err)
	}
	if len(got) != 2 || got[0].Date != "2024-01-02" || got[0].HoldingValue != 2000 || got[1].TotalEquity != 3100 {
		t.Fatalf("map mismatch: %+v", got)
	}
}

// TestEquityHistoryService_Downsamples 驗證點數超過上限時等距取樣壓至上限內,且首末點保留 (末點為期末權益)。
func TestEquityHistoryService_Downsamples(t *testing.T) {
	eq := newFakeEquity()
	n := maxEquityCurvePoints*3 + 7 // 明顯超過上限且非整除,逼出取樣與補末點路徑
	snaps := make([]entity.EquitySnapshot, n)
	for i := range snaps {
		snaps[i] = entity.EquitySnapshot{TotalEquity: float64(i)}
	}
	eq.list = snaps
	svc := NewEquityHistoryService(eq, newTestLogger())

	got, err := svc.EquityHistory(context.Background())
	if err != nil {
		t.Fatalf("EquityHistory: %v", err)
	}
	if len(got) == 0 || len(got) > maxEquityCurvePoints {
		t.Fatalf("downsample out of bounds: %d (cap %d)", len(got), maxEquityCurvePoints)
	}
	if got[0].TotalEquity != 0 || got[len(got)-1].TotalEquity != float64(n-1) {
		t.Fatalf("first/last not preserved: first=%+v last=%+v", got[0], got[len(got)-1])
	}
}

// TestEquityHistoryService_EmptyReturnsEmptySlice 驗證無資料時回傳空切片 (非 nil)。
func TestEquityHistoryService_EmptyReturnsEmptySlice(t *testing.T) {
	svc := NewEquityHistoryService(newFakeEquity(), newTestLogger())

	got, err := svc.EquityHistory(context.Background())
	if err != nil {
		t.Fatalf("EquityHistory: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice, got %v", got)
	}
}

// TestEquityHistoryService_ErrorPropagates 驗證讀取失敗時回傳錯誤 (不吞錯)。
func TestEquityHistoryService_ErrorPropagates(t *testing.T) {
	eq := newFakeEquity()
	eq.listErr = errFake
	svc := NewEquityHistoryService(eq, newTestLogger())

	if _, err := svc.EquityHistory(context.Background()); err == nil {
		t.Fatalf("expected error to propagate")
	}
}
