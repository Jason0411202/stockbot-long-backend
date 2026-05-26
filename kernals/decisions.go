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
}

// Snapshot 為某一 (stockID, today) 的凍結市場 + 持倉檢視。
// 純函式 DecideBuy / DecideSell 只看 Snapshot,不直接讀 DB 或記憶體,
// 因此上線模式與回測模式只要能各自組出正確的 Snapshot,就會得到完全相同的決策。
type Snapshot struct {
	StockID          string
	Today            time.Time
	TodayPrice       float64
	MA20             float64 // 不足 20 日資料時為 NaN
	HighestHeldPrice float64 // 當前持倉的最高買入價;無持倉為 -1
	LowestHeldPrice  float64 // 當前持倉的最低買入價;無持倉為 -1
	HasLastBuy       bool
	LastBuyDate      time.Time
}

// DecideBuy 是 Baseline 策略的買入判斷,純函式,不產生任何副作用。
// 規則:
//  1. 當日有正常價格
//  2. 有足夠資料算 20MA 且今日股價 < 20MA
//  3. 不在冷卻期 (今日 - lastBuy >= cooldown_days)
//  4. 依 (今日股價 - 持倉最高買入價) / 持倉最高買入價 選 baseline tier 金額
//  5. shares = round(amount * multiplier / 今日股價)
func DecideBuy(cfg *config.Config, snap Snapshot) BuyIntent {
	if snap.TodayPrice <= 0 {
		return BuyIntent{}
	}
	if math.IsNaN(snap.MA20) {
		return BuyIntent{}
	}
	if snap.TodayPrice >= snap.MA20 {
		return BuyIntent{}
	}
	if snap.HasLastBuy {
		cooldown := time.Duration(cfg.CooldownDays) * 24 * time.Hour
		if snap.Today.Sub(snap.LastBuyDate) < cooldown {
			return BuyIntent{}
		}
	}

	percentages := 0.0
	if snap.HighestHeldPrice > 0 {
		percentages = (snap.TodayPrice - snap.HighestHeldPrice) / snap.HighestHeldPrice
	}
	amount := baselineBuyAmountFromCfg(cfg, percentages) * cfg.BuyAndSellMultiplier
	shares := amountToShares(amount, snap.TodayPrice)
	if shares <= 0 {
		return BuyIntent{}
	}
	return BuyIntent{
		Should: true,
		Shares: shares,
		Price:  snap.TodayPrice,
	}
}

// DecideSell 是 Baseline 策略的賣出判斷,純函式。
// 規則:
//  1. 當日有正常價格
//  2. 有持倉 (LowestHeldPrice > 0)
//  3. (今日股價 - 持倉最低買入價) / 持倉最低買入價 >= baseline_sell_threshold
//  4. 目標股數 = round(baseline_sell_amount * multiplier / 今日股價)
func DecideSell(cfg *config.Config, snap Snapshot) SellIntent {
	if snap.TodayPrice <= 0 {
		return SellIntent{}
	}
	if snap.LowestHeldPrice <= 0 {
		return SellIntent{}
	}
	gain := (snap.TodayPrice - snap.LowestHeldPrice) / snap.LowestHeldPrice
	if gain < cfg.BaselineSellThreshold {
		return SellIntent{}
	}
	shares := amountToShares(cfg.BaselineSellAmount*cfg.BuyAndSellMultiplier, snap.TodayPrice)
	if shares <= 0 {
		return SellIntent{}
	}
	return SellIntent{
		Should:       true,
		TargetShares: shares,
		Price:        snap.TodayPrice,
	}
}

// baselineBuyAmountFromCfg 依照 config 中 tier 決定買入目標金額,純函式。
func baselineBuyAmountFromCfg(cfg *config.Config, percentages float64) float64 {
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
