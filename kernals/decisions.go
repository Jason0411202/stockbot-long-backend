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
	Reason       string  // "trail" / "stop" / "profit";供統計觸發次數
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

	// 以下為選用的優化旗標輸入。由引擎依 cfg 旗標「按需」填入;旗標關閉時維持零值/NaN,
	// DecideBuy/DecideSell 不會讀取 → 行為與原始 Baseline 完全相同。
	LongMA       float64 // 長期趨勢均線值;未啟用為 NaN
	LongMASlope  float64 // 長均線斜率 (今值 - lookback 前值);未啟用為 NaN
	RSIForBuy    float64 // 進場 gate 用 RSI;未啟用為 NaN
	RSIForSell   float64 // 出場 gate 用 RSI;未啟用為 NaN
	PrevClose    float64 // 昨日收盤;HasPrevClose=false 時無效
	HasPrevClose bool

	IsBull        bool    // 牛熊判定;RegimeMethod 關閉時恆為 false (= bear/中性,行為不變)
	RecentPeak    float64 // 近期高點 (peak 深度基準用);未啟用為 NaN
	Cash          float64 // 當前現金 (動態部位大小用)
	Equity        float64 // 當前總權益 = 現金 + 持股市值 (動態部位大小用)
	PeakSinceHold float64 // 持倉期間 (含今日) 的最高收盤;移動停利用。無持倉/未追蹤為 0
	HeldShares    int     // 目前持有總股數;移動停利全出時用
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
	// 進場觸發:預設「今價 < 進場均線」。bull regime 且設定 BullBuyBand>0 時放寬為
	// 「今價 < 均線×(1+band)」,讓多頭也能部署 (band 關閉=0 時與原始完全相同)。
	band := 0.0
	if snap.IsBull && cfg.BullBuyBand > 0 {
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
		cooldown := time.Duration(cdDays) * 24 * time.Hour
		if snap.Today.Sub(snap.LastBuyDate) < cooldown {
			return BuyIntent{}
		}
	}

	// ─── 選用進場 gate (旗標關閉時整段跳過,行為與原始 Baseline 相同) ───
	// 長期趨勢硬濾網:僅在收盤站上長均線時加碼 (#5)。
	if cfg.BuyLongMAWindow > 0 && cfg.BuyRequireAboveLongMA {
		if math.IsNaN(snap.LongMA) || snap.TodayPrice < snap.LongMA {
			return BuyIntent{}
		}
	}
	// 長均線斜率濾網:均線翻揚才加碼 (#30/#80)。
	if cfg.BuyLongMAWindow > 0 && cfg.BuyRequireLongMASlopeUp {
		if math.IsNaN(snap.LongMASlope) || snap.LongMASlope < 0 {
			return BuyIntent{}
		}
	}
	// 乖離率最小深度:跌得不夠深不進場 (#8/#17)。
	if cfg.BuyBiasMin > 0 {
		if snap.MA20 <= 0 {
			return BuyIntent{}
		}
		bias := (snap.MA20 - snap.TodayPrice) / snap.MA20
		if bias < cfg.BuyBiasMin {
			return BuyIntent{}
		}
	}
	// RSI 超賣 gate:RSI 夠低才進場 (#34)。
	if cfg.BuyRSIPeriod > 0 {
		if math.IsNaN(snap.RSIForBuy) || snap.RSIForBuy > cfg.BuyRSIMax {
			return BuyIntent{}
		}
	}
	// 止穩確認:今收須高於昨收 (#33)。
	if cfg.BuyConfirmUp {
		if !snap.HasPrevClose || snap.TodayPrice <= snap.PrevClose {
			return BuyIntent{}
		}
	}

	// 買入金額:bull 且設定固定大額時用固定額 (少次大額、省手續費);否則走 depth 表 (越深越大)。
	var amount float64
	if snap.IsBull && cfg.BullBuyAmount > 0 {
		amount = cfg.BullBuyAmount * cfg.BuyAndSellMultiplier
	} else {
		amount = baselineBuyAmountFromCfg(cfg, buyDepthPct(cfg, snap)) * cfg.BuyAndSellMultiplier
	}
	// 動態部位大小:金字塔形狀不變,只把絕對額按帳戶大小等比縮放 (cash 或 equity)。
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

	// 註:真‧砍倉/停損 (含 per-lot、bear-only 多形態) 移到引擎層 applyCuts 處理,
	// 因為它要對「個別 lot」操作,純函式 DecideSell 只看聚合值看不到單筆。

	// ── 移動停利 (regime):價跌破「持倉峰值×(1-trail)」即出場。預設全出;TrailStopSellFrac>0 則分批。 ──
	// 僅在部位曾達 TrailMinGain 獲利後才武裝,避免把剛逢低買進的部位停損掉。
	trail := cfg.TrailStopBear
	if snap.IsBull {
		trail = cfg.TrailStopBull
	}
	if trail > 0 && snap.PeakSinceHold > 0 && snap.HeldShares > 0 {
		peakGain := snap.PeakSinceHold/snap.LowestHeldPrice - 1
		if peakGain >= cfg.TrailMinGain && snap.TodayPrice <= snap.PeakSinceHold*(1-trail) {
			tShares := snap.HeldShares
			if cfg.TrailStopSellFrac > 0 && cfg.TrailStopSellFrac < 1 {
				tShares = int(math.Round(cfg.TrailStopSellFrac * float64(snap.HeldShares)))
				if tShares < 1 {
					tShares = 1
				}
			}
			return SellIntent{Should: true, TargetShares: tShares, Price: snap.TodayPrice, Reason: "trail"}
		}
	}

	// 賣出階梯 (金字塔/倒金字塔) 啟用時,獲利了結改由引擎層 applySellLadder 處理,此處跳過。
	if cfg.SellLadderMode != "" {
		return SellIntent{}
	}

	// ── 獲利出場門檻 (regime 切換):牛市可讓贏家多跑、熊市可提早鎖利。 ──
	thr := cfg.BaselineSellThreshold
	if snap.IsBull && cfg.SellThresholdBull > 0 {
		thr = cfg.SellThresholdBull
	} else if !snap.IsBull && cfg.SellThresholdBear > 0 {
		thr = cfg.SellThresholdBear
	}
	gain := (snap.TodayPrice - snap.LowestHeldPrice) / snap.LowestHeldPrice
	if gain < thr {
		return SellIntent{}
	}
	// 選用出場 gate:RSI 須達超買才賣 (#39/#94)。旗標關閉時跳過,行為不變。
	if cfg.SellRSIPeriod > 0 {
		if math.IsNaN(snap.RSIForSell) || snap.RSIForSell < cfg.SellRSIMin {
			return SellIntent{}
		}
	}
	// 賣出量:預設固定金額;SellFracOfPosition>0 則改為「賣掉持股的這個比例」(分批出場)。
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
	return SellIntent{
		Should:       true,
		TargetShares: shares,
		Price:        snap.TodayPrice,
		Reason:       "profit",
	}
}

// buyDepthPct 回傳「加碼深度」判斷值 (越負代表跌越深 → 命中越深的 tier → 買越多)。
// 由 cfg.BuyDepthBasis 決定基準;預設 "held_high" 與原始行為完全相同。
//
//	held_high:(今價-持倉最高買入價)/持倉最高買入價 (相對自己的成本,較粗糙)
//	ma       :(今價-進場均線)/進場均線 (乖離率,相對市場均線的折價)
//	peak     :(今價-近期高點)/近期高點 (距高點回撤,相對市場高點的折價)
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

// baselineBuyAmountFromCfg 依照 config 中 tier 決定買入目標金額,純函式。
//
// 選用旗標 (BuyBaseAmount>0 && BuyTierRatio>0, #9/#49):金額改用幾何級數
// base×ratio^i (i 為命中的 tier 索引;跌破最深 tier 時用 ratio^len 當 fallback),
// 沿用既有 tier 的 above 邊界。旗標關閉時與原始「逐 tier 查表」完全相同。
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
