package kernals

import (
	"main/config"
	"math"
	"testing"
	"time"
)

func decisionCfg() *config.Config {
	return &config.Config{
		TrackStocks:               []string{"TEST"},
		ScalingStrategy:           "Baseline",
		BuyAndSellMultiplier:      1.0,
		CooldownDays:              14,
		BaselineBuyTiers:          []config.BaselineBuyTier{{Above: -0.1, Amount: 500}, {Above: -0.2, Amount: 1000}},
		BaselineBuyFallbackAmount: 3000,
		BaselineSellAmount:        10000,
		BaselineSellThreshold:     1.0,
		InitialCash:               1_000_000,
	}
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// DecideBuy 必須在「無 20MA」(NaN) 時拒絕買入,即使其他條件都成立。
func TestDecideBuy_NoMA20_Skipped(t *testing.T) {
	cfg := decisionCfg()
	snap := Snapshot{
		StockID:          "TEST",
		Today:            mustDate(t, "2024-06-01"),
		TodayPrice:       100,
		MA20:             math.NaN(),
		HighestHeldPrice: -1,
		LowestHeldPrice:  -1,
	}
	if got := DecideBuy(cfg, snap); got.Should {
		t.Fatalf("expected no buy when MA20 is NaN, got %+v", got)
	}
}

// 今日價 >= 20MA 時,DecideBuy 不應觸發。
func TestDecideBuy_PriceAboveMA20_Skipped(t *testing.T) {
	cfg := decisionCfg()
	snap := Snapshot{
		StockID:          "TEST",
		Today:            mustDate(t, "2024-06-01"),
		TodayPrice:       110,
		MA20:             100,
		HighestHeldPrice: -1,
		LowestHeldPrice:  -1,
	}
	if got := DecideBuy(cfg, snap); got.Should {
		t.Fatalf("expected no buy when price >= MA20, got %+v", got)
	}
}

// 在冷卻期內,DecideBuy 不應觸發,即使其他訊號都對。
func TestDecideBuy_InsideCooldown_Skipped(t *testing.T) {
	cfg := decisionCfg() // cooldown_days = 14
	snap := Snapshot{
		StockID:          "TEST",
		Today:            mustDate(t, "2024-06-10"),
		TodayPrice:       90,
		MA20:             100,
		HighestHeldPrice: 100,
		LowestHeldPrice:  100,
		HasLastBuy:       true,
		LastBuyDate:      mustDate(t, "2024-06-01"), // 9 天前 < 14 天冷卻
	}
	if got := DecideBuy(cfg, snap); got.Should {
		t.Fatalf("expected no buy inside cooldown, got %+v", got)
	}
}

// 冷卻期剛好過,DecideBuy 應觸發。
func TestDecideBuy_ExactlyAtCooldownBoundary_Triggers(t *testing.T) {
	cfg := decisionCfg() // cooldown_days = 14
	snap := Snapshot{
		StockID:          "TEST",
		Today:            mustDate(t, "2024-06-15"),
		TodayPrice:       90,
		MA20:             100,
		HighestHeldPrice: 100, // (90-100)/100 = -0.10 → 不符合 -0.1 tier (above 是嚴格 > -0.1) → fallback 3000
		LowestHeldPrice:  100,
		HasLastBuy:       true,
		LastBuyDate:      mustDate(t, "2024-06-01"), // 14 天前 == cooldown
	}
	got := DecideBuy(cfg, snap)
	if !got.Should {
		t.Fatalf("expected buy at cooldown boundary, got %+v", got)
	}
	if got.Shares <= 0 {
		t.Fatalf("expected positive shares, got %d", got.Shares)
	}
}

// 已有持倉、最低成本不夠高時,DecideSell 不應觸發。
func TestDecideSell_GainBelowThreshold_Skipped(t *testing.T) {
	cfg := decisionCfg() // baseline_sell_threshold = 1.0 (gain 必須 > 100%)
	snap := Snapshot{
		StockID:         "TEST",
		Today:           mustDate(t, "2024-06-01"),
		TodayPrice:      150, // (150-100)/100 = 0.5,未達 1.0
		LowestHeldPrice: 100,
	}
	if got := DecideSell(cfg, snap); got.Should {
		t.Fatalf("expected no sell when gain < threshold, got %+v", got)
	}
}

// 達到獲利門檻時,DecideSell 應觸發。
func TestDecideSell_GainMeetsThreshold_Triggers(t *testing.T) {
	cfg := decisionCfg()
	snap := Snapshot{
		StockID:         "TEST",
		Today:           mustDate(t, "2024-06-01"),
		TodayPrice:      210, // (210-100)/100 = 1.10 > 1.0
		LowestHeldPrice: 100,
	}
	got := DecideSell(cfg, snap)
	if !got.Should {
		t.Fatalf("expected sell when gain >= threshold, got %+v", got)
	}
	if got.TargetShares <= 0 {
		t.Fatalf("expected positive target shares, got %d", got.TargetShares)
	}
}

// 無持倉時 DecideSell 不應觸發。
func TestDecideSell_NoPositions_Skipped(t *testing.T) {
	cfg := decisionCfg()
	snap := Snapshot{
		StockID:         "TEST",
		Today:           mustDate(t, "2024-06-01"),
		TodayPrice:      999,
		LowestHeldPrice: -1, // 無持倉
	}
	if got := DecideSell(cfg, snap); got.Should {
		t.Fatalf("expected no sell with empty positions, got %+v", got)
	}
}

// Engine 的現金夾取 — 即使 DecideBuy 想買很多,執行層也只能買得起的部分。
func TestEngine_CashClampPreventsNegativeCash(t *testing.T) {
	cfg := decisionCfg()
	cfg.InitialCash = 1500                  // 只夠買 15 股@100
	cfg.BaselineBuyFallbackAmount = 100_000 // 策略想買 1000 股@100 → 應被夾取到 15
	cfg.BaselineBuyTiers = nil              // 強制走 fallback

	// 30 天序列,價格全 100,MA20 在 i>=19 後有效
	series := map[string]*stockSeries{
		"TEST": makeSeries(30, 2024, time.January),
	}
	// 改成有效的 buy 觸發:把第 25 天的價格壓到 50 (< MA20=100,且持倉高點 -50% → fallback 3000 tier)
	series["TEST"].closePrices[25] = 50
	// MA20 重算 (簡化:直接設成 100,因為前 24 天都 100,第 25 天 50,window 內仍接近 100)
	// 為了測試確定性,直接給定 ma20[25] = 100
	series["TEST"].ma20[25] = 100

	engine := NewEngine(cfg)
	day := series["TEST"].dates[25]
	if err := engine.ProcessDay(day, series, noopExecutor{}); err != nil {
		t.Fatalf("ProcessDay error: %v", err)
	}
	if engine.Cash() < 0 {
		t.Fatalf("cash went negative: %.2f", engine.Cash())
	}
	// 應該有買進，但被夾取到 15 股以內
	stats := engine.Stats()
	if stats.TotalBuys != 1 {
		t.Fatalf("expected 1 buy, got %d", stats.TotalBuys)
	}
}
