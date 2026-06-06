package kernals

import (
	"testing"
	"time"
)

// backtest_test.go 為回測核心的整合測試:起點 (common issuance)、區間視窗、每月注資時程、
// 防呆 (僅支援 Baseline)。回測與上線共用同一引擎,故這些測試也間接守住上線決策。

func TestCommonIssuanceStart_LatestListing(t *testing.T) {
	// Arrange — A 早上市、B 晚上市;common issuance = 較晚者第一天。
	series := map[string]*stockSeries{
		"A": flatSeries(300, 2019, time.January, 100),
		"B": flatSeries(300, 2019, time.March, 100),
	}
	cfg := baseCfg("A", "B")

	// Act
	got, ok := commonIssuanceStart(cfg, series)

	// Assert
	want := time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("commonIssuanceStart = (%s,%v), want (%s,true)", got.Format("2006-01-02"), ok, want.Format("2006-01-02"))
	}
}

func TestRunBacktestOnSeries_StartsAtCommonIssuance(t *testing.T) {
	// Arrange
	series := map[string]*stockSeries{
		"A": flatSeries(400, 2019, time.January, 100),
		"B": flatSeries(400, 2019, time.March, 100),
	}
	cfg := baseCfg("A", "B")

	// Act — runBacktestOnSeries 應等於「從 common issuance 起算的視窗」。
	ci, _ := commonIssuanceStart(cfg, series)
	end := collectDateUnion(series)
	want, err := runBacktestWindow(cfg, series, ci, end[len(end)-1])
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	got, err := runBacktestOnSeries(cfg, series)
	if err != nil {
		t.Fatalf("onSeries: %v", err)
	}

	// Assert
	if got.FinalTotal != want.FinalTotal || got.TotalBuys != want.TotalBuys {
		t.Fatalf("onSeries not anchored at common issuance: got(Final=%.2f Buys=%d) want(Final=%.2f Buys=%d)",
			got.FinalTotal, got.TotalBuys, want.FinalTotal, want.TotalBuys)
	}
}

func TestRunBacktestWindow_TracksContributions(t *testing.T) {
	// Arrange — 90 連續日從 2020-01-01 (跨 1~3 月),每月注資 2500 → 起始月 (Jan) 不注、Feb/Mar 各注一次。
	cfg := baseCfg("TEST")
	cfg.MonthlyContribution = 2500
	series := map[string]*stockSeries{"TEST": flatSeries(90, 2020, time.January, 100)}
	dates := series["TEST"].dates

	// Act
	res, err := runBacktestWindow(cfg, series, dates[0], dates[len(dates)-1])
	if err != nil {
		t.Fatalf("window: %v", err)
	}

	// Assert — 平盤序列零成交,期末現金 = 期初 + 注資合計;注資合計 = 2500×2。
	if res.TotalContributed != 5000 {
		t.Fatalf("TotalContributed = %.2f, want 5000", res.TotalContributed)
	}
	if res.TotalBuys != 0 || res.TotalSells != 0 {
		t.Fatalf("flat series should not trade, got buys=%d sells=%d", res.TotalBuys, res.TotalSells)
	}
	if res.FinalCash != cfg.InitialCash+5000 {
		t.Fatalf("FinalCash = %.2f, want %.2f", res.FinalCash, cfg.InitialCash+5000)
	}
}

func TestRunBacktestWindow_RejectsNonBaseline(t *testing.T) {
	// Arrange
	cfg := baseCfg("TEST")
	cfg.ScalingStrategy = "SomethingElse"
	series := map[string]*stockSeries{"TEST": flatSeries(30, 2020, time.January, 100)}
	dates := series["TEST"].dates

	// Act + Assert
	if _, err := runBacktestWindow(cfg, series, dates[0], dates[len(dates)-1]); err == nil {
		t.Fatalf("expected error for non-Baseline strategy")
	}
}

func TestRunBacktestWindow_EmptyWindowErrors(t *testing.T) {
	// Arrange — start 在所有資料之後 → 視窗內無交易日。
	cfg := baseCfg("TEST")
	series := map[string]*stockSeries{"TEST": flatSeries(30, 2020, time.January, 100)}

	// Act + Assert
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := runBacktestWindow(cfg, series, future, future.AddDate(0, 1, 0)); err == nil {
		t.Fatalf("expected error for empty window")
	}
}

func TestContributionAmounts(t *testing.T) {
	// Arrange — 90 連續日從 2020-01-01。
	dates := flatSeries(90, 2020, time.January, 100).dates

	// Act
	got := contributionAmounts(dates, 2500)

	// Assert — out[0]=0;每逢月份切換注一次;合計 = 2500×2 (Feb/Mar)。
	if got[0] != 0 {
		t.Fatalf("first day should never receive contribution, got %.2f", got[0])
	}
	total, injections := 0.0, 0
	for _, v := range got {
		if v > 0 {
			total += v
			injections++
		}
	}
	if injections != 2 || total != 5000 {
		t.Fatalf("got %d injections totalling %.2f, want 2 totalling 5000 (Feb/Mar)", injections, total)
	}

	// monthly<=0 → 全 0 (退化回無注資)。
	for _, v := range contributionAmounts(dates, 0) {
		if v != 0 {
			t.Fatalf("monthly=0 should disable contributions")
		}
	}
}

func TestCollectDateUnion_SortedDedup(t *testing.T) {
	// Arrange — 兩檔部分重疊的日期。
	series := map[string]*stockSeries{
		"A": seriesFrom(mustDate(t, "2020-01-01"), constPrices(3, 1)),
		"B": seriesFrom(mustDate(t, "2020-01-02"), constPrices(3, 1)),
	}

	// Act
	union := collectDateUnion(series)

	// Assert — 去重 + 升冪:01-01..01-04 共 4 天。
	if len(union) != 4 {
		t.Fatalf("union len = %d, want 4", len(union))
	}
	for i := 1; i < len(union); i++ {
		if !union[i].After(union[i-1]) {
			t.Fatalf("union not strictly increasing at %d", i)
		}
	}
}
