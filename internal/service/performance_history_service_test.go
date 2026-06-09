// internal/service/performance_history_service_test.go 驗證統一日期軸績效歷史:
// 回測欄位全期皆有、實盤欄位 go-live 前為 null、對齊日的損益恆等式正確,且取樣不超過上限。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// TestPerformanceHistoryService_UnifiedTimeline 驗證回測 + 實盤對齊同一日期軸的逐日點。
func TestPerformanceHistoryService_UnifiedTimeline(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	cfg.InitialCash = 100000
	cfg.MonthlyContribution = 0 // lump-sum → invested 恆為 100000
	start := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	series := &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": risingHistory(start, 1000, 100),
		"BBB": risingHistory(start, 1000, 50),
	}}
	// 在「序列最後一日」放一筆實盤快照 (等距取樣必含末點,故可穩定對齊)。
	lastDate := start.AddDate(0, 0, 999).Format("2006-01-02")
	eq := newFakeEquity()
	eq.list = []entity.EquitySnapshot{
		{Date: lastDate, Cash: 40000, HoldingValue: 110000, TotalEquity: 150000, CostBasis: 90000},
	}

	svc := NewPerformanceHistoryService(cfg, series, eq, newTestLogger())
	hist, err := svc.History(context.Background())
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) == 0 || len(hist) > maxEquityCurvePoints {
		t.Fatalf("history length out of bounds: %d (cap %d)", len(hist), maxEquityCurvePoints)
	}

	// 回測欄位每日皆有值;lump-sum 下 invested 恆為期初本金。
	first := hist[0]
	if first.Invested != 100000 || first.StratEquity <= 0 || first.BHEquity <= 0 {
		t.Fatalf("backtest fields not populated on first point: %+v", first)
	}
	// go-live 前 (首點無對應快照) 實盤欄位應為 nil。
	if first.TotalEquity != nil || first.Cash != nil || first.RealizedPnL != nil {
		t.Fatalf("live fields should be nil before go-live: %+v", first)
	}

	// 末點對齊快照:實盤欄位應填入並滿足 realized = total_pnl − unrealized 等恆等式。
	last := hist[len(hist)-1]
	if last.Date != lastDate {
		t.Fatalf("last point date = %q, want %q", last.Date, lastDate)
	}
	if last.TotalEquity == nil || *last.TotalEquity != 150000 {
		t.Fatalf("last total_equity = %v, want 150000", deref(last.TotalEquity))
	}
	if last.UnrealizedPnL == nil || *last.UnrealizedPnL != 20000 { // 110000 − 90000
		t.Fatalf("unrealized = %v, want 20000", deref(last.UnrealizedPnL))
	}
	if last.TotalPnL == nil || *last.TotalPnL != 50000 { // 150000 − 100000
		t.Fatalf("total_pnl = %v, want 50000", deref(last.TotalPnL))
	}
	if last.RealizedPnL == nil || *last.RealizedPnL != 30000 { // 50000 − 20000
		t.Fatalf("realized = %v, want 30000", deref(last.RealizedPnL))
	}
	if last.Multiple == nil || *last.Multiple != 1.5 { // 150000 / 100000
		t.Fatalf("multiple = %v, want 1.5", deref(last.Multiple))
	}
	if last.HoldingRatio == nil || *last.HoldingRatio != 73.33 { // 110000/150000*100
		t.Fatalf("holding_ratio = %v, want 73.33", deref(last.HoldingRatio))
	}
}

// TestPerformanceHistoryService_NoLiveAllNull 驗證無實盤快照時所有實盤欄位為 null、回測欄位仍完整。
func TestPerformanceHistoryService_NoLiveAllNull(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	start := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	series := &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": risingHistory(start, 600, 100),
		"BBB": risingHistory(start, 600, 50),
	}}
	svc := NewPerformanceHistoryService(cfg, series, newFakeEquity(), newTestLogger())

	hist, err := svc.History(context.Background())
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) == 0 {
		t.Fatalf("expected non-empty history")
	}
	for _, p := range hist {
		if p.StratEquity <= 0 || p.TotalEquity != nil {
			t.Fatalf("expected backtest-only point, got %+v", p)
		}
	}
}

// TestPerformanceHistoryService_SeriesErrorPropagates 驗證序列載入失敗時回傳錯誤 (不吞錯)。
func TestPerformanceHistoryService_SeriesErrorPropagates(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc := NewPerformanceHistoryService(cfg, &fakeSeriesLoader{err: errFake}, newFakeEquity(), newTestLogger())

	if _, err := svc.History(context.Background()); err == nil {
		t.Fatalf("expected error on series load failure")
	}
}

// deref 安全解參考 *float64 供測試錯誤訊息列印 (nil → NaN-ish 標示)。
func deref(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
