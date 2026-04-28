package kernals

import (
	"bytes"
	"main/config"
	"math"
	"strings"
	"testing"
	"time"
)

// makeSeries 建立 n 個連續日曆日的合成序列 (起點 startYear/startMonth 1 號),
// 收盤價固定為 100 → backtest 不會觸發 baseline 買賣,
// 所以我們可以單獨驗 truncation / warning 行為。
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
		MaxBackMonths:             1,
		BackTestingMonths:         0, // 測試時用參數覆寫,struct 欄位本身不直接被 runBacktestOnSeries 讀
		CooldownDays:              14,
		BaselineBuyTiers:          []config.BaselineBuyTier{{Above: -0.1, Amount: 500}},
		BaselineBuyFallbackAmount: 3000,
		BaselineSellAmount:        10000,
		BaselineSellThreshold:     1.0,
		InitialCash:               1000000,
		InitDBBackMonths:          1,
	}
}

// installSink 把 backtestWarnSink 換成 buffer,測試結束自動還原。
func installSink(t *testing.T) *bytes.Buffer {
	t.Helper()
	prev := backtestWarnSink
	buf := &bytes.Buffer{}
	backtestWarnSink = buf
	t.Cleanup(func() { backtestWarnSink = prev })
	return buf
}

// 序列覆蓋 30 個月 (~900 天) → 要求 60 個月超出資料,應觸發 warning。
func TestRunBacktestOnSeries_WarnsWhenRequestExceedsAvailable(t *testing.T) {
	buf := installSink(t)

	series := map[string]*stockSeries{
		"TEST": makeSeries(900, 2020, time.January),
	}
	cfg := minimalCfg()

	_, err := runBacktestOnSeries(cfg, series, 60)
	if err != nil {
		t.Fatalf("backtest err: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "back_testing_months=60") {
		t.Fatalf("expected warning mentioning back_testing_months=60, got: %q", out)
	}
	if !strings.Contains(out, "DB 最早資料只到") {
		t.Fatalf("expected warning mentioning earliest DB date, got: %q", out)
	}
}

// 序列覆蓋 6 個月 (~180 天) → 要求 3 個月,完全在範圍內,不警告。
func TestRunBacktestOnSeries_NoWarnWhenRequestFitsAvailable(t *testing.T) {
	buf := installSink(t)

	series := map[string]*stockSeries{
		"TEST": makeSeries(180, 2024, time.January),
	}
	cfg := minimalCfg()

	if _, err := runBacktestOnSeries(cfg, series, 3); err != nil {
		t.Fatalf("backtest err: %v", err)
	}

	if buf.Len() != 0 {
		t.Fatalf("expected no warning, got: %q", buf.String())
	}
}

// 邊界:cutoff 剛好等於序列起始日 → 視為「資料剛好不夠」,觸發 warning。
// 序列起點 2024-01-01,長度 365 (跨入 2024-12),latest = 2024-12-30,
// backTestMonths=12 → cutoff = 2023-12-30 < 2024-01-01,所以警告。
func TestRunBacktestOnSeries_WarnsAtBoundary(t *testing.T) {
	buf := installSink(t)

	series := map[string]*stockSeries{
		"TEST": makeSeries(365, 2024, time.January),
	}
	cfg := minimalCfg()

	if _, err := runBacktestOnSeries(cfg, series, 12); err != nil {
		t.Fatalf("backtest err: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatalf("expected warning at boundary (cutoff before earliest), got nothing")
	}
}

// backTestMonths <= 0 表示「停用」,絕不應該觸發警告。
func TestRunBacktestOnSeries_NoWarnWhenDisabled(t *testing.T) {
	buf := installSink(t)

	series := map[string]*stockSeries{
		"TEST": makeSeries(50, 2024, time.January),
	}
	cfg := minimalCfg()

	if _, err := runBacktestOnSeries(cfg, series, 0); err != nil {
		t.Fatalf("backtest err: %v", err)
	}
	if _, err := runBacktestOnSeries(cfg, series, -1); err != nil {
		t.Fatalf("backtest err: %v", err)
	}

	if buf.Len() != 0 {
		t.Fatalf("expected no warning when disabled, got: %q", buf.String())
	}
}
