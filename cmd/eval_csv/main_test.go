package main

import (
	"io"
	"math"
	"os"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/config"
	"github.com/Jason0411202/stockbot-long-backend/kernals"
)

// captureStdout 暫時把 os.Stdout 導向丟棄,避免 print 系列函式污染測試輸出。
// 回傳一個還原函式,呼叫端 defer 它即可。
func captureStdout(t *testing.T) func() {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe 失敗: %v", err)
	}
	os.Stdout = w
	// 持續排空 pipe,避免 writer 在大量輸出時被 buffer 卡住。
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

// --- 純格式化函式 (含 NaN/Inf 邊界) ---

func TestPct(t *testing.T) {
	// Arrange / Act / Assert: 一般值、NaN、±Inf
	cases := []struct {
		in   float64
		want string
	}{
		{0.1234, "+12.3%"},
		{-0.05, "-5.0%"},
		{0, "+0.0%"},
		{nan(), "n/a"},
		{posInf(), "inf"},
		{negInf(), "inf"}, // pct 對 ±Inf 都回 "inf"
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
		{0, "0.00"},
		{nan(), "n/a"},
		{posInf(), "inf"},
		{negInf(), "-inf"},
	}
	for _, c := range cases {
		if got := ratio(c.in); got != c.want {
			t.Errorf("ratio(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMoney(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0"},
		{999, "$999"},
		{1000, "$1,000"},
		{1234567, "$1,234,567"},
		{-2500, "-$2,500"},
		{12.7, "$13"}, // 四捨五入到整數
	}
	for _, c := range cases {
		if got := money(c.in); got != c.want {
			t.Errorf("money(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPassFail(t *testing.T) {
	// Assert: true→PASS, false→FAIL (含 emoji 字首即可)
	if got := passFail(true); got != "PASS ✅" {
		t.Errorf("passFail(true) = %q", got)
	}
	if got := passFail(false); got != "FAIL ❌" {
		t.Errorf("passFail(false) = %q", got)
	}
}

func TestGateStr(t *testing.T) {
	// Arrange: 5 道關卡通過 3 道
	a := kernals.AggregateReport{
		G1RetParticipation: true,
		G2RiskReduction:    false,
		G3CalmarVsBH:       true,
		G4Skill:            false,
		G5Robustness:       true,
	}
	// Act / Assert
	if got := gateStr(a); got != "3/5" {
		t.Errorf("gateStr = %q, want 3/5", got)
	}
	if got := gateStr(kernals.AggregateReport{}); got != "0/5" {
		t.Errorf("gateStr(zero) = %q, want 0/5", got)
	}
}

// --- print 系列:只求執行不 panic + 覆蓋率 ---

func sampleMetrics() kernals.SeriesMetrics {
	return kernals.SeriesMetrics{
		MWR: 0.12, MWROK: true, MaxDD: -0.2, Calmar: 0.6,
		Sortino: 1.1, AvgExp: 0.5, Multiple: 1.3,
	}
}

func sampleWindowReport() kernals.WindowReport {
	return kernals.WindowReport{
		Start: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Years: 4.0, TotalIn: 100000,
		Strat: sampleMetrics(), BH: sampleMetrics(), Blend: sampleMetrics(),
		Buys: 10, Sells: 4, BHBuys: 5, TrailSells: 2, ProfitSells: 2,
		StratFinalCash:   1500,
		RetParticipation: 1.1,
	}
}

func sampleAggregate() kernals.AggregateReport {
	return kernals.AggregateReport{
		NWindows:    6,
		MedStratMWR: 0.1, MedBHMWR: 0.12, MedBlendMWR: 0.08,
		MedStratMDD: -0.15, MedBHMDD: -0.3,
		MedStratCalmar: 0.7, MedBHCalmar: 0.4,
		MedStratAvgExp: 0.5, MedRetParticipation: 0.9,
		CalmarWinRate: 0.8, BlendSkillRate: 0.6,
		DispersionStratMWR: 0.05, WorstStratMWR: -0.1,
		WorstStratMDD: -0.25, WorstBHMDD: -0.4,
		G1RetParticipation: true, G2RiskReduction: true, G3CalmarVsBH: true,
		G4Skill: true, G5Robustness: false, OverallPass: true,
	}
}

func TestPrintHeadline(t *testing.T) {
	defer captureStdout(t)()
	// Arrange
	cfg := &config.Config{
		TrackStocks: []string{"00631L", "00830"}, InitialCash: 50000, MonthlyContribution: 2500,
	}
	// Act + Assert (不 panic 即可)
	printHeadline(cfg, sampleWindowReport())
}

func TestPrintWalkForward(t *testing.T) {
	defer captureStdout(t)()
	cfg := &config.Config{TrackStocks: []string{"00631L"}}
	printWalkForward(cfg, 24, 3, sampleAggregate())
}

func TestPrintRollingOOS(t *testing.T) {
	defer captureStdout(t)()
	// Arrange: 構造一份含多折 (含一折 Calmar 為 NaN/Inf 走邊界) 的報告
	is := sampleAggregate()
	oos := sampleAggregate()
	r := kernals.RollingOOSReport{
		ISMonths: 36, FoldMonths: 12,
		Anchor: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		NIS:    4, NOOS: 6,
		IS: is, OOS: oos,
		Folds: []kernals.OOSFold{
			{
				FirstStart: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
				LastEnd:    time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC),
				N:          3, Calmar: 0.5, CalmarWin: 0.66, GatesPass: 3,
			},
			{
				FirstStart: time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC),
				LastEnd:    time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
				N:          3, Calmar: nan(), CalmarWin: 0.0, GatesPass: 1, // NaN 折走 IsNaN 分支
			},
		},
	}
	// Act + Assert
	printRollingOOS(r)

	// 額外覆蓋: OOS 嚴重退化 → "過擬合疑慮" 分支 (IS Calmar 高、OOS 極低)
	r2 := r
	r2.IS.MedStratCalmar = 1.0
	r2.OOS.MedStratCalmar = 0.1
	printRollingOOS(r2)

	// 額外覆蓋: "尚可,略退化" 分支 (OOS 介於 0.6~0.8 IS)
	r3 := r
	r3.IS.MedStratCalmar = 1.0
	r3.OOS.MedStratCalmar = 0.7
	r3.Folds = []kernals.OOSFold{
		{FirstStart: r.Anchor, LastEnd: r.Anchor, N: 1, Calmar: 0.9, CalmarWin: 1.0, GatesPass: 5},
	}
	printRollingOOS(r3)
}

func TestRow2AndRow(t *testing.T) {
	defer captureStdout(t)()
	// 直接呼叫 row / row2 確保覆蓋 (僅排版)。
	row("label", "a", "b")
	row2("label", "a", "b")
}

// 小工具: 產生特殊浮點值 (集中於此,讓上方 case 表更乾淨)。
func nan() float64    { return math.NaN() }
func posInf() float64 { return math.Inf(1) }
func negInf() float64 { return math.Inf(-1) }
