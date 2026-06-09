// internal/service/performance_service_test.go 驗證績效摘要的本金 / 實盤算式與回測區塊的 best-effort 行為。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// fakePerfPortfolio 模擬 portfolioReader,回傳預設的未實現 / 已實現損益列表或錯誤。
type fakePerfPortfolio struct {
	unreal []dto.UnrealizedGainLoss
	real   []dto.RealizedGainLoss
	uErr   error
	rErr   error
}

// UnrealizedGainsLosses 回傳預設未實現損益列表。
func (f *fakePerfPortfolio) UnrealizedGainsLosses(context.Context) ([]dto.UnrealizedGainLoss, error) {
	return f.unreal, f.uErr
}

// RealizedGainsLosses 回傳預設已實現損益列表。
func (f *fakePerfPortfolio) RealizedGainsLosses(context.Context) ([]dto.RealizedGainLoss, error) {
	return f.real, f.rErr
}

// TestPerformanceService_LiveSummaryMath 驗證本金明細、總權益、總損益與總報酬率的算式。
func TestPerformanceService_LiveSummaryMath(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.InitialCash = 100000
	cfg.MonthlyContribution = 2500
	state := newFakeState()
	state.values["current_cash"] = "50000"
	state.values["total_contributed"] = "10000"
	port := &fakePerfPortfolio{
		unreal: []dto.UnrealizedGainLoss{
			{NowValue: 60000, PredictProfitLoss: 5000},
			{NowValue: 20000, PredictProfitLoss: -1000},
		},
		real: []dto.RealizedGainLoss{
			{ProfitLoss: 3000},
			{ProfitLoss: -500},
		},
	}
	// 序列載入回錯 → 回測區塊為 nil,本測試只驗證實盤算式。
	series := &fakeSeriesLoader{err: errFake}

	svc := NewPerformanceService(cfg, port, state, series, newTestLogger())
	got, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	// 本金明細。
	if got.InitialCash != 100000 || got.MonthlyContribution != 2500 ||
		got.TotalContributed != 10000 || got.TotalInvested != 110000 {
		t.Fatalf("principal mismatch: %+v", got)
	}
	// 實盤現況:holdingValue=80000、unrealizedPnL=4000、realizedPnL=2500。
	if got.CurrentCash != 50000 || got.HoldingValue != 80000 {
		t.Fatalf("cash/holding mismatch: cash=%.2f holding=%.2f", got.CurrentCash, got.HoldingValue)
	}
	if got.UnrealizedPnL != 4000 || got.RealizedPnL != 2500 {
		t.Fatalf("pnl mismatch: unreal=%.2f real=%.2f", got.UnrealizedPnL, got.RealizedPnL)
	}
	// 總權益 = 50000+80000 = 130000;總損益 = 130000-110000 = 20000;報酬率 = 20000/110000 = 18.18%。
	if got.TotalEquity != 130000 || got.TotalPnL != 20000 || got.TotalReturnRate != 18.18 {
		t.Fatalf("equity/pnl/return mismatch: equity=%.2f pnl=%.2f rate=%.2f",
			got.TotalEquity, got.TotalPnL, got.TotalReturnRate)
	}
	// 資產配置比例:持股 80000/130000=61.54%、現金 50000/130000=38.46% (合計≈100)。
	if got.HoldingRatio != 61.54 || got.CashRatio != 38.46 {
		t.Fatalf("ratio mismatch: holding=%.2f cash=%.2f", got.HoldingRatio, got.CashRatio)
	}
	// 序列載入失敗 → 回測區塊應為 nil。
	if got.Backtest != nil {
		t.Fatalf("backtest should be nil when series load fails, got %+v", got.Backtest)
	}
}

// TestPerformanceService_CashFallbackToInitial 驗證 BotState 無 current_cash 時退回期初現金、無注資紀錄時 total=0。
func TestPerformanceService_CashFallbackToInitial(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.InitialCash = 100000
	state := newFakeState() // 無任何鍵
	port := &fakePerfPortfolio{}
	svc := NewPerformanceService(cfg, port, state, &fakeSeriesLoader{err: errFake}, newTestLogger())

	got, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.CurrentCash != 100000 || got.TotalContributed != 0 || got.TotalInvested != 100000 {
		t.Fatalf("fallback mismatch: cash=%.2f contributed=%.2f invested=%.2f",
			got.CurrentCash, got.TotalContributed, got.TotalInvested)
	}
	// 無持倉、無現金紀錄:總權益 = 期初本金、損益 0。
	if got.TotalEquity != 100000 || got.TotalPnL != 0 {
		t.Fatalf("empty portfolio mismatch: equity=%.2f pnl=%.2f", got.TotalEquity, got.TotalPnL)
	}
}

// TestPerformanceService_PortfolioErrorPropagates 驗證帳本讀取失敗時 Summary 回傳錯誤 (不吞錯)。
func TestPerformanceService_PortfolioErrorPropagates(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	port := &fakePerfPortfolio{uErr: errFake}
	svc := NewPerformanceService(cfg, port, newFakeState(), &fakeSeriesLoader{}, newTestLogger())

	if _, err := svc.Summary(context.Background()); err == nil {
		t.Fatalf("Summary should propagate portfolio read error")
	}
}

// TestPerformanceService_BacktestPopulated 驗證有足夠歷史序列時回測區塊被填入 (span、視窗、指標)。
func TestPerformanceService_BacktestPopulated(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	cfg.MonthlyContribution = 2500
	// 兩檔約 1000 連續日 (~33 月) 多頭序列,足以產生至少一個 24 月 walk-forward 視窗。
	start := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	series := &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": risingHistory(start, 1000, 100),
		"BBB": risingHistory(start, 1000, 50),
	}}
	port := &fakePerfPortfolio{}
	svc := NewPerformanceService(cfg, port, newFakeState(), series, newTestLogger())

	got, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Backtest == nil {
		t.Fatalf("backtest should be populated with sufficient data")
	}
	bt := got.Backtest
	if bt.SpanStart == "" || bt.SpanEnd == "" || bt.Years <= 0 || bt.TotalIn <= 0 {
		t.Fatalf("backtest span/principal not set: %+v", bt)
	}
	if bt.WalkForward.WindowMonths != 24 || bt.WalkForward.StepMonths != 3 {
		t.Fatalf("walk-forward params not reflected: %+v", bt.WalkForward)
	}
	if bt.WalkForward.NWindows < 1 {
		t.Fatalf("expected at least one walk-forward window, got %d", bt.WalkForward.NWindows)
	}
	// 權益曲線應被填入、點數受取樣上限約束,且末點對齊 span 終點 (期末權益)。
	if len(bt.EquityCurve) == 0 {
		t.Fatalf("equity_curve should be populated with sufficient data")
	}
	if len(bt.EquityCurve) > maxEquityCurvePoints {
		t.Fatalf("equity_curve exceeds sample cap %d: %d", maxEquityCurvePoints, len(bt.EquityCurve))
	}
	lastPt := bt.EquityCurve[len(bt.EquityCurve)-1]
	if lastPt.Date != bt.SpanEnd || lastPt.StratEquity <= 0 || lastPt.BHEquity <= 0 {
		t.Fatalf("equity_curve last point mismatch: %+v (spanEnd=%s)", lastPt, bt.SpanEnd)
	}
}
