package trading

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"testing"
	"time"
)

func TestRestoredDisplayValuation_UsesNewerFillUntilClose(t *testing.T) {
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	e := NewEngine(&config.Config{TrackStocks: []string{"AAA"}})
	e.SeedPosition("AAA", day, 100, 39.1)
	series := map[string]*StockSeries{"AAA": NewStockSeries([]time.Time{day.AddDate(0, 0, -1)}, []float64{38}, []float64{38.28}, nil, nil, nil)}
	if got := e.HoldingValueForDisplay(series, day); got != 3910 {
		t.Fatalf("stale monitoring valuation: %v", got)
	}
	if got := e.HoldingValueAsOf(series, day); got != 3828 {
		t.Fatalf("decision valuation changed: %v", got)
	}
	series["AAA"] = NewStockSeries([]time.Time{day}, []float64{39.1}, []float64{40}, nil, nil, nil)
	if got := e.HoldingValueForDisplay(series, day); got != 4000 {
		t.Fatalf("close must replace provisional fill: %v", got)
	}
}
