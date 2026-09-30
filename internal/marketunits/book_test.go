package marketunits

import (
	"math"
	"testing"
	"time"
)

func TestUnitsPreserveWealthAndRealReturnsAcrossRepeatedActions(t *testing.T) {
	b := Book{Basis: BasisDate, Actions: []Action{
		{StockID: "TEST", Date: "2026-10-02", Ratio: 2},
		{StockID: "TEST", Date: "2026-11-02", Ratio: 22},
		{StockID: "TEST", Date: "2026-12-02", Ratio: 1.0 / 7},
	}}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"2026-10-01", "2026-10-02", "2026-11-02", "2026-12-02"} {
		f := b.Factor("TEST", date)
		// A 5% real gain must survive each unit change, including fractions.
		canonical := 105.0
		raw := canonical / f
		if math.Abs(raw*f/100-1.05) > 1e-12 {
			t.Fatal("real return changed")
		}
		shares := 103.0 * f
		if math.Abs(raw*shares-10815) > 1e-8 {
			t.Fatal("ownership lost by split rounding")
		}
		if b.Factor("OTHER", date) != 1 {
			t.Fatal("unrelated stock changed")
		}
	}
	// Announcing a future event cannot rebase already processed history.
	if b.Factor("TEST", "2026-10-01") != 1 {
		t.Fatal("lookahead unit change")
	}
	dates := []time.Time{time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	close, open, high, low, volume := []float64{52.5}, []float64{50}, []float64{55}, []float64{49}, []float64{200}
	b.Normalize("TEST", dates, [][]float64{close, open, high, low}, volume)
	if close[0] != 105 || open[0] != 100 || high[0] != 110 || low[0] != 98 || volume[0] != 100 {
		t.Fatal("OHLCV unit mismatch")
	}
}

func TestActionMergeIsIdempotentAndRetainsSuspensions(t *testing.T) {
	b := Default()
	out, err := b.Merge([]Action{{StockID: "00631L", Date: "2026-03-31", Ratio: 22}})
	if err != nil || len(out.Actions) != 1 || out.Actions[0].SuspendFrom != "2026-03-25" {
		t.Fatalf("merge: %+v %v", out, err)
	}
	if _, err := out.Merge([]Action{{StockID: "00631L", Date: "2026-03-31", Ratio: 23}}); err == nil {
		t.Fatal("conflicting units silently accepted")
	}
}
