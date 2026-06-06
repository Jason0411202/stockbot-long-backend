package kernals

import (
	"github.com/Jason0411202/stockbot-long-backend/config"
	"math"
	"testing"
	"time"
)

// helpers_test.go 收錄整個 kernals 測試套件共用的建構工具 (factory / builder),
// 讓各測試檔遵守 DRY、聚焦在「Arrange-Act-Assert」的 Act/Assert,而非重複鋪設資料。

func iptr(v int) *int         { return &v }
func fptr(v float64) *float64 { return &v }

// mustDate 解析 "2006-01-02";失敗即 t.Fatal (測試資料寫錯應立刻爆)。
func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// baseCfg 回傳一份「現行 live 演算法」風格的設定 (現金比例買入 + 比例賣出 + regime + 移動停利),
// 供 engine / backtest / walk-forward 整合測試使用。未指定 stocks 時預設單檔 "TEST"。
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

// decideCfg 回傳一份精簡的純決策設定 (現金比例),供 DecideBuy / DecideSell 等純函式單元測試。
// 不含 regime / per-stock,讓單一條件被獨立驗證。
func decideCfg() *config.Config {
	return &config.Config{
		ScalingStrategy:       "Baseline",
		InitialCash:           1_000_000,
		CooldownDays:          14,
		BuyFracBasis:          "cash",
		BullBuyFrac:           0.20,
		BearBuyFrac:           0.02,
		BuyTierRatio:          2.5,
		BuyDepthBasis:         "held_high",
		BaselineBuyTiers:      []config.BaselineBuyTier{{Above: -0.1}, {Above: -0.2}},
		BaselineSellThreshold: 1.0,
		SellFracOfPosition:    0.33,
	}
}

// seriesFrom 由起始日 + 連續日曆日收盤價建立完整 *stockSeries (含 ma20 / prefixClose,
// 與 loadStockSeries 同邏輯),讓 maAt / peakAt / regime 都能運作。
func seriesFrom(start time.Time, prices []float64) *stockSeries {
	n := len(prices)
	dates := make([]time.Time, n)
	idx := make(map[string]int, n)
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i)
		dates[i] = d
		idx[d.Format("2006-01-02")] = i
	}
	cp := make([]float64, n)
	copy(cp, prices)
	return &stockSeries{
		dates:       dates,
		dateIndex:   idx,
		closePrices: cp,
		ma20:        rollingMA(cp, 20),
		prefixClose: buildPrefixClose(cp),
	}
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
func flatSeries(n int, startYear int, startMonth time.Month, v float64) *stockSeries {
	return seriesFrom(time.Date(startYear, startMonth, 1, 0, 0, 0, 0, time.UTC), constPrices(n, v))
}

// risingSeries 為緩升序列 (多頭、常落在 10MA×1.05 內 → 會觸發現金比例買入)。
func risingSeries(start time.Time, n int) *stockSeries {
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

// approxEq 在絕對誤差容忍下比較浮點 (供不想引入 metrics_test 的 approx 簽名時使用)。
func approxEq(a, b, tolerance float64) bool { return math.Abs(a-b) <= tolerance }
