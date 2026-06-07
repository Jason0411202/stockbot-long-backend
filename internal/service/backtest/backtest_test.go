// internal/service/backtest/backtest_test.go 驗證回測進入點的起算日、注資與視窗組裝。
package backtest

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
	"testing"
	"time"
)

// backtest_test.go 為回測核心的整合測試:起點 (common issuance)、區間視窗、每月注資時程、
// 防呆 (僅支援 Baseline)。回測與上線共用同一引擎,故這些測試也間接守住上線決策。

// TestCommonIssuanceStart_LatestListing 驗證多標的中最晚上市日為共同起算日。
func TestCommonIssuanceStart_LatestListing(t *testing.T) {
	// Arrange — A 早上市、B 晚上市;common issuance = 較晚者第一天。
	series := map[string]*trading.StockSeries{
		"A": flatSeries(300, 2019, time.January, 100),
		"B": flatSeries(300, 2019, time.March, 100),
	}
	cfg := baseCfg("A", "B")

	// Act
	got, ok := CommonIssuanceStart(cfg, series)

	// Assert
	want := time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("CommonIssuanceStart = (%s,%v), want (%s,true)", got.Format("2006-01-02"), ok, want.Format("2006-01-02"))
	}
}

// TestRunBacktestOnSeries_StartsAtCommonIssuance 驗證 RunBacktestOnSeries 以共同起算日為視窗起點,結果與 RunBacktestWindow 一致。
func TestRunBacktestOnSeries_StartsAtCommonIssuance(t *testing.T) {
	// Arrange
	series := map[string]*trading.StockSeries{
		"A": flatSeries(400, 2019, time.January, 100),
		"B": flatSeries(400, 2019, time.March, 100),
	}
	cfg := baseCfg("A", "B")

	// Act — RunBacktestOnSeries 應等於「從 common issuance 起算的視窗」。
	ci, _ := CommonIssuanceStart(cfg, series)
	end := trading.CollectDateUnion(series)
	want, err := RunBacktestWindow(cfg, series, ci, end[len(end)-1])
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	got, err := RunBacktestOnSeries(cfg, series)
	if err != nil {
		t.Fatalf("onSeries: %v", err)
	}

	// Assert
	if got.FinalTotal != want.FinalTotal || got.TotalBuys != want.TotalBuys {
		t.Fatalf("onSeries not anchored at common issuance: got(Final=%.2f Buys=%d) want(Final=%.2f Buys=%d)",
			got.FinalTotal, got.TotalBuys, want.FinalTotal, want.TotalBuys)
	}
}

// TestRunBacktestWindow_TracksContributions 驗證每月注資金額正確累計至 TotalContributed,平盤序列零成交且期末現金等於期初加注資合計。
func TestRunBacktestWindow_TracksContributions(t *testing.T) {
	// Arrange — 90 連續日從 2020-01-01 (跨 1~3 月),每月注資 2500 → 起始月 (Jan) 不注、Feb/Mar 各注一次。
	cfg := baseCfg("TEST")
	cfg.MonthlyContribution = 2500
	series := map[string]*trading.StockSeries{"TEST": flatSeries(90, 2020, time.January, 100)}
	dates := series["TEST"].Dates

	// Act
	res, err := RunBacktestWindow(cfg, series, dates[0], dates[len(dates)-1])
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

// TestRunBacktestWindow_RejectsNonBaseline 驗證非 Baseline 策略回傳錯誤。
func TestRunBacktestWindow_RejectsNonBaseline(t *testing.T) {
	// Arrange
	cfg := baseCfg("TEST")
	cfg.ScalingStrategy = "SomethingElse"
	series := map[string]*trading.StockSeries{"TEST": flatSeries(30, 2020, time.January, 100)}
	dates := series["TEST"].Dates

	// Act + Assert
	if _, err := RunBacktestWindow(cfg, series, dates[0], dates[len(dates)-1]); err == nil {
		t.Fatalf("expected error for non-Baseline strategy")
	}
}

// TestRunBacktestWindow_EmptyWindowErrors 驗證視窗內無任何交易日時回傳錯誤。
func TestRunBacktestWindow_EmptyWindowErrors(t *testing.T) {
	// Arrange — start 在所有資料之後 → 視窗內無交易日。
	cfg := baseCfg("TEST")
	series := map[string]*trading.StockSeries{"TEST": flatSeries(30, 2020, time.January, 100)}

	// Act + Assert
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := RunBacktestWindow(cfg, series, future, future.AddDate(0, 1, 0)); err == nil {
		t.Fatalf("expected error for empty window")
	}
}

// TestContributionDue 驗證單日注資判定:前一日為零值或同月 → 0、跨月 → monthly、monthly<=0 → 0。
func TestContributionDue(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name    string
		prev, d time.Time
		monthly float64
		want    float64
	}{
		{"零值前一日不注資", time.Time{}, d(2020, time.January, 1), 2500, 0},
		{"同月不注資", d(2020, time.January, 5), d(2020, time.January, 31), 2500, 0},
		{"跨月注資", d(2020, time.January, 31), d(2020, time.February, 3), 2500, 2500},
		{"跨年注資", d(2019, time.December, 31), d(2020, time.January, 2), 2500, 2500},
		{"monthly<=0 關閉", d(2020, time.January, 31), d(2020, time.February, 3), 0, 0},
	}
	for _, c := range cases {
		if got := ContributionDue(c.prev, c.d, c.monthly); got != c.want {
			t.Fatalf("%s: ContributionDue = %.0f, want %.0f", c.name, got, c.want)
		}
	}
}

// TestContributionAmounts 驗證注資金額切片:起始日為 0、每逢月份切換注資一次、monthly<=0 時全為 0。
func TestContributionAmounts(t *testing.T) {
	// Arrange — 90 連續日從 2020-01-01。
	dates := flatSeries(90, 2020, time.January, 100).Dates

	// Act
	got := ContributionAmounts(dates, 2500)

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
	for _, v := range ContributionAmounts(dates, 0) {
		if v != 0 {
			t.Fatalf("monthly=0 should disable contributions")
		}
	}
}

// TestCollectDateUnion_SortedDedup 驗證多標的日期聯集已去重且嚴格升冪排列。
func TestCollectDateUnion_SortedDedup(t *testing.T) {
	// Arrange — 兩檔部分重疊的日期。
	series := map[string]*trading.StockSeries{
		"A": seriesFrom(mustDate(t, "2020-01-01"), constPrices(3, 1)),
		"B": seriesFrom(mustDate(t, "2020-01-02"), constPrices(3, 1)),
	}

	// Act
	union := trading.CollectDateUnion(series)

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

// TestEngine_PerStockOverride_Isolates 驗證 per-stock override 僅影響指定標的,停用 A 後投組行為等同純 B。
func TestEngine_PerStockOverride_Isolates(t *testing.T) {
	// Arrange — A、B 同資料;把 A 的 MAWindow override 到極大 (永遠算不出均線 → A 不買)。
	start := mustDate(t, "2020-01-01")
	series := map[string]*trading.StockSeries{
		"A": risingSeries(start, 300),
		"B": risingSeries(start, 300),
	}
	end := series["A"].Dates[299]

	base := baseCfg("A", "B")
	r0, err := RunBacktestWindow(base, series, series["A"].Dates[0], end)
	if err != nil {
		t.Fatalf("r0: %v", err)
	}

	ov := baseCfg("A", "B")
	ov.StockOverrides = map[string]config.StockParams{"A": {MAWindow: iptr(9999)}}
	r1, err := RunBacktestWindow(ov, series, series["A"].Dates[0], end)
	if err != nil {
		t.Fatalf("r1: %v", err)
	}

	bOnly := baseCfg("B")
	rB, err := RunBacktestWindow(bOnly, series, series["B"].Dates[0], end)
	if err != nil {
		t.Fatalf("rB: %v", err)
	}

	// Assert — 停用 A 後買入下降;且「A+B 但停用 A」的投組 == 「只有 B」(A 不買不佔現金)。
	if r0.TotalBuys <= r1.TotalBuys {
		t.Fatalf("disabling A should reduce buys: r0=%d r1=%d", r0.TotalBuys, r1.TotalBuys)
	}
	if r1.TotalBuys != rB.TotalBuys || r1.FinalTotal != rB.FinalTotal {
		t.Fatalf("portfolio with A disabled should equal B-only: r1(buys=%d total=%.2f) vs B(buys=%d total=%.2f)",
			r1.TotalBuys, r1.FinalTotal, rB.TotalBuys, rB.FinalTotal)
	}
}
