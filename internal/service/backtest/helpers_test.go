package backtest

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
	"math"
	"testing"
	"time"
)

// helpers_test.go 收錄 backtest 套件白箱測試共用的建構工具 (factory / builder)。
// 由原 kernals/helpers_test.go 拆分而來;series 工廠改用 trading.NewStockSeries 建構。

func iptr(v int) *int         { return &v }
func fptr(v float64) *float64 { return &v }

// mustDate 解析 "2006-01-02";失敗即 t.Fatal。
func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// baseCfg 回傳一份「現行 live 演算法」風格的設定。未指定 stocks 時預設單檔 "TEST"。
func baseCfg(stocks ...string) *config.Config {
	if len(stocks) == 0 {
		stocks = []string{"TEST"}
	}
	return &config.Config{
		TrackStocks:             stocks,
		ScalingStrategy:         "Baseline",
		InitialCash:             1_000_000,
		MAWindow:                10,
		RegimeMethod:            "ma_pos",
		RegimeMAWindow:          50,
		CooldownDays:            14,
		BullCooldownDays:        14,
		BullBuyBand:             0.05,
		BuyFracBasis:            "cash",
		BullBuyFrac:             0.20,
		BearBuyFrac:             0.02,
		BuyTierRatio:            2.5,
		BuyDepthBasis:           "peak",
		BuyPeakLookback:         252,
		BaselineBuyTiers:        []config.BaselineBuyTier{{Above: -0.1}, {Above: -0.2}, {Above: -0.3}, {Above: -0.4}},
		BaselineSellThreshold:   1.0,
		SellFracOfPosition:      0.33,
		TrailStopBear:           0.10,
		TrailMinGain:            0.10,
		CooldownBreakWindowDays: 365,
	}
}

// seriesFrom 由起始日 + 連續日曆日收盤價建立完整 *trading.StockSeries (含 MA20 / PrefixClose)。
func seriesFrom(start time.Time, prices []float64) *trading.StockSeries {
	n := len(prices)
	dates := make([]time.Time, n)
	for i := 0; i < n; i++ {
		dates[i] = start.AddDate(0, 0, i)
	}
	cp := make([]float64, n)
	copy(cp, prices)
	return trading.NewStockSeries(dates, cp, nil, nil, nil)
}

// constPrices 回傳 n 個值皆為 v 的價格切片 (平盤序列;策略零成交,方便驗起點 / 區間)。
func constPrices(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// flatSeries 為「起點 startYear/startMonth 1 號、連續 n 日、價格恆 v」的平盤序列。
func flatSeries(n int, startYear int, startMonth time.Month, v float64) *trading.StockSeries {
	return seriesFrom(time.Date(startYear, startMonth, 1, 0, 0, 0, 0, time.UTC), constPrices(n, v))
}

// risingSeries 為緩升序列 (多頭、常落在 10MA×1.05 內 → 會觸發現金比例買入)。
func risingSeries(start time.Time, n int) *trading.StockSeries {
	p := make([]float64, n)
	for i := range p {
		p[i] = 100 + 0.1*float64(i)
	}
	return seriesFrom(start, p)
}

// linRamp 回傳由 from 線性漲到 to 的 n 點價格 (用於 B&H 倍數驗證等需要已知終值的情境)。
func linRamp(n int, from, to float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = from + (to-from)*float64(i)/float64(n-1)
	}
	return out
}

// approxEq 在絕對誤差容忍下比較浮點。
func approxEq(a, b, tolerance float64) bool { return math.Abs(a-b) <= tolerance }
