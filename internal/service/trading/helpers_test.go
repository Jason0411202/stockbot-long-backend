// internal/service/trading/helpers_test.go 提供 trading 測試共用的設定與序列建構函式。
package trading

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"testing"
	"time"
)

// helpers_test.go 收錄 trading 套件白箱測試共用的建構工具 (factory / builder)。
// 這些工廠由原 kernals/helpers_test.go 拆分而來;只保留 trading 層測試實際需要的部分。

// iptr 將整數值包裝為指標,供需要 *int 欄位的測試資料使用。
func iptr(v int) *int { return &v }

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
// 供 engine 整合測試使用。未指定 stocks 時預設單檔 "TEST"。
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

// seriesFrom 由起始日 + 連續日曆日收盤價建立完整 *StockSeries (含 MA20 / PrefixClose,
// 與 NewStockSeries 同邏輯),讓 maAt / peakAt / regime 都能運作。
func seriesFrom(start time.Time, prices []float64) *StockSeries {
	n := len(prices)
	dates := make([]time.Time, n)
	for i := 0; i < n; i++ {
		dates[i] = start.AddDate(0, 0, i)
	}
	cp := make([]float64, n)
	copy(cp, prices)
	return NewStockSeries(dates, cp, nil, nil, nil)
}

// constPrices 回傳 n 個值皆為 v 的價格切片 (平盤序列;策略零成交,方便驗起點 / 區間)。
func constPrices(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// risingSeries 為緩升序列 (多頭、常落在 10MA×1.05 內 → 會觸發現金比例買入)。
func risingSeries(start time.Time, n int) *StockSeries {
	p := make([]float64, n)
	for i := range p {
		p[i] = 100 + 0.1*float64(i)
	}
	return seriesFrom(start, p)
}

// linRamp 回傳由 from 線性漲到 to 的 n 點價格。
func linRamp(n int, from, to float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = from + (to-from)*float64(i)/float64(n-1)
	}
	return out
}
