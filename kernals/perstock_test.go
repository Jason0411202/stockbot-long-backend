package kernals

import (
	"main/config"
	"testing"
	"time"
)

// risingSeries 緩升序列 (多頭、且常落在 10MA×1.05 內 → 會觸發買入)。
func risingSeries(start time.Time, n int) *stockSeries {
	p := make([]float64, n)
	for i := range p {
		p[i] = 100 + 0.1*float64(i)
	}
	s := buildSeries(start, p)
	s.prefixClose = buildPrefixClose(s.closePrices) // maAt (進場/regime MA) 需要
	return s
}

func perStockCfg(stocks []string) *config.Config {
	return &config.Config{
		TrackStocks:      stocks,
		ScalingStrategy:  "Baseline",
		InitialCash:      100000,
		MAWindow:         10,
		RegimeMethod:     "ma_pos",
		RegimeMAWindow:   50,
		CooldownDays:     14,
		BullCooldownDays: 14,
		BullBuyBand:      0.05,
		BuyFracBasis:     "cash",
		BullBuyFrac:      0.10,
		BearBuyFrac:      0.02,
		BuyTierRatio:     2.5,
		BuyDepthBasis:    "peak",
		BaselineBuyTiers: []config.BaselineBuyTier{
			{Above: -0.1}, {Above: -0.2}, {Above: -0.3}, {Above: -0.4},
		},
		BaselineSellThreshold: 1.0,
		SellFracOfPosition:    0.33,
	}
}

// per-stock override 應只影響該股:把 A 用 MAWindow=9999 (序列永遠算不出 → A 不買),
// 則 A+B 投組結果應等於「只有 B」的結果 (A 不買、不佔現金),且明顯異於無 override。
func TestEngine_PerStockOverride_Isolates(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	series := map[string]*stockSeries{
		"A": risingSeries(start, 300),
		"B": risingSeries(start, 300),
	}
	end := series["A"].dates[299]

	base := perStockCfg([]string{"A", "B"})
	r0, err := runBacktestWindow(base, series, series["A"].dates[0], end)
	if err != nil {
		t.Fatalf("r0 err: %v", err)
	}

	// 停用 A:override A 的 MAWindow 到極大 → A 永遠 NaN 均線 → 不買。
	ov := perStockCfg([]string{"A", "B"})
	ov.StockOverrides = map[string]config.StockParams{"A": {MAWindow: iptr(9999)}}
	r1, err := runBacktestWindow(ov, series, series["A"].dates[0], end)
	if err != nil {
		t.Fatalf("r1 err: %v", err)
	}

	bOnly := perStockCfg([]string{"B"})
	rB, err := runBacktestWindow(bOnly, series, series["B"].dates[0], end)
	if err != nil {
		t.Fatalf("rB err: %v", err)
	}

	if r0.TotalBuys <= r1.TotalBuys {
		t.Fatalf("override 停用 A 後買入次數應下降: r0=%d r1=%d", r0.TotalBuys, r1.TotalBuys)
	}
	// A 不買、不佔現金 → 投組結果應等於只有 B。
	if r1.TotalBuys != rB.TotalBuys || r1.FinalTotal != rB.FinalTotal {
		t.Fatalf("停用 A 的投組應 == 只有 B: r1(buys=%d total=%.2f) vs B(buys=%d total=%.2f)",
			r1.TotalBuys, r1.FinalTotal, rB.TotalBuys, rB.FinalTotal)
	}
}

func iptr(v int) *int { return &v }
