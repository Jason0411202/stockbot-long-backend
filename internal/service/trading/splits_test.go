package trading

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestOfficialSplitKeepsRealReturn(t *testing.T) {
	b := marketunits.Default()
	dates := []time.Time{time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)}
	closes := []float64{443.15, 19.17}
	opens := []float64{444, 19.75}
	vols := []float64{100, 2200}
	b.Normalize("00631L", dates, [][]float64{closes, opens}, vols)
	if math.Abs(closes[0]-443.15/22) > 1e-10 || closes[1] != 19.17 || vols[0] != 2200 {
		t.Fatalf("wrong normalization: %v %v", closes, vols)
	}
	if math.Abs(closes[1]/closes[0]-1) < 0.04 {
		t.Fatal("real resumed-session loss was erased")
	}
	s := NewStockSeries(dates, opens, closes, nil, nil, nil)
	s.SetSuspensions("00631L", b)
	if !s.Suspended(dates[0].AddDate(0, 0, 1)) || s.Suspended(dates[1]) {
		t.Fatal("wrong suspension interval")
	}
}

func TestSplitsDoNotChangeDecisionsCashPeaksOrRestart(t *testing.T) {
	cfg := baseCfg("TEST")
	cfg.DecisionPriceBasis = "open"
	start := mustDate(t, "2026-10-01")
	book := marketunits.Book{Basis: marketunits.BasisDate, Actions: []marketunits.Action{
		{StockID: "TEST", Date: "2026-12-01", Ratio: 2},
		{StockID: "TEST", Date: "2027-01-01", Ratio: 22},
		{StockID: "TEST", Date: "2027-02-01", Ratio: 1.0 / 7},
	}}
	dates := make([]time.Time, 200)
	raw := make([]float64, 200)
	prices := make([]float64, 200)
	for i := range dates {
		dates[i] = start.AddDate(0, 0, i)
		prices[i] = 100 + 30*math.Sin(float64(i)*0.045) + float64(i)/4
		raw[i] = prices[i] / book.Factor("TEST", dates[i].Format(time.DateOnly))
	}
	book.Normalize("TEST", dates, [][]float64{raw}, nil)
	base := map[string]*StockSeries{"TEST": NewStockSeries(dates, prices, prices, nil, nil, nil)}
	split := map[string]*StockSeries{"TEST": NewStockSeries(dates, raw, raw, nil, nil, nil)}
	want, got, online := NewEngine(cfg), NewEngine(cfg), NewEngine(cfg)
	for i, d := range dates {
		if err := want.ProcessDay(d, base, NoopExecutor{}); err != nil {
			t.Fatal(err)
		}
		if err := got.ProcessDay(d, split, NoopExecutor{}); err != nil {
			t.Fatal(err)
		}
		if err := online.ProcessOpenDecision(d, map[string]float64{"TEST": raw[i]}, split, NoopExecutor{}); err != nil {
			t.Fatal(err)
		}
		if i == 61 || i == 123 { // restart immediately after unit events
			restarted := NewEngine(cfg)
			restarted.SeedCash(online.Cash())
			for _, lot := range online.positions["TEST"] {
				restarted.SeedPosition("TEST", lot.date, lot.shares, lot.price)
			}
			restarted.SeedRiskState("TEST", online.RiskState("TEST"))
			online = restarted
		}
		for _, e := range []*Engine{got, online} {
			if math.Abs(e.Cash()-want.Cash()) > 1e-7 || math.Abs(e.HoldingValueAsOf(split, d)-want.HoldingValueAsOf(base, d)) > 1e-7 || math.Abs(e.CostBasis()-want.CostBasis()) > 1e-7 {
				t.Fatalf("split changed accounting on %s", d)
			}
			wr, gr := want.RiskState("TEST"), e.RiskState("TEST")
			if !wr.LastBuy.Equal(gr.LastBuy) || !wr.LastTrailSell.Equal(gr.LastTrailSell) || math.Abs(wr.Peak-gr.Peak) > 1e-9 {
				t.Fatal("split changed risk anchors")
			}
		}
	}
	if want.Stats().TotalBuys == 0 || want.Stats().TotalSells == 0 {
		t.Fatal("fixture did not exercise both buys and sells")
	}
	if !reflect.DeepEqual(want.Stats(), got.Stats()) {
		t.Fatal("different trading decisions")
	}
}
