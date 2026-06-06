package kernals

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"math"
	"math/rand"
	"testing"
	"time"
)

// characterization_test.go 是「黃金測試 (golden / characterization test)」:
// 用一份「鏡像 config.yaml 的 live 策略設定」對一段確定性 (固定 seed) 合成資料跑完整引擎,
// 把端到端輸出 (買/賣/停利/了結/現金/總權益) 釘成常數。
//
// 它存在的唯一目的,是在「移除棄用演算法分支」的重構過程中,證明『目前交易演算法的行為完全不變』:
// 任何會改動 live 決策路徑的修改,都會讓這些精確數字對不上 → 測試失敗。
// 合成資料刻意製造兩段崩盤,確保牛熊翻轉、深跌加碼、移動停利、獲利了結、打破冷卻都被觸發。

// liveStrategyCfg 以程式碼鏡像 config.yaml 的定版策略 (含 00631L per-stock override)。
// 這是「目前交易演算法」的唯一事實來源,golden 測試與重構後行為都以它為準。
func liveStrategyCfg() *config.Config {
	return &config.Config{
		TrackStocks:             []string{"00631L", "00830"},
		ScalingStrategy:         "Baseline",
		InitialCash:             100000,
		MonthlyContribution:     2500,
		MAWindow:                10,
		RegimeMethod:            "ma_pos",
		RegimeMAWindow:          95,
		CooldownDays:            14,
		BullCooldownDays:        14,
		BullBuyBand:             0.05,
		BuyFracBasis:            "cash",
		BullBuyFrac:             0.20,
		BearBuyFrac:             0.02,
		CooldownBreakBudget:     2,
		CooldownBreakWindowDays: 365,
		BuyDepthBasis:           "peak",
		BuyPeakLookback:         252,
		BuyTierRatio:            2.5,
		BaselineBuyTiers: []config.BaselineBuyTier{
			{Above: -0.1}, {Above: -0.2}, {Above: -0.3}, {Above: -0.4},
		},
		BaselineSellThreshold: 1.0,
		SellFracOfPosition:    0.33,
		TrailStopBear:         0.10,
		TrailMinGain:          0.10,
		StockOverrides: map[string]config.StockParams{
			"00631L": {RegimeMAWindow: iptr(60)},
		},
	}
}

// charSeries 產生確定性 (固定 seed) 的幾何隨機漫步價格序列,並在兩個區間注入崩盤,
// 確保牛熊翻轉 + 深跌加碼 + 移動停利 + 獲利了結都會被觸發。
func charSeries(seed int64, n int, startPx, drift, vol float64) *stockSeries {
	r := rand.New(rand.NewSource(seed))
	prices := make([]float64, n)
	px := startPx
	for i := 0; i < n; i++ {
		step := drift + vol*(r.Float64()*2-1)
		if i >= 200 && i < 260 { // 第一段崩盤
			step -= 0.020
		}
		if i >= 400 && i < 440 { // 第二段崩盤
			step -= 0.025
		}
		px *= 1 + step
		if px < 1 {
			px = 1
		}
		prices[i] = math.Round(px*100) / 100
	}

	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	dates := make([]time.Time, n)
	idx := make(map[string]int, n)
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i)
		dates[i] = d
		idx[d.Format("2006-01-02")] = i
	}
	return &stockSeries{
		dates:       dates,
		dateIndex:   idx,
		closePrices: prices,
		ma20:        rollingMA(prices, 20),
		prefixClose: buildPrefixClose(prices),
	}
}

// TestCharacterization_LiveStrategyFingerprint 鎖定 live 策略的端到端指紋。
// 重構 (移除棄用分支) 後此數字必須一字不差,才證明『目前演算法行為不變』。
func TestCharacterization_LiveStrategyFingerprint(t *testing.T) {
	// Arrange — live 設定 + 確定性雙標的資料 (一檔較波動模擬 2x 槓桿)。
	cfg := liveStrategyCfg()
	series := map[string]*stockSeries{
		"00631L": charSeries(1, 700, 20, 0.0045, 0.028),
		"00830":  charSeries(2, 700, 30, 0.0035, 0.014),
	}

	// Act — 以 common issuance 為起點、每月注資,跑完整引擎 (與 runBacktestWindow 同路徑,額外取 trail/profit 拆解)。
	allDates := collectDateUnion(series)
	start := allDates[0]
	if ci, ok := commonIssuanceStart(cfg, series); ok && ci.After(start) {
		start = ci
	}
	end := allDates[len(allDates)-1]
	lo := 0
	for lo < len(allDates) && allDates[lo].Before(start) {
		lo++
	}
	windowDates := allDates[lo:]
	contribOnDay := contributionAmounts(windowDates, cfg.MonthlyContribution)

	engine := NewEngine(cfg)
	for i, d := range windowDates {
		if contribOnDay[i] > 0 {
			engine.AddCash(contribOnDay[i])
		}
		if err := engine.ProcessDay(d, series, noopExecutor{}); err != nil {
			t.Fatalf("ProcessDay(%s): %v", d.Format("2006-01-02"), err)
		}
	}
	stats := engine.Stats()
	finalCash := math.Round(engine.Cash())
	finalTotal := math.Round(engine.Cash() + engine.HoldingValueAsOf(series, end))

	// Assert — golden 指紋 (由首次跑出的真實值釘定;見下方常數)。
	want := struct {
		buys, sells, skipped, trail, profit int
		finalCash, finalTotal               float64
	}{
		buys: goldenBuys, sells: goldenSells, skipped: goldenSkipped,
		trail: goldenTrail, profit: goldenProfit,
		finalCash: goldenFinalCash, finalTotal: goldenFinalTotal,
	}

	if want.buys < 0 { // 尚未釘定:印出實際值供首次填入。
		t.Fatalf("CAPTURE golden: buys=%d sells=%d skipped=%d trail=%d profit=%d finalCash=%.0f finalTotal=%.0f",
			stats.TotalBuys, stats.TotalSells, stats.SkippedBuys, stats.TrailSells, stats.ProfitSells, finalCash, finalTotal)
	}

	if stats.TotalBuys != want.buys || stats.TotalSells != want.sells || stats.SkippedBuys != want.skipped ||
		stats.TrailSells != want.trail || stats.ProfitSells != want.profit ||
		finalCash != want.finalCash || finalTotal != want.finalTotal {
		t.Fatalf("live 策略指紋改變!\n got: buys=%d sells=%d skipped=%d trail=%d profit=%d finalCash=%.0f finalTotal=%.0f\nwant: buys=%d sells=%d skipped=%d trail=%d profit=%d finalCash=%.0f finalTotal=%.0f",
			stats.TotalBuys, stats.TotalSells, stats.SkippedBuys, stats.TrailSells, stats.ProfitSells, finalCash, finalTotal,
			want.buys, want.sells, want.skipped, want.trail, want.profit, want.finalCash, want.finalTotal)
	}

	// 自我檢查:資料確實觸發了所有關鍵賣出路徑,指紋才有意義。
	if stats.TrailSells == 0 || stats.ProfitSells == 0 {
		t.Fatalf("characterization 資料未觸發 trail(%d)/profit(%d) 賣出,指紋覆蓋不足", stats.TrailSells, stats.ProfitSells)
	}
}

// golden 指紋常數 — 首次以 -1 跑出實際值後填入 (見上方 CAPTURE 分支)。
const (
	goldenBuys       = 96
	goldenSells      = 84
	goldenSkipped    = 0
	goldenTrail      = 11
	goldenProfit     = 7
	goldenFinalCash  = 50747
	goldenFinalTotal = 319543
)
