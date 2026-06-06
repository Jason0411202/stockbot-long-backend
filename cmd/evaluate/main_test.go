package main

import (
	"io"
	"math"
	"os"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
)

// captureStdout 把 os.Stdout 暫時導向丟棄,避免 print 系列污染測試輸出。
func captureStdout(t *testing.T) func() {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe 失敗: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	return func() {
		_ = w.Close()
		os.Stdout = orig
		<-done
		_ = r.Close()
	}
}

// --- 純格式化 (注意此版 pct/ratio 的填補空白與 eval_csv 版不同) ---

func TestPct(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.123, "+12.3%"},
		{-0.05, "-5.0%"},
		{math.NaN(), "  n/a"},
		{math.Inf(1), "  inf"},
		{math.Inf(-1), "  inf"},
	}
	for _, c := range cases {
		if got := pct(c.in); got != c.want {
			t.Errorf("pct(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRatio(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1.5, "1.50"},
		{-2.0, "-2.00"},
		{math.NaN(), " n/a"},
		{math.Inf(1), " inf"},
		{math.Inf(-1), "-inf"},
	}
	for _, c := range cases {
		if got := ratio(c.in); got != c.want {
			t.Errorf("ratio(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPartCell(t *testing.T) {
	cases := []struct {
		strat, bh float64
		want      string
	}{
		{0.1, 0.2, "0.50"},        // 正常: 0.1/0.2
		{0.1, 0, "   —"},          // B&H<=0 → 破折號
		{0.1, -0.05, "   —"},      // B&H<0 → 破折號
		{0.1, math.NaN(), "   —"}, // B&H NaN → 破折號
	}
	for _, c := range cases {
		if got := partCell(c.strat, c.bh); got != c.want {
			t.Errorf("partCell(%v,%v) = %q, want %q", c.strat, c.bh, got, c.want)
		}
	}
}

func TestYesNo(t *testing.T) {
	if got := yesNo(true); got != "✓" {
		t.Errorf("yesNo(true) = %q", got)
	}
	if got := yesNo(false); got != "·" {
		t.Errorf("yesNo(false) = %q", got)
	}
}

func TestPassFail(t *testing.T) {
	if got := passFail(true); got != "PASS ✅" {
		t.Errorf("passFail(true) = %q", got)
	}
	if got := passFail(false); got != "FAIL ❌" {
		t.Errorf("passFail(false) = %q", got)
	}
}

func TestVerdict(t *testing.T) {
	// 只驗證兩分支各回不同字串 (確保 if/else 都被執行)。
	if verdict(true) == verdict(false) {
		t.Fatalf("verdict 兩分支不應相同")
	}
	if verdict(true) == "" || verdict(false) == "" {
		t.Fatalf("verdict 不應為空字串")
	}
}

// --- print 系列:執行不 panic + 覆蓋率 ---

func sampleMetrics() backtest.SeriesMetrics {
	return backtest.SeriesMetrics{
		MWR: 0.12, MWROK: true, MaxDD: -0.2, Calmar: 0.6,
		Sortino: 1.1, AvgExp: 0.5, Multiple: 1.3,
	}
}

func sampleWindowReport() backtest.WindowReport {
	return backtest.WindowReport{
		Start: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC),
		Years: 2.0, TotalIn: 100000,
		Strat: sampleMetrics(), BH: sampleMetrics(), Blend: sampleMetrics(),
		CalmarBeatsBH: true, BeatsBlendBoth: false, RetParticipation: 1.0,
	}
}

func sampleAggregate(g4 bool) backtest.AggregateReport {
	return backtest.AggregateReport{
		NWindows:    3,
		MedStratMWR: 0.1, MedBHMWR: 0.12, MedBlendMWR: 0.08,
		MedStratMDD: -0.15, MedBHMDD: -0.3,
		MedStratCalmar: 0.7, MedBHCalmar: 0.4,
		MedStratAvgExp: 0.5, MedRetParticipation: 0.9,
		CalmarWinRate: 0.8, BlendSkillRate: 0.6,
		DispersionStratMWR: 0.05, WorstStratMWR: -0.1,
		WorstStratMDD: -0.25, WorstBHMDD: -0.4,
		G1RetParticipation: true, G2RiskReduction: true, G3CalmarVsBH: true,
		G4Skill: g4, G5Robustness: true, OverallPass: g4,
	}
}

func TestPrintReport(t *testing.T) {
	defer captureStdout(t)()
	cfg := &config.Config{
		TrackStocks: []string{"00631L", "00830"}, InitialCash: 50000, MonthlyContribution: 2500,
	}
	p := backtest.WalkForwardParams{WindowMonths: 24, StepMonths: 3, MinTradeDays: 200}
	reports := []backtest.WindowReport{sampleWindowReport(), sampleWindowReport()}
	// Act: 含有 reports → 走 len>0 + participation>0 分支
	printReport(cfg, p, reports, sampleAggregate(true))

	// 額外覆蓋: reports 為空 + MedBHMWR<=0 (participation "n/a")
	aggNoBH := sampleAggregate(false)
	aggNoBH.MedBHMWR = 0
	printReport(cfg, p, nil, aggNoBH)
}

func TestPrintDisclosures(t *testing.T) {
	defer captureStdout(t)()
	stocks := []string{"00631L"}
	// G4 通過分支
	printDisclosures(stocks, sampleAggregate(true))
	// G4 未通過分支
	printDisclosures(stocks, sampleAggregate(false))
}
