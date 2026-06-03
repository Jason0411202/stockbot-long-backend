package kernals

import (
	"main/config"
	"math"
	"testing"
	"time"
)

// buildSeries 由起始日 + 連續日曆日收盤價建立 stockSeries (ma20 與 loadStockSeries 同邏輯)。
func buildSeries(start time.Time, prices []float64) *stockSeries {
	n := len(prices)
	dates := make([]time.Time, n)
	idx := make(map[string]int, n)
	ma20 := make([]float64, n)
	const window = 20
	sum := 0.0
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i)
		dates[i] = d
		idx[d.Format("2006-01-02")] = i
		sum += prices[i]
		if i >= window {
			sum -= prices[i-window]
		}
		if i >= window-1 {
			ma20[i] = sum / float64(window)
		} else {
			ma20[i] = math.NaN()
		}
	}
	cp := make([]float64, n)
	copy(cp, prices)
	return &stockSeries{dates: dates, dateIndex: idx, closePrices: cp, ma20: ma20}
}

func constPrices(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func wfCfg(stocks []string) *config.Config {
	return &config.Config{
		TrackStocks:               stocks,
		ScalingStrategy:           "Baseline",
		BuyAndSellMultiplier:      1.0,
		CooldownDays:              14,
		BaselineBuyTiers:          []config.BaselineBuyTier{{Above: -0.1, Amount: 500}},
		BaselineBuyFallbackAmount: 3000,
		BaselineSellAmount:        10000,
		BaselineSellThreshold:     1.0,
		InitialCash:               100000,
	}
}

func TestCloseAsOf(t *testing.T) {
	s := &stockSeries{
		dates: []time.Time{
			time.Date(2019, 5, 3, 0, 0, 0, 0, time.UTC),
			time.Date(2019, 5, 6, 0, 0, 0, 0, time.UTC),
			time.Date(2019, 5, 8, 0, 0, 0, 0, time.UTC),
		},
		closePrices: []float64{30, 31, 32},
	}
	// 非交易日 2019-05-07 -> 取 <= 該日最近收盤 = 2019-05-06 的 31
	if px, ok := s.closeAsOf(time.Date(2019, 5, 7, 0, 0, 0, 0, time.UTC)); !ok || px != 31 {
		t.Fatalf("closeAsOf(05-07) = (%v,%v), want (31,true)", px, ok)
	}
	// 上市前 -> (0,false),無未來資訊
	if px, ok := s.closeAsOf(time.Date(2018, 6, 1, 0, 0, 0, 0, time.UTC)); ok || px != 0 {
		t.Fatalf("closeAsOf(pre-listing) = (%v,%v), want (0,false)", px, ok)
	}
	// 剛好交易日 -> 當日收盤
	if px, ok := s.closeAsOf(time.Date(2019, 5, 8, 0, 0, 0, 0, time.UTC)); !ok || px != 32 {
		t.Fatalf("closeAsOf(05-08) = (%v,%v), want (32,true)", px, ok)
	}
}

func TestCommonSupportStart(t *testing.T) {
	series := map[string]*stockSeries{
		"A": buildSeries(time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), constPrices(50, 100)),
		"B": buildSeries(time.Date(2019, 2, 1, 0, 0, 0, 0, time.UTC), constPrices(50, 100)),
	}
	cfg := wfCfg([]string{"A", "B"})
	got, ok := commonSupportStart(cfg, series)
	if !ok {
		t.Fatal("expected common support found")
	}
	// B 較晚上市,其第 20 個交易日 (dates[19]) = 2019-02-20
	want := time.Date(2019, 2, 20, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("commonSupportStart = %s, want %s", got.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

func TestGenerateWindows_UniverseAndStep(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	series := map[string]*stockSeries{
		"A": buildSeries(start, constPrices(800, 100)),
		"B": buildSeries(start, constPrices(800, 100)),
	}
	cfg := wfCfg([]string{"A", "B"})
	allDates := collectDateUnion(series)
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 3, DCAEveryDays: 21, MinTradeDays: 100}
	windows := generateWindows(cfg, series, allDates, p)
	if len(windows) < 3 {
		t.Fatalf("expected >=3 windows, got %d", len(windows))
	}
	for i, w := range windows {
		// 每個視窗起點皆 2 檔可交易
		if u := len(tradableAt(cfg, series, w[0])); u != 2 {
			t.Fatalf("window %d universe = %d, want 2", i, u)
		}
		// 視窗長度約 12 個月
		yrs := yearsBetween(w[0], w[1])
		if yrs < 0.9 || yrs > 1.1 {
			t.Fatalf("window %d span = %.3f years, want ~1.0", i, yrs)
		}
		// 起點遞增
		if i > 0 && !w[0].After(windows[i-1][0]) {
			t.Fatalf("window starts not strictly increasing at %d", i)
		}
	}
}

func TestLumpSumBenchmark_RisingStock(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]float64, 366) // day0..day365
	for i := range prices {
		prices[i] = 100 + 100*float64(i)/365.0 // 100 -> 200 線性
	}
	series := map[string]*stockSeries{"A": buildSeries(start, prices)}
	windowDates := series["A"].dates
	bh := lumpSumBenchmark(series, windowDates, []string{"A"}, 100000)

	// 起點 px=100 -> 1000 股 = 100000;終點 px=200 -> 200000
	if math.Abs(bh.curve[0]-100000) > 1 {
		t.Fatalf("bh start equity = %.2f, want ~100000", bh.curve[0])
	}
	if math.Abs(bh.curve[len(bh.curve)-1]-200000) > 1 {
		t.Fatalf("bh end equity = %.2f, want ~200000", bh.curve[len(bh.curve)-1])
	}
	years := yearsBetween(windowDates[0], windowDates[len(windowDates)-1])
	m := curveMetrics(bh.curve, 100000, years)
	if math.Abs(m.CAGR-1.0) > 0.02 {
		t.Fatalf("bh CAGR = %.4f, want ~1.0 (+100%%/yr)", m.CAGR)
	}
	// deployed XIRR 對 lump-sum 應 == pool CAGR (全數投入、無餘額)
	x, ok := xirr(bh.deployed)
	if !ok || math.Abs(x-1.0) > 0.02 {
		t.Fatalf("bh deployed XIRR = (%.4f,%v), want ~1.0", x, ok)
	}
}

func TestExposureMatchedBlend_ZeroWeightIsFlat(t *testing.T) {
	bhCurve := []float64{100000, 120000, 90000, 130000}
	blend := exposureMatchedBlend(bhCurve, 0.0, 100000)
	for i, v := range blend {
		if math.Abs(v-100000) > 1e-9 {
			t.Fatalf("blend[%d] = %.4f, want 100000 (w=0 -> 全現金)", i, v)
		}
	}
	// w=1 應完全複製 B&H 曲線
	full := exposureMatchedBlend(bhCurve, 1.0, 100000)
	for i := range bhCurve {
		if math.Abs(full[i]-bhCurve[i]) > 1e-6 {
			t.Fatalf("blend(w=1)[%d] = %.4f, want %.4f", i, full[i], bhCurve[i])
		}
	}
}

func TestCalmarWin(t *testing.T) {
	cases := []struct {
		s, b           float64
		wantWin, wantC bool
	}{
		{1.2, 0.9, true, true},
		{0.5, 0.9, false, true},
		{math.Inf(1), 5.0, true, true},           // 策略無回撤、benchmark 有 -> 策略贏且可比
		{math.Inf(1), math.Inf(1), false, false}, // 雙方皆無回撤 -> 不可比 (排除)
		{math.NaN(), 1.0, false, false},          // NaN -> 不可比
	}
	for i, c := range cases {
		win, comp := calmarWin(c.s, c.b)
		if win != c.wantWin || comp != c.wantC {
			t.Fatalf("case %d calmarWin(%v,%v) = (%v,%v), want (%v,%v)", i, c.s, c.b, win, comp, c.wantWin, c.wantC)
		}
	}
}

// mkReport 建立一個只填 G1 相關欄位 (Strat/BH CAGR、MaxDD) 的最小 WindowReport。
func mkReport(stratCAGR, bhCAGR float64) WindowReport {
	return WindowReport{
		Strat: SeriesMetrics{CAGR: stratCAGR, MaxDD: -0.10},
		BH:    SeriesMetrics{CAGR: bhCAGR, MaxDD: -0.20},
	}
}

// G1「守住 B&H 七成報酬」在空頭 (B&H 中位 CAGR <= 0) 時必須改用方向性比較,
// 不可因 0.75×負值抬高門檻而把『少賠的策略』誤判 FAIL。
func TestAggregateG1_DownMarketGuard(t *testing.T) {
	// 空頭:B&H -10%,策略 -8% (少賠) -> G1 應 PASS
	down := aggregate([]WindowReport{mkReport(-0.08, -0.10), mkReport(-0.08, -0.10), mkReport(-0.08, -0.10)})
	if !down.G1RetParticipation {
		t.Fatalf("down-market: strat(-8%%) beats BH(-10%%), G1 should PASS, got FAIL")
	}
	// 空頭:策略賠更多 (-15% vs -10%) -> G1 應 FAIL
	worse := aggregate([]WindowReport{mkReport(-0.15, -0.10), mkReport(-0.15, -0.10), mkReport(-0.15, -0.10)})
	if worse.G1RetParticipation {
		t.Fatalf("down-market: strat(-15%%) worse than BH(-10%%), G1 should FAIL, got PASS")
	}
	// 多頭參與率語意維持:+9% vs +10% (>=75%) PASS;+5% vs +10% (<75%) FAIL
	up := aggregate([]WindowReport{mkReport(0.09, 0.10), mkReport(0.09, 0.10), mkReport(0.09, 0.10)})
	if !up.G1RetParticipation {
		t.Fatalf("up-market: strat 9%% >= 75%% of BH 10%%, G1 should PASS")
	}
	low := aggregate([]WindowReport{mkReport(0.05, 0.10), mkReport(0.05, 0.10), mkReport(0.05, 0.10)})
	if low.G1RetParticipation {
		t.Fatalf("up-market: strat 5%% < 75%% of BH 10%%, G1 should FAIL")
	}
}

// 全平盤序列 (策略零成交) 不可造成 panic,且能正常產生彙整。
func TestWalkForward_FlatSeries_NoPanic(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	series := map[string]*stockSeries{
		"A": buildSeries(start, constPrices(800, 100)),
		"B": buildSeries(start, constPrices(800, 100)),
	}
	cfg := wfCfg([]string{"A", "B"})
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 6, DCAEveryDays: 21, MinTradeDays: 100}
	reports, agg, err := walkForwardOnSeries(cfg, series, p)
	if err != nil {
		t.Fatalf("walkForwardOnSeries err: %v", err)
	}
	if agg.NWindows == 0 || len(reports) == 0 {
		t.Fatalf("expected >=1 window")
	}
	// 策略零成交 -> 曲線平盤 -> MaxDD 0、Calmar 非有限;不應 panic 已由跑到這裡證明。
	for _, r := range reports {
		if r.Buys != 0 || r.Sells != 0 {
			t.Fatalf("flat series should yield no trades, got buys=%d sells=%d", r.Buys, r.Sells)
		}
		if r.Strat.MaxDD != 0 {
			t.Fatalf("flat strat MaxDD = %v, want 0", r.Strat.MaxDD)
		}
	}
}
