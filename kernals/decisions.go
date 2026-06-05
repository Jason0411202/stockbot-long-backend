package kernals

import (
	"main/config"
	"math"
	"time"
)

// BuyIntent 是 DecideBuy 的純函式輸出。
// Shares 為「策略想買的目標股數」,不考慮現金限制 — 執行層需再做夾取。
type BuyIntent struct {
	Should bool
	Shares int
	Price  float64 // = 當日收盤價
}

// SellIntent 是 DecideSell 的純函式輸出。
type SellIntent struct {
	Should       bool
	TargetShares int
	Price        float64 // = 當日收盤價
	Reason       string  // "trail" / "profit";供統計觸發次數
}

// Snapshot 為某一 (stockID, today) 的凍結市場 + 持倉檢視。
// 純函式 DecideBuy / DecideSell 只看 Snapshot,不直接讀 DB 或記憶體,
// 因此上線模式與回測模式只要能各自組出正確的 Snapshot,就會得到完全相同的決策。
type Snapshot struct {
	StockID          string
	Today            time.Time
	TodayPrice       float64
	MA20             float64 // 進場均線 (長度由 cfg.MAWindow 決定);資料不足時為 NaN
	HighestHeldPrice float64 // 當前持倉的最高買入價;無持倉為 -1
	LowestHeldPrice  float64 // 當前持倉的最低買入價;無持倉為 -1
	HasLastBuy       bool
	LastBuyDate      time.Time

	// 以下由引擎依 cfg 旗標「按需」填入;旗標關閉時維持零值/NaN,決策函式不讀取 → 行為與原始 Baseline 相同。
	IsBull        bool    // 牛熊判定 (RegimeMethod 關閉時恆為 false = bear/中性)
	RecentPeak    float64 // 近期高點 (peak 深度基準用);未啟用為 NaN
	Cash          float64 // 當前現金 (動態部位大小用)
	Equity        float64 // 當前總權益 = 現金 + 持股市值 (動態部位大小用)
	PeakSinceHold float64 // 持倉期間 (含今日) 的最高收盤;移動停利用。無持倉/未追蹤為 0
	HeldShares    int     // 目前持有總股數;移動停利全出時用
}

// DecideBuy 是買入判斷,純函式,不產生任何副作用。
// 規則:
//  1. 當日有正常價格、有進場均線。
//  2. 觸發:今價 < 進場均線×(1+band);bull 用 BullBuyBand 放寬,bear 嚴格 (band=0)。
//  3. 不在冷卻期 (bull 可用 BullCooldownDays)。
//  4. 金額:bull 用固定大額 (BullBuyAmount),bear 走 depth 表 (越深越大);可按帳戶大小動態縮放。
func DecideBuy(cfg *config.Config, snap Snapshot) BuyIntent {
	if snap.TodayPrice <= 0 || math.IsNaN(snap.MA20) {
		return BuyIntent{}
	}
	band := 0.0 // bear 嚴格「今價<均線」
	if snap.IsBull {
		band = cfg.BullBuyBand
	}
	if snap.TodayPrice >= snap.MA20*(1+band) {
		return BuyIntent{}
	}
	if snap.HasLastBuy {
		cdDays := cfg.CooldownDays
		if snap.IsBull && cfg.BullCooldownDays > 0 {
			cdDays = cfg.BullCooldownDays
		}
		if snap.Today.Sub(snap.LastBuyDate) < time.Duration(cdDays)*24*time.Hour {
			return BuyIntent{}
		}
	}

	// 買入金額:bull 固定大額 (少次大額);否則 depth 表 (越深越大)。
	var amount float64
	if snap.IsBull && cfg.BullBuyAmount > 0 {
		amount = cfg.BullBuyAmount * cfg.BuyAndSellMultiplier
	} else {
		amount = baselineBuyAmountFromCfg(cfg, buyDepthPct(cfg, snap)) * cfg.BuyAndSellMultiplier
	}
	// 動態部位大小:金字塔形狀不變,只把絕對額按帳戶大小等比縮放。
	if cfg.InitialCash > 0 {
		switch cfg.BuySizeMode {
		case "cash":
			if snap.Cash > 0 {
				amount *= snap.Cash / cfg.InitialCash
			}
		case "equity":
			if snap.Equity > 0 {
				amount *= snap.Equity / cfg.InitialCash
			}
		}
	}
	shares := amountToShares(amount, snap.TodayPrice)
	if shares <= 0 {
		return BuyIntent{}
	}
	return BuyIntent{Should: true, Shares: shares, Price: snap.TodayPrice}
}

// DecideSell 是賣出判斷,純函式。先檢查熊市移動停利 (保護式全出),再檢查獲利了結 (可分批)。
func DecideSell(cfg *config.Config, snap Snapshot) SellIntent {
	if snap.TodayPrice <= 0 || snap.LowestHeldPrice <= 0 {
		return SellIntent{}
	}

	// ── 移動停利:價跌破「持倉峰值×(1-trail)」即全數出場。僅熊市生效,且部位曾達 TrailMinGain 才武裝 ──
	// (不在尚未獲利時就把剛逢低買進的部位停損掉)。
	if !snap.IsBull && cfg.TrailStopBear > 0 && snap.PeakSinceHold > 0 && snap.HeldShares > 0 {
		peakGain := snap.PeakSinceHold/snap.LowestHeldPrice - 1
		if peakGain >= cfg.TrailMinGain && snap.TodayPrice <= snap.PeakSinceHold*(1-cfg.TrailStopBear) {
			return SellIntent{Should: true, TargetShares: snap.HeldShares, Price: snap.TodayPrice, Reason: "trail"}
		}
	}

	// ── 獲利了結:持倉最低成本獲利 >= 門檻時賣出 ──
	gain := (snap.TodayPrice - snap.LowestHeldPrice) / snap.LowestHeldPrice
	if gain < cfg.BaselineSellThreshold {
		return SellIntent{}
	}
	// 賣出量:預設固定金額;SellFracOfPosition>0 則改賣「當前持股的此比例」(分批出場)。
	var shares int
	if cfg.SellFracOfPosition > 0 && snap.HeldShares > 0 {
		shares = int(math.Round(cfg.SellFracOfPosition * float64(snap.HeldShares)))
		if shares < 1 {
			shares = 1
		}
	} else {
		shares = amountToShares(cfg.BaselineSellAmount*cfg.BuyAndSellMultiplier, snap.TodayPrice)
	}
	if shares <= 0 {
		return SellIntent{}
	}
	return SellIntent{Should: true, TargetShares: shares, Price: snap.TodayPrice, Reason: "profit"}
}

// buyDepthPct 回傳「加碼深度」判斷值 (越負代表跌越深 → 命中越深的 tier → 買越多)。
// 由 cfg.BuyDepthBasis 決定基準;預設 "held_high" 與原始行為相同。
//
//	held_high:(今價-持倉最高買入價)/持倉最高買入價 (相對自己的成本,較粗糙)
//	ma       :(今價-進場均線)/進場均線 (乖離率)
//	peak     :(今價-近期高點)/近期高點 (距高點回撤;最佳)
func buyDepthPct(cfg *config.Config, snap Snapshot) float64 {
	switch cfg.BuyDepthBasis {
	case "ma":
		if snap.MA20 > 0 {
			return (snap.TodayPrice - snap.MA20) / snap.MA20
		}
		return 0
	case "peak":
		if snap.RecentPeak > 0 {
			return (snap.TodayPrice - snap.RecentPeak) / snap.RecentPeak
		}
		return 0
	default: // "" / "held_high"
		if snap.HighestHeldPrice > 0 {
			return (snap.TodayPrice - snap.HighestHeldPrice) / snap.HighestHeldPrice
		}
		return 0
	}
}

// baselineBuyAmountFromCfg 依 tier 決定買入目標金額,純函式。
// 幾何加碼曲線啟用時 (BuyBaseAmount>0 && BuyTierRatio>0):金額 = base×ratio^i
// (i = 命中 tier 索引;跌破最深 tier 用 ratio^len 當 fallback),沿用 tier 的 above 邊界。
func baselineBuyAmountFromCfg(cfg *config.Config, percentages float64) float64 {
	if cfg.BuyBaseAmount > 0 && cfg.BuyTierRatio > 0 {
		for i, tier := range cfg.BaselineBuyTiers {
			if percentages > tier.Above {
				return cfg.BuyBaseAmount * math.Pow(cfg.BuyTierRatio, float64(i))
			}
		}
		return cfg.BuyBaseAmount * math.Pow(cfg.BuyTierRatio, float64(len(cfg.BaselineBuyTiers)))
	}
	for _, tier := range cfg.BaselineBuyTiers {
		if percentages > tier.Above {
			return tier.Amount
		}
	}
	return cfg.BaselineBuyFallbackAmount
}

// amountToShares 將金額轉為最接近的股數 (四捨五入)。
func amountToShares(amount float64, price float64) int {
	if price <= 0 || amount <= 0 {
		return 0
	}
	return int(math.Round(amount / price))
}
