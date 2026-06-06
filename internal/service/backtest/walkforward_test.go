package backtest

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
	"math"
	"testing"
	"time"
)

// walkforward_test.go 為 walk-forward 評估與對照組的整合測試:as-of 查價、視窗產生、
// B&H / 同曝險 Blend 對照組、Calmar 比較、五道關卡彙整。

func TestCloseAsOf(t *testing.T) {
	// Arrange — 含非交易日空隙。
	s := &trading.StockSeries{
		Dates: []time.Time{
			mustDate(t, "2019-05-03"), mustDate(t, "2019-05-06"), mustDate(t, "2019-05-08"),
		},
		ClosePrices: []float64{30, 31, 32},
	}
	cases := []struct {
		name string
		day  string
		px   float64
		ok   bool
	}{
		{"non-trading day takes prior close", "2019-05-07", 31, true},
		{"pre-listing returns false", "2018-06-01", 0, false},
		{"exact trading day", "2019-05-08", 32, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			px, ok := s.CloseAsOf(mustDate(t, c.day))
			if px != c.px || ok != c.ok {
				t.Fatalf("CloseAsOf(%s) = (%v,%v), want (%v,%v)", c.day, px, ok, c.px, c.ok)
			}
		})
	}
}

func TestCommonSupportStart(t *testing.T) {
	// Arrange — B 較晚上市,其第 20 個交易日決定共同有效起點。
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(mustDate(t, "2019-01-01"), constPrices(50, 100)),
		"B": seriesFrom(mustDate(t, "2019-02-01"), constPrices(50, 100)),
	}
	cfg := baseCfg("A", "B")

	// Act
	got, ok := commonSupportStart(cfg, series)

	// Assert — B 的 Dates[19] = 2019-02-20。
	want := time.Date(2019, 2, 20, 0, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("commonSupportStart = (%s,%v), want (%s,true)", got.Format("2006-01-02"), ok, want.Format("2006-01-02"))
	}
}

func TestGenerateWindows_UniverseAndStep(t *testing.T) {
	// Arrange
	start := mustDate(t, "2019-01-01")
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(start, constPrices(800, 100)),
		"B": seriesFrom(start, constPrices(800, 100)),
	}
	cfg := baseCfg("A", "B")
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 3, MinTradeDays: 100}

	// Act
	windows := generateWindows(cfg, series, trading.CollectDateUnion(series), p)

	// Assert — 多個視窗、起點皆  2 檔可交易、長度約 12 月、起點嚴格遞增。
	if len(windows) < 3 {
		t.Fatalf("expected >=3 windows, got %d", len(windows))
	}
	for i, w := range windows {
		if u := len(tradableAt(cfg, series, w[0])); u != 2 {
			t.Fatalf("window %d universe = %d, want 2", i, u)
		}
		if yrs := yearsBetween(w[0], w[1]); yrs < 0.9 || yrs > 1.1 {
			t.Fatalf("window %d span = %.3f yrs, want ~1.0", i, yrs)
		}
		if i > 0 && !w[0].After(windows[i-1][0]) {
			t.Fatalf("window starts not strictly increasing at %d", i)
		}
	}
}

func TestTradableAt_OnlyListedStocks(t *testing.T) {
	// Arrange — A 已上市、B 之後才上市。
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(mustDate(t, "2020-01-01"), constPrices(10, 100)),
		"B": seriesFrom(mustDate(t, "2020-06-01"), constPrices(10, 100)),
	}
	cfg := baseCfg("A", "B")

	// Act + Assert — 2020-01-05 只有 A 可交易。
	if got := tradableAt(cfg, series, mustDate(t, "2020-01-05")); len(got) != 1 || got[0] != "A" {
		t.Fatalf("tradableAt = %v, want [A]", got)
	}
}

func TestDeployAllCash_EqualWeightWholeShares(t *testing.T) {
	// Arrange — 兩檔等權買滿;整股、餘額留現金。
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(mustDate(t, "2020-01-01"), constPrices(5, 100)),
		"B": seriesFrom(mustDate(t, "2020-01-01"), constPrices(5, 50)),
	}
	cfg := baseCfg("A", "B")
	positions := map[string]int{}
	cash := 1000.0

	// Act — 每檔分得 500:A@100→5 股、B@50→10 股,花 1000、餘 0。
	bought := deployAllCash(cfg, series, mustDate(t, "2020-01-01"), positions, &cash)

	// Assert
	if !bought || positions["A"] != 5 || positions["B"] != 10 || cash != 0 {
		t.Fatalf("deployAllCash: bought=%v A=%d B=%d cash=%.2f, want true/5/10/0", bought, positions["A"], positions["B"], cash)
	}
}

func TestBHImmediateArm_NoContribEqualsLumpSum(t *testing.T) {
	// Arrange — 升勢股 100→200,無注資。
	start := mustDate(t, "2020-01-01")
	series := map[string]*trading.StockSeries{"A": seriesFrom(start, linRamp(366, 100, 200))}
	windowDates := series["A"].Dates
	cfg := baseCfg("A")
	contribOnDay := make([]float64, len(windowDates))

	// Act
	arm := bhImmediateArm(cfg, series, windowDates, contribOnDay)
	m := armMetrics(arm, cfg.InitialCash)

	// Assert — 期初買滿 ~1,000,000、期末 ~2,000,000、MWR ~+100%/yr、曝險 ~100%。
	if !approxEq(arm.curve[0], 1_000_000, 1) {
		t.Fatalf("bh start equity = %.2f, want ~1000000", arm.curve[0])
	}
	if !approxEq(arm.finalEquity, 2_000_000, 1) {
		t.Fatalf("bh final equity = %.2f, want ~2000000", arm.finalEquity)
	}
	if !approxEq(m.MWR, 1.0, 0.02) {
		t.Fatalf("bh MWR = %.4f, want ~1.0", m.MWR)
	}
	if m.AvgExp < 0.99 {
		t.Fatalf("bh exposure = %.4f, want ~1.0", m.AvgExp)
	}
}

func TestBlendMetrics_WeightBounds(t *testing.T) {
	// Arrange
	bhNav := []float64{1.0, 1.2, 0.9, 1.3}
	base := mustDate(t, "2020-01-01")
	dates := []time.Time{base, base.AddDate(0, 0, 120), base.AddDate(0, 0, 240), base.AddDate(0, 0, 360)}
	contribOnDay := make([]float64, len(bhNav))

	// Act + Assert — w=0:全現金平盤 (MWR≈0、回撤 0)。
	m0 := blendMetrics(bhNav, 0.0, contribOnDay, 100000, dates)
	if m0.MaxDD != 0 || !approxEq(m0.MWR, 0, 1e-6) {
		t.Fatalf("blend(w=0) = MDD %.4f MWR %.6f, want 0/0", m0.MaxDD, m0.MWR)
	}
	// w=1:完全複製 B&H NAV (回撤、曝險相同)。
	m1 := blendMetrics(bhNav, 1.0, contribOnDay, 100000, dates)
	if !approxEq(m1.MaxDD, maxDrawdown(bhNav), 1e-9) || !approxEq(m1.AvgExp, 1.0, 1e-12) {
		t.Fatalf("blend(w=1) = MDD %.6f AvgExp %.6f, want %.6f/1.0", m1.MaxDD, m1.AvgExp, maxDrawdown(bhNav))
	}
}

func TestFlowsFromNav(t *testing.T) {
	// Arrange — NAV 翻倍、期中注資 50 (以前一日 NAV 換單位)。
	nav := []float64{1.0, 1.0, 2.0}
	base := mustDate(t, "2020-01-01")
	dates := []time.Time{base, base.AddDate(0, 0, 1), base.AddDate(0, 0, 2)}
	contribOnDay := []float64{0, 50, 0}

	// Act
	flows, finalEq, totalIn := flowsFromNav(nav, contribOnDay, 100, dates)

	// Assert — units = 100 + 50/1.0 = 150;finalEq = 150×2 = 300;totalIn = 150。
	if !approxEq(finalEq, 300, 1e-9) || !approxEq(totalIn, 150, 1e-9) {
		t.Fatalf("flowsFromNav finalEq=%.2f totalIn=%.2f, want 300/150", finalEq, totalIn)
	}
	// 現金流:期初 -100、注資 -50、期末 +300 共 3 筆。
	if len(flows) != 3 || flows[0].Amount != -100 || flows[len(flows)-1].Amount != 300 {
		t.Fatalf("flows = %+v, want [-100, -50, +300]", flows)
	}
}

func TestCalmarWin(t *testing.T) {
	cases := []struct {
		s, b           float64
		wantWin, wantC bool
	}{
		{1.2, 0.9, true, true},
		{0.5, 0.9, false, true},
		{math.Inf(1), 5.0, true, true},           // 策略無回撤、benchmark 有 → 策略贏且可比
		{math.Inf(1), math.Inf(1), false, false}, // 雙方皆無回撤 → 不可比
		{math.NaN(), 1.0, false, false},          // NaN → 不可比
	}
	for i, c := range cases {
		win, comp := calmarWin(c.s, c.b)
		if win != c.wantWin || comp != c.wantC {
			t.Fatalf("case %d calmarWin(%v,%v)=(%v,%v), want (%v,%v)", i, c.s, c.b, win, comp, c.wantWin, c.wantC)
		}
	}
}

// mkReport 建立可調的 WindowReport,供 aggregate 關卡測試精準控制輸入。
func mkReport(stratMWR, bhMWR, stratMDD, bhMDD, blendMWR float64, calmarBeatsBH, beatsBlend bool) WindowReport {
	part := math.NaN()
	if bhMWR != 0 {
		part = stratMWR / bhMWR
	}
	return WindowReport{
		Strat:            SeriesMetrics{MWR: stratMWR, MaxDD: stratMDD, Calmar: calmar(stratMWR, stratMDD), AvgExp: 0.5},
		BH:               SeriesMetrics{MWR: bhMWR, MaxDD: bhMDD, Calmar: calmar(bhMWR, bhMDD)},
		Blend:            SeriesMetrics{MWR: blendMWR},
		CalmarBeatsBH:    calmarBeatsBH,
		BeatsBlendBoth:   beatsBlend,
		RetParticipation: part,
	}
}

func TestAggregate_G1DownMarketGuard(t *testing.T) {
	// 空頭 (B&H 中位 <=0):改用方向性比較,少賠即 PASS。
	down := aggregate([]WindowReport{
		mkReport(-0.08, -0.10, -0.1, -0.2, 0, true, true),
		mkReport(-0.08, -0.10, -0.1, -0.2, 0, true, true),
	})
	if !down.G1RetParticipation {
		t.Fatalf("down-market: strat(-8%%) beats BH(-10%%) → G1 should PASS")
	}
	worse := aggregate([]WindowReport{
		mkReport(-0.15, -0.10, -0.1, -0.2, 0, false, false),
		mkReport(-0.15, -0.10, -0.1, -0.2, 0, false, false),
	})
	if worse.G1RetParticipation {
		t.Fatalf("down-market: strat(-15%%) worse than BH(-10%%) → G1 should FAIL")
	}
	// 多頭參與率語意:>=75% PASS;<75% FAIL。
	up := aggregate([]WindowReport{mkReport(0.09, 0.10, -0.1, -0.2, 0, true, true)})
	if !up.G1RetParticipation {
		t.Fatalf("up-market: 9%% >= 75%% of 10%% → G1 should PASS")
	}
	low := aggregate([]WindowReport{mkReport(0.05, 0.10, -0.1, -0.2, 0, false, false)})
	if low.G1RetParticipation {
		t.Fatalf("up-market: 5%% < 75%% of 10%% → G1 should FAIL")
	}
}

func TestAggregate_AllGatesPassAndFail(t *testing.T) {
	// Arrange — 全勝情境:報酬參與足、回撤遠小、Calmar 全勝、雙贏 Blend、最差回撤更淺。
	good := []WindowReport{
		mkReport(0.20, 0.20, -0.10, -0.40, 0.05, true, true),
		mkReport(0.18, 0.22, -0.12, -0.45, 0.04, true, true),
		mkReport(0.25, 0.20, -0.08, -0.50, 0.06, true, true),
	}
	// Act
	a := aggregate(good)
	// Assert — 五道關卡 + 綜合皆 PASS。
	if !(a.G1RetParticipation && a.G2RiskReduction && a.G3CalmarVsBH && a.G4Skill && a.G5Robustness && a.OverallPass) {
		t.Fatalf("expected all gates PASS, got %+v", a)
	}

	// Arrange — 全敗情境:回撤跟 B&H 一樣大、無 Calmar 勝、無 Blend 雙贏。
	bad := []WindowReport{
		mkReport(0.05, 0.20, -0.40, -0.40, 0.30, false, false),
		mkReport(0.04, 0.22, -0.45, -0.42, 0.28, false, false),
	}
	b := aggregate(bad)
	if b.G2RiskReduction || b.G3CalmarVsBH || b.G4Skill || b.OverallPass {
		t.Fatalf("expected risk/calmar/skill gates FAIL, got %+v", b)
	}
}

func TestAggregate_EmptyReports(t *testing.T) {
	// Arrange + Act
	a := aggregate(nil)
	// Assert — 不可 panic,視窗數 0。
	if a.NWindows != 0 {
		t.Fatalf("empty aggregate NWindows = %d, want 0", a.NWindows)
	}
}

func TestEvaluateWindow_StrategyVsBenchmarks(t *testing.T) {
	// Arrange — 升勢雙標的、含每月注資。
	start := mustDate(t, "2019-01-01")
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(start, linRamp(500, 50, 150)),
		"B": seriesFrom(start, linRamp(500, 30, 90)),
	}
	cfg := baseCfg("A", "B")
	cfg.MonthlyContribution = 2500
	allDates := trading.CollectDateUnion(series)

	// Act
	rep, err := evaluateWindow(cfg, series, allDates, allDates[0], allDates[len(allDates)-1])
	if err != nil {
		t.Fatalf("evaluateWindow: %v", err)
	}

	// Assert — 兩檔皆可交易、交易日數正確、B&H 幾乎滿倉、策略曝險 < B&H。
	if rep.Universe != 2 {
		t.Fatalf("universe = %d, want 2", rep.Universe)
	}
	if rep.TradeDays != len(allDates) {
		t.Fatalf("trade days = %d, want %d", rep.TradeDays, len(allDates))
	}
	if rep.BH.AvgExp < 0.95 {
		t.Fatalf("B&H exposure = %.3f, want ~1.0", rep.BH.AvgExp)
	}
	if rep.Strat.AvgExp >= rep.BH.AvgExp {
		t.Fatalf("strategy exposure %.3f should be below B&H %.3f", rep.Strat.AvgExp, rep.BH.AvgExp)
	}
}

func TestEvaluateFullSpan(t *testing.T) {
	// Arrange
	start := mustDate(t, "2019-01-01")
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(start, linRamp(400, 50, 150)),
		"B": seriesFrom(start, linRamp(400, 30, 90)),
	}
	cfg := baseCfg("A", "B")

	// Act
	rep, err := EvaluateFullSpan(cfg, series)
	if err != nil {
		t.Fatalf("EvaluateFullSpan: %v", err)
	}

	// Assert — 起點 = 共同有效起點 (含 MA 暖身),終點 = 最後資料日。
	cs, _ := commonSupportStart(cfg, series)
	if !rep.Start.Equal(cs) {
		t.Fatalf("full-span start = %s, want commonSupportStart %s", rep.Start.Format("2006-01-02"), cs.Format("2006-01-02"))
	}
}

func TestWalkForwardOnSeries_FlatSeriesNoTrades(t *testing.T) {
	// Arrange — 全平盤:策略零成交。
	start := mustDate(t, "2019-01-01")
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(start, constPrices(800, 100)),
		"B": seriesFrom(start, constPrices(800, 100)),
	}
	cfg := baseCfg("A", "B")
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 6, MinTradeDays: 100}

	// Act
	reports, agg, err := walkForwardOnSeries(cfg, series, p)
	if err != nil {
		t.Fatalf("walkForwardOnSeries: %v", err)
	}

	// Assert — 有視窗、且每個視窗策略零成交、回撤 0。
	if agg.NWindows == 0 || len(reports) == 0 {
		t.Fatalf("expected >=1 window")
	}
	for _, r := range reports {
		if r.Buys != 0 || r.Sells != 0 || r.Strat.MaxDD != 0 {
			t.Fatalf("flat series should not trade, got buys=%d sells=%d mdd=%v", r.Buys, r.Sells, r.Strat.MaxDD)
		}
	}
}

func TestWalkForwardOnSeries_AppliesDefaultParams(t *testing.T) {
	// Arrange — 傳零值參數,應套用預設 (24 月 / 3 月 / 200 日)。
	start := mustDate(t, "2019-01-01")
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(start, constPrices(900, 100)),
		"B": seriesFrom(start, constPrices(900, 100)),
	}
	cfg := baseCfg("A", "B")

	// Act
	_, agg, err := walkForwardOnSeries(cfg, series, WalkForwardParams{})

	// Assert — 預設參數下仍能切出視窗。
	if err != nil || agg.NWindows == 0 {
		t.Fatalf("default params should yield windows, got NWindows=%d err=%v", agg.NWindows, err)
	}
}

func TestWalkForwardOnSeries_InsufficientDataErrors(t *testing.T) {
	// Arrange — 資料過短,湊不出任何視窗。
	cfg := baseCfg("A")
	series := map[string]*trading.StockSeries{"A": seriesFrom(mustDate(t, "2019-01-01"), constPrices(60, 100))}

	// Act + Assert
	if _, _, err := walkForwardOnSeries(cfg, series, WalkForwardParams{WindowMonths: 24, StepMonths: 3, MinTradeDays: 200}); err == nil {
		t.Fatalf("expected error when no window can be formed")
	}
}

func TestWalkForwardOnSeries_RejectsNonBaseline(t *testing.T) {
	// Arrange
	cfg := baseCfg("A")
	cfg.ScalingStrategy = "X"
	series := map[string]*trading.StockSeries{"A": seriesFrom(mustDate(t, "2019-01-01"), constPrices(400, 100))}

	// Act + Assert
	if _, _, err := walkForwardOnSeries(cfg, series, WalkForwardParams{}); err == nil {
		t.Fatalf("expected error for non-Baseline strategy")
	}
}
