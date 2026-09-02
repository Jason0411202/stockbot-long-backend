// internal/service/trading/decisions.go 實作純策略買賣決策，不產生任何 I/O 副作用。
package trading

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"math"
	"time"
)

// BuyIntent 是 DecideBuy 的純函式輸出。
// Shares 為「策略想買的目標股數」,不考慮現金限制 — 執行層需再做夾取。
type BuyIntent struct {
	Should bool
	Shares int
	Price  float64 // = 當日成交價 (close 基準=收盤;open 基準=開盤)

	// TradeReason 為本筆買入的決策理由 (決策端欄位已填;成交股數 / 金額 / 餘額由引擎 apply 時補上)。
	TradeReason TradeReason
}

// SellIntent 是 DecideSell 的純函式輸出。
type SellIntent struct {
	Should       bool
	TargetShares int
	Price        float64 // = 當日成交價 (close 基準=收盤;open 基準=開盤)
	Reason       string  // "trail";供統計觸發次數

	// TradeReason 為本筆賣出的決策理由 (決策端欄位已填;成交股數 / 金額 / 餘額由引擎 apply 時補上)。
	TradeReason TradeReason
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
	RecentPeak    float64 // 近期高點 (peak 深度基準 / 深跌判斷用);未啟用為 NaN
	Cash          float64 // 當前現金 (動態部位大小用)
	Equity        float64 // 當前總權益 = 現金 + 持股市值 (動態部位大小用)
	PeakSinceHold float64 // 持倉期間 (含今日) 的最高決策價;移動停利用。無持倉/未追蹤為 0
	HeldShares    int     // 目前持有總股數;移動停利全出時用
}

// DecideBuy 是買入判斷,純函式,不產生任何副作用。
// 規則:
//  1. 當日有正常價格、有進場均線。
//  2. 觸發:今價 < 進場均線×(1+band);bull 用 BullBuyBand 放寬,bear 嚴格 (band=0)。
//  3. 冷卻 (passesCooldown):距上次買入須滿冷卻天數 (bull 可用 BullCooldownDays)。
//  4. 金額 (buyAmount):買「現金/權益基準的固定比例」— 牛市 ×BullBuyFrac;熊市 ×BearBuyFrac×幾何深度權重。
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

	if !passesCooldown(cfg, snap) {
		return BuyIntent{}
	}

	shares := amountToShares(buyAmount(cfg, snap), snap.TodayPrice)
	if shares <= 0 {
		return BuyIntent{}
	}
	// 組裝決策端理由 (regime / 進場均線 / 帶寬 / 深度);成交股數與金額由引擎 apply 時補上。
	regime := "bear"
	if snap.IsBull {
		regime = "bull"
	}
	reason := TradeReason{
		Action: "buy", Trigger: "dip", Regime: regime,
		Price: snap.TodayPrice, EntryMA: snap.MA20, BandPct: band,
		DepthPct: buyDepthPct(cfg, snap),
	}
	return BuyIntent{Should: true, Shares: shares, Price: snap.TodayPrice, TradeReason: reason}
}

// passesCooldown 判斷是否通過買進冷卻:無上次買入,或距上次買入已滿冷卻天數 (bull 可用 BullCooldownDays) → 通過。
func passesCooldown(cfg *config.Config, snap Snapshot) bool {
	// 無上次買入紀錄,冷卻尚未啟動,直接放行。
	if !snap.HasLastBuy {
		return true
	}
	// 多頭時若設有較短冷卻天數則改用之,加快進場頻率。
	cdDays := cfg.CooldownDays
	if snap.IsBull && cfg.BullCooldownDays > 0 {
		cdDays = cfg.BullCooldownDays
	}
	return snap.Today.Sub(snap.LastBuyDate) >= time.Duration(cdDays)*24*time.Hour
}

// buyAmount 回傳本次買入的目標金額 (現金夾取前):買「現金/權益基準的固定比例」。
//
//	牛市 = 基準 × BullBuyFrac。
//	熊市 = 基準 × BearBuyFrac × 幾何深度權重 (跌越深買越大比例;永遠留現金尾巴 → 深跌仍有錢)。
//
// 基準 (cash/equity) 由 BuyFracBasis 決定;基準 <= 0 (沒錢) 回傳 0 → 不買。
func buyAmount(cfg *config.Config, snap Snapshot) float64 {
	basis := fracBasis(cfg, snap)
	if basis <= 0 {
		return 0
	}
	if snap.IsBull {
		return basis * cfg.BullBuyFrac
	}
	return basis * cfg.BearBuyFrac * bearDepthWeight(cfg, buyDepthPct(cfg, snap))
}

// fracBasis 回傳比例買入的基準金額 (現金或權益)。
func fracBasis(cfg *config.Config, snap Snapshot) float64 {
	switch cfg.BuyFracBasis {
	case "cash":
		return snap.Cash
	case "equity":
		return snap.Equity
	}
	return 0
}

// bearDepthWeight 回傳熊市「跌越深買越多」的幾何權重 (ratio^命中tier索引),供比例買入縮放。
func bearDepthWeight(cfg *config.Config, depthPct float64) float64 {
	// ratio <= 0 時退回 1 (權重恆為 1,等同不放大)。
	ratio := cfg.BuyTierRatio
	if ratio <= 0 {
		ratio = 1
	}
	// 從 depthPct 最淺的 tier 往下比對;命中第一個符合層回傳對應倍率。
	for i, tier := range cfg.BaselineBuyTiers {
		if depthPct > tier.Above {
			return math.Pow(ratio, float64(i))
		}
	}
	// 超過所有 tier 門檻 (跌最深) 時使用最高倍率。
	return math.Pow(ratio, float64(len(cfg.BaselineBuyTiers)))
}

// DecideSell 是賣出判斷,純函式。唯一的賣出路徑為熊市移動停利 (保護式全出);多頭不賣出、持股續抱。
//   - 僅熊市生效,且部位曾自最低成本獲利 ≥ TrailMinGain 才武裝 (不在尚未獲利時就把剛逢低買進的部位停損掉)。
//   - 價跌破「持倉峰值×(1-TrailStopBear)」即全數出場。
func DecideSell(cfg *config.Config, snap Snapshot) SellIntent {
	if snap.TodayPrice <= 0 || snap.LowestHeldPrice <= 0 {
		return SellIntent{}
	}
	// 多頭、未啟用移動停利、無峰值紀錄或無持股:不賣。
	if snap.IsBull || cfg.TrailStopBear <= 0 || snap.PeakSinceHold <= 0 || snap.HeldShares <= 0 {
		return SellIntent{}
	}
	// 武裝條件與觸發條件:峰值相對最低成本獲利達門檻,且今價自峰值回落達停利幅度。
	peakGain := snap.PeakSinceHold/snap.LowestHeldPrice - 1
	if peakGain < cfg.TrailMinGain || snap.TodayPrice > snap.PeakSinceHold*(1-cfg.TrailStopBear) {
		return SellIntent{}
	}
	reason := TradeReason{
		Action: "sell", Trigger: "trail", Regime: "bear",
		Price: snap.TodayPrice, GainPct: peakGain, TrailStopPct: cfg.TrailStopBear,
	}
	return SellIntent{Should: true, TargetShares: snap.HeldShares, Price: snap.TodayPrice, Reason: "trail", TradeReason: reason}
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
		// 乖離率:(今價 - 均線) / 均線。
		if snap.MA20 > 0 {
			return (snap.TodayPrice - snap.MA20) / snap.MA20
		}
		return 0
	case "peak":
		// 距高點回撤率:(今價 - 近期高點) / 近期高點。
		if snap.RecentPeak > 0 {
			return (snap.TodayPrice - snap.RecentPeak) / snap.RecentPeak
		}
		return 0
	default: // "" / "held_high"
		// 相對持倉最高買入成本:(今價 - 最高買入價) / 最高買入價。
		if snap.HighestHeldPrice > 0 {
			return (snap.TodayPrice - snap.HighestHeldPrice) / snap.HighestHeldPrice
		}
		return 0
	}
}

// amountToShares 將金額轉為最接近的股數 (四捨五入)。
func amountToShares(amount float64, price float64) int {
	if price <= 0 || amount <= 0 {
		return 0
	}
	return int(math.Round(amount / price))
}
