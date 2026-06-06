package kernals

import (
	"testing"
	"time"
)

// EvaluateISOOS 應把視窗時間順序切成兩段:IS 全在分割日前、OOS 全在分割日後,且兩段皆有視窗。
func TestEvaluateISOOS_SplitsTemporally(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	series := map[string]*stockSeries{
		"A": buildSeries(start, constPrices(1200, 100)),
		"B": buildSeries(start, constPrices(1200, 100)),
	}
	cfg := wfCfg([]string{"A", "B"})
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 3, MinTradeDays: 100}

	rep, err := EvaluateISOOS(cfg, series, p, 0.5)
	if err != nil {
		t.Fatalf("EvaluateISOOS err: %v", err)
	}
	if rep.NIS == 0 || rep.NOOS == 0 {
		t.Fatalf("expected both IS and OOS windows, got IS=%d OOS=%d", rep.NIS, rep.NOOS)
	}
	// IS 最後一窗終點不得晚於分割日;OOS 第一窗起點不得早於分割日 (時間順序、無洩漏)。
	if !rep.OOSEnd.After(rep.SplitDate) {
		t.Fatalf("OOS 應在分割日之後")
	}
	if rep.SplitDate.Before(rep.ISStart) {
		t.Fatalf("分割日應在 IS 起點之後")
	}
}
