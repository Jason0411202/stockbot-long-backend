package kernals

import (
	"main/config"
	"math"
	"testing"
	"time"
)

// makeSeries 建立 n 個連續日曆日的合成序列 (起點 startYear/startMonth 1 號),
// 收盤價固定為 100 → backtest 不會觸發 baseline 買賣,方便單獨驗起點 / 區間行為。
func makeSeries(n int, startYear int, startMonth time.Month) *stockSeries {
	dates := make([]time.Time, n)
	prices := make([]float64, n)
	idx := make(map[string]int, n)
	ma20 := make([]float64, n)

	start := time.Date(startYear, startMonth, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i)
		dates[i] = d
		prices[i] = 100.0
		idx[d.Format("2006-01-02")] = i
		if i >= 19 {
			ma20[i] = 100.0
		} else {
			ma20[i] = math.NaN()
		}
	}
	return &stockSeries{
		dates:       dates,
		dateIndex:   idx,
		closePrices: prices,
		ma20:        ma20,
	}
}

func minimalCfg() *config.Config {
	return &config.Config{
		TrackStocks:               []string{"TEST"},
		ScalingStrategy:           "Baseline",
		BuyAndSellMultiplier:      1.0,
		CooldownDays:              14,
		BaselineBuyTiers:          []config.BaselineBuyTier{{Above: -0.1, Amount: 500}},
		BaselineBuyFallbackAmount: 3000,
		BaselineSellAmount:        10000,
		BaselineSellThreshold:     1.0,
		InitialCash:               1000000,
	}
}

// commonIssuanceStart = 各追蹤股票第一筆資料日的最大值 (較晚上市者決定起點)。
func TestCommonIssuanceStart_LatestListing(t *testing.T) {
	series := map[string]*stockSeries{
		"A": makeSeries(300, 2019, time.January), // 2019-01-01 起
		"B": makeSeries(300, 2019, time.March),   // 2019-03-01 起 (較晚上市)
	}
	cfg := minimalCfg()
	cfg.TrackStocks = []string{"A", "B"}

	got, ok := commonIssuanceStart(cfg, series)
	if !ok {
		t.Fatal("expected common issuance found")
	}
	want := time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("commonIssuanceStart = %s, want %s (較晚上市的 B)", got.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

// 回測起點應為 commonIssuanceStart:某檔尚未上市的空窗期不納入回測 (不在那段做決策)。
func TestRunBacktestOnSeries_StartsAtCommonIssuance(t *testing.T) {
	series := map[string]*stockSeries{
		"A": makeSeries(400, 2019, time.January),
		"B": makeSeries(400, 2019, time.March),
	}
	cfg := minimalCfg()
	cfg.TrackStocks = []string{"A", "B"}

	// 直接比對 runBacktestWindow(從 common issuance 起) 與 runBacktestOnSeries 結果一致,
	// 證明 runBacktestOnSeries 確實以 common issuance 為起點。
	ci, _ := commonIssuanceStart(cfg, series)
	allDates := collectDateUnion(series)
	end := allDates[len(allDates)-1]
	want, err := runBacktestWindow(cfg, series, ci, end)
	if err != nil {
		t.Fatalf("runBacktestWindow err: %v", err)
	}
	got, err := runBacktestOnSeries(cfg, series)
	if err != nil {
		t.Fatalf("runBacktestOnSeries err: %v", err)
	}
	if got.FinalTotal != want.FinalTotal || got.TotalBuys != want.TotalBuys {
		t.Fatalf("runBacktestOnSeries 未從 common issuance 起算: got Final=%.2f Buys=%d, want Final=%.2f Buys=%d",
			got.FinalTotal, got.TotalBuys, want.FinalTotal, want.TotalBuys)
	}
}

// 單一標的:common issuance = 該檔第一天,回測使用全部資料,不報錯。
func TestRunBacktestOnSeries_SingleStockUsesAll(t *testing.T) {
	series := map[string]*stockSeries{"TEST": makeSeries(250, 2024, time.January)}
	cfg := minimalCfg()
	res, err := runBacktestOnSeries(cfg, series)
	if err != nil {
		t.Fatalf("backtest err: %v", err)
	}
	if res.InitialCash != cfg.InitialCash {
		t.Fatalf("InitialCash = %.2f, want %.2f", res.InitialCash, cfg.InitialCash)
	}
}
