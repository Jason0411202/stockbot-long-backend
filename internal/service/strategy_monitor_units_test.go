// internal/service/strategy_monitor_units_test.go 驗證快照單位轉換及無成交日持久化。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// TestStrategyMonitor_ConvertsOnlyPricesAndShares 驗證分割轉換不改變金額、百分比與空值。
func TestStrategyMonitor_ConvertsOnlyPricesAndShares(t *testing.T) {
	number := func(v float64) *float64 { return &v }
	p := trading.DecisionPhase{Status: "blocked", IntendedShares: 5, CashBefore: 1000, CashAfter: 1000, HeldSharesBefore: 10, HeldSharesAfter: 10,
		Conditions: []trading.DecisionCondition{
			{ID: "entry", Value: number(400), Threshold: number(420), Unit: "price", Status: "passed"},
			{ID: "minimum", Value: number(5), Threshold: number(1), Unit: "shares", Status: "passed"},
			{ID: "cash", Value: number(1000), Threshold: number(400), Unit: "currency", Status: "passed"},
			{ID: "drawdown", Value: number(3), Threshold: number(8), Unit: "percent", Status: "blocked"},
			{ID: "no_value", Unit: "price", Status: "inactive"},
		},
		Metrics: []trading.DecisionMetric{{ID: "peak", Value: number(500), Unit: "price"}},
	}
	got := monitorPhase(p, 4)
	if *got.Conditions[0].Value != 100 || *got.Conditions[0].Threshold != 105 || *got.Conditions[1].Threshold != 4 || *got.Conditions[2].Threshold != 400 || *got.Conditions[3].Threshold != 8 || got.Conditions[4].Value != nil {
		t.Fatalf("unit conversion mismatch: %+v", got)
	}
	if got.IntendedShares != 20 || got.HeldSharesBefore != 40 || got.CashBefore != 1000 || *got.Metrics[0].Value != 125 || *p.Conditions[0].Value != 400 {
		t.Fatalf("phase conversion mutated source or changed economic value: %+v", got)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatal(err)
	}
}

// TestStrategyMonitor_PersistsNoTradeDay 驗證無成交日仍保存當日所有股票的真實阻擋條件。
func TestStrategyMonitor_PersistsNoTradeDay(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	cfg.DecisionPriceBasis = "open"
	s, _, state, _, ledger, _ := newTradingFixture(cfg)
	day := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	state.values[stateKeyWatermark] = day.AddDate(0, 0, -1).Format(dateLayout)
	s.series = unitSeriesLoader{&fakeSeriesLoader{}, marketunits.Book{Basis: marketunits.BasisDate, Actions: []marketunits.Action{{StockID: "AAA", Date: "2026-10-01", Ratio: 4}}}}
	series := map[string]*trading.StockSeries{"AAA": flatSeries(day.AddDate(0, 0, -60), 60, 100), "BBB": flatSeries(day.AddDate(0, 0, -60), 60, 100)}
	if err := s.commitDay(context.Background(), day, series, map[string]float64{"AAA": 100, "BBB": 100}, false); err != nil {
		t.Fatal(err)
	}
	got, err := NewStrategyMonitorService(state).StrategyMonitor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("two-stock strategy snapshot: %d UTF-8 bytes", len(state.values[stateKeyStrategyMonitor]))
	if len(ledger.lots) != 0 || len(got.Stocks) != 2 || got.DecisionDate != "2026-10-13" || got.Stocks[0].IndicatorDate != "2026-10-12" || *got.Stocks[0].DecisionPrice != 25 || *got.Stocks[1].DecisionPrice != 100 {
		t.Fatalf("no-trade snapshot mismatch: %+v", got)
	}
	for _, stock := range got.Stocks {
		if stock.Buy.Status != "blocked" || stock.Sell.Status != "inactive" || stock.Buy.ExecutedShares != 0 || len(stock.Buy.Conditions) == 0 || len(stock.Buy.Reasons) == 0 {
			t.Fatalf("missing conditions for %s: %+v", stock.StockID, stock)
		}
	}
}
