package kernals

import (
	"testing"
	"time"
)

// EvaluateRollingOOS 應依錨定日把視窗切成 IS (錨定前) / OOS (錨定後),兩段皆有視窗,且 OOS 分成多折。
func TestEvaluateRollingOOS_AnchorsAndFolds(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	series := map[string]*stockSeries{
		"A": buildSeries(start, constPrices(1500, 100)),
		"B": buildSeries(start, constPrices(1500, 100)),
	}
	cfg := wfCfg([]string{"A", "B"})
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 3, MinTradeDays: 100}

	rep, err := EvaluateRollingOOS(cfg, series, p, 18, 6)
	if err != nil {
		t.Fatalf("EvaluateRollingOOS err: %v", err)
	}
	if rep.NIS == 0 || rep.NOOS == 0 {
		t.Fatalf("expected both IS and OOS windows, got IS=%d OOS=%d", rep.NIS, rep.NOOS)
	}
	if len(rep.Folds) < 2 {
		t.Fatalf("expected >=2 OOS folds, got %d", len(rep.Folds))
	}
	// 每折視窗起點皆 >= 錨定日 (OOS 時間順序);折按起點遞增。
	var prev time.Time
	for i, f := range rep.Folds {
		if f.FirstStart.Before(rep.Anchor) {
			t.Fatalf("fold %d 起點 %s 早於錨定日 %s", i, f.FirstStart.Format("2006-01-02"), rep.Anchor.Format("2006-01-02"))
		}
		if i > 0 && f.FirstStart.Before(prev) {
			t.Fatalf("折未按時間遞增")
		}
		prev = f.FirstStart
	}
}
