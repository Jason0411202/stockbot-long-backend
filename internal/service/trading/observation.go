// internal/service/trading/observation.go 唯讀觀察實際決策階段的條件與執行結果。
package trading

import (
	"fmt"
	"math"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
)

// DecisionObserver 接收單檔實際處理結果；由外層收集並與整日交易一起提交。
type DecisionObserver func(DecisionObservation)

// DecisionObservation 保存單檔買入前及賣出前各自使用的輸入。
type DecisionObservation struct {
	StockID, Status, Regime, RegimeMethod string
	IndicatorDate                         time.Time
	Price, RegimeValue, RegimeBase        *float64
	RegimeWindow                          int
	Buy, Sell                             DecisionPhase
}

// DecisionPhase 保存一個交易階段的判斷及實際成交前後狀態。
type DecisionPhase struct {
	Status                            string
	Reasons                           []string
	Conditions                        []DecisionCondition
	Metrics                           []DecisionMetric
	IntendedShares, ExecutedShares    int
	CashBefore, CashAfter             float64
	HeldSharesBefore, HeldSharesAfter int
}

// DecisionCondition 保存一個真正參與決策的閘門；百分比以 100 為一倍。
type DecisionCondition struct {
	ID, Label, Status, Operator, Unit, Description string
	Value, Threshold                               *float64
	RemainingDays                                  *int
	ReleaseDate                                    string
}

// DecisionMetric 補充部位大小及價格參考，不增加交易規則。
type DecisionMetric struct {
	ID, Label, Unit, Description string
	Value                        *float64
}

// SetDecisionObserver 設定純記憶體觀察回呼；nil 表示不收集。
func (e *Engine) SetDecisionObserver(observer DecisionObserver) { e.observer = observer }

// observationNumber 將缺值與非有限數字保留為無資料。
func observationNumber(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// condition 建立帶有實際比較結果的單一條件。
func condition(id, label string, value, threshold float64, operator, unit string, passed bool, description string) DecisionCondition {
	status := "blocked"
	if passed {
		status = "passed"
	}
	return DecisionCondition{ID: id, Label: label, Status: status, Value: observationNumber(value), Threshold: observationNumber(threshold), Operator: operator, Unit: unit, Description: description}
}

// metric 建立一個無額外觸發語意的參考數值。
func metric(id, label string, value float64, unit, description string) DecisionMetric {
	return DecisionMetric{ID: id, Label: label, Value: observationNumber(value), Unit: unit, Description: description}
}

// cooldownCondition 以引擎日期及完整 24 小時計算冷卻，不改用交易日。
func cooldownCondition(id, label string, today, anchor time.Time, hasAnchor bool, days int, enabled bool) DecisionCondition {
	c := condition(id, label, math.NaN(), float64(days), "gte", "days", true, "尚無前次交易紀錄，無需等待。")
	if !enabled {
		c.Status, c.Description = "inactive", "此股票未啟用這項冷卻。"
		c.Threshold = nil
		return c
	}
	remaining := 0
	c.RemainingDays = &remaining
	if !hasAnchor {
		return c
	}
	elapsed := today.Sub(anchor).Hours() / 24
	release := anchor.Add(time.Duration(days) * 24 * time.Hour)
	remaining = int(math.Max(0, math.Ceil(release.Sub(today).Hours()/24)))
	c.Value = observationNumber(elapsed)
	c.ReleaseDate = release.Format("2006-01-02")
	c.Description = fmt.Sprintf("前次日期 %s；須經過 %d 個日曆天。", anchor.Format("2006-01-02"), days)
	if today.Before(release) {
		c.Status = "blocked"
	}
	return c
}

// phaseStatus 彙總各閘門，保留所有阻擋原因而非只呈現第一項。
func phaseStatus(p *DecisionPhase) {
	p.Status = "eligible"
	p.Reasons = []string{}
	for _, c := range p.Conditions {
		if c.Status == "blocked" || c.Status == "unavailable" {
			p.Reasons = append(p.Reasons, c.Label+"："+c.Description)
			if c.Status == "unavailable" {
				p.Status = "unavailable"
			} else if p.Status != "unavailable" {
				p.Status = "blocked"
			}
		}
	}
}

// observeBuy 使用現有純函式取得買入閘門與部位大小，輸入凍結於成交前。
func (e *Engine) observeBuy(cfg *config.Config, snap Snapshot) DecisionPhase {
	p := DecisionPhase{CashBefore: snap.Cash, CashAfter: snap.Cash, HeldSharesBefore: snap.HeldShares, HeldSharesAfter: snap.HeldShares}
	band, fraction := 0.0, cfg.BearBuyFrac
	if snap.IsBull {
		band, fraction = cfg.BullBuyBand, cfg.BullBuyFrac
	}
	limit := snap.MA20 * (1 + band)
	priceGate := condition("entry_price", "進場價格", snap.TodayPrice, limit, "lt", "price", snap.TodayPrice > 0 && snap.TodayPrice < limit, "決策價必須嚴格低於進場均線乘上當前市況帶寬。")
	if math.IsNaN(snap.MA20) {
		priceGate.Status, priceGate.Description = "unavailable", "歷史資料不足以計算進場均線。"
	}
	cdDays := cfg.CooldownDays
	if snap.IsBull && cfg.BullCooldownDays > 0 {
		cdDays = cfg.BullCooldownDays
	}
	lastSell, hasSell := e.lastTrailSell[snap.StockID]
	p.Conditions = []DecisionCondition{
		priceGate,
		cooldownCondition("buy_cooldown", "買入間隔", snap.Today, snap.LastBuyDate, snap.HasLastBuy, cdDays, true),
		cooldownCondition("reentry_cooldown", "賣出後再進場冷卻", snap.Today, lastSell, hasSell, cfg.TrailReentryCooldownDays, cfg.TrailReentryCooldownDays > 0),
	}

	// 目標股數沿用四捨五入；可負擔股數沿用成交端向下取整。
	amount := buyAmount(cfg, snap)
	requested := amountToShares(amount, snap.TodayPrice)
	affordable := 0
	if snap.TodayPrice > 0 {
		affordable = int(math.Floor(snap.Cash / snap.TodayPrice))
	}
	p.Conditions = append(p.Conditions,
		condition("order_size", "買入預算", float64(requested), 1, "gte", "shares", requested > 0, "策略預算換算股數後須達本帳戶最小可成交數量；分割換算後門檻不一定為一股。"),
		condition("available_cash", "可用現金", snap.Cash, snap.TodayPrice, "gte", "currency", affordable > 0, "可用現金須足以支付最小可成交數量所需金額；買入不借款。"),
	)
	p.IntendedShares = DecideBuy(cfg, snap).Shares
	if cfg.TrailReentryCooldownDays > 0 && hasSell && snap.Today.Sub(lastSell) < time.Duration(cfg.TrailReentryCooldownDays)*24*time.Hour {
		p.IntendedShares = 0
	}

	// 深度只影響熊市加碼金額，並不是另一項進場閘門。
	depth := buyDepthPct(cfg, snap)
	weight := 1.0
	if !snap.IsBull {
		weight = bearDepthWeight(cfg, depth)
	}
	maWindow := cfg.MAWindow
	if maWindow <= 0 {
		maWindow = 20
	}
	p.Metrics = []DecisionMetric{
		metric("entry_ma", "進場均線", snap.MA20, "price", fmt.Sprintf("%d 日收盤均線，只使用決策時可見的資料。", maWindow)),
		metric("buy_band", "進場帶寬", band*100, "percent", "多頭依設定放寬；空頭為零。"),
		metric("buy_depth", "加碼深度", -depth*100, "percent", "正值表示低於深度基準；此值只決定熊市加碼倍率。"),
		metric("buy_fraction", "基礎買入比例", fraction*100, "percent", "目前市況採用的基礎資金比例。"),
		metric("depth_multiplier", "加碼倍率", weight, "ratio", "熊市使用設定的幾何深度權重；多頭為一倍。"),
		metric("effective_fraction", "實際預算比例", fraction*weight*100, "percent", "基礎買入比例乘上加碼倍率，仍受現金夾取。"),
		metric("budget_basis", "預算基礎", fracBasis(cfg, snap), "currency", "資金基準："+cfg.BuyFracBasis),
		metric("target_amount", "策略預算", amount, "currency", "尚未取整股數及夾取可用現金的目標金額。"),
	}
	if cfg.BuyDepthBasis == "peak" {
		lookback := cfg.BuyPeakLookback
		if lookback <= 0 {
			lookback = 252
		}
		p.Metrics = append(p.Metrics, metric("recent_peak", "近期收盤高點", snap.RecentPeak, "price", fmt.Sprintf("截至指標日期最近 %d 個交易日的最高收盤價。", lookback)))
	}
	if cfg.BuyDepthBasis == "held_high" || cfg.BuyDepthBasis == "" {
		highest := snap.HighestHeldPrice
		if highest <= 0 {
			highest = math.NaN()
		}
		p.Metrics = append(p.Metrics, metric("highest_cost", "最高持倉成本", highest, "price", "目前尚未賣出部位的最高買入成本。"))
	}
	for i, tier := range cfg.BaselineBuyTiers {
		p.Metrics = append(p.Metrics, metric(fmt.Sprintf("depth_tier_%d", i+1), fmt.Sprintf("加碼分層 %d", i+1), -tier.Above*100, "percent", "熊市回落達此邊界時進入下一層倍率。"))
	}
	phaseStatus(&p)
	return p
}

// observeSell 在買入完成且持倉峰值更新後觀察唯一的熊市移動停利。
func observeSell(cfg *config.Config, snap Snapshot, cash float64) DecisionPhase {
	p := DecisionPhase{CashBefore: cash, CashAfter: cash, HeldSharesBefore: snap.HeldShares, HeldSharesAfter: snap.HeldShares}
	regime := 0.0
	if snap.IsBull {
		regime = 1
	}
	peakGain, drawdown := math.NaN(), math.NaN()
	if snap.LowestHeldPrice > 0 && snap.PeakSinceHold > 0 {
		peakGain = (snap.PeakSinceHold/snap.LowestHeldPrice - 1) * 100
		drawdown = (1 - snap.TodayPrice/snap.PeakSinceHold) * 100
	}
	p.Conditions = []DecisionCondition{
		condition("held_position", "持有部位", float64(snap.HeldShares), 0, "gt", "shares", snap.HeldShares > 0 && snap.LowestHeldPrice > 0, "需要有尚未賣出的持股。"),
		condition("bear_regime", "空頭市況", regime, 0, "eq", "regime", !snap.IsBull, "只有空頭啟用移動停利；多頭持股續抱。"),
		condition("trail_enabled", "移動停利設定", cfg.TrailStopBear*100, 0, "gt", "percent", cfg.TrailStopBear > 0, "回落門檻須大於零才啟用。"),
		condition("peak_gain", "獲利保護啟動", peakGain, cfg.TrailMinGain*100, "gte", "percent", !math.IsNaN(peakGain) && snap.PeakSinceHold/snap.LowestHeldPrice-1 >= cfg.TrailMinGain, "持倉峰值相對最低持倉成本的獲利須先達門檻。"),
		condition("trailing_drawdown", "峰值回落", drawdown, cfg.TrailStopBear*100, "gte", "percent", snap.PeakSinceHold > 0 && snap.TodayPrice <= snap.PeakSinceHold*(1-cfg.TrailStopBear), "決策價低於或等於持倉峰值乘上（1 − 回落門檻）時全數出場。"),
	}
	if snap.HeldShares <= 0 || snap.LowestHeldPrice <= 0 {
		for i := 3; i < len(p.Conditions); i++ {
			p.Conditions[i].Status = "inactive"
			p.Conditions[i].Value = nil
			p.Conditions[i].Description = "目前無持股，此條件不適用。"
		}
	}
	peak, lowest, stopPrice := snap.PeakSinceHold, snap.LowestHeldPrice, math.NaN()
	if peak <= 0 {
		peak = math.NaN()
	}
	if lowest <= 0 {
		lowest = math.NaN()
	}
	if snap.PeakSinceHold > 0 {
		stopPrice = snap.PeakSinceHold * (1 - cfg.TrailStopBear)
	}
	p.Metrics = []DecisionMetric{
		metric("holding_peak", "持倉期間峰值", peak, "price", "含本次決策價的持倉期間最高決策價，並非盤中最高價。"),
		metric("lowest_cost", "最低持倉成本", lowest, "price", "賣出判斷當下的最低買入成本；包含本次剛買入的部位。"),
		metric("trailing_price", "移動停利觸發價", stopPrice, "price", "只有其他賣出條件也滿足時才會全數出場。"),
	}
	p.IntendedShares = DecideSell(cfg, snap).TargetShares
	phaseStatus(&p)
	if snap.HeldShares <= 0 || snap.LowestHeldPrice <= 0 {
		p.Status, p.Reasons = "inactive", []string{"目前無持股，移動停利不適用。"}
	} else if cfg.TrailStopBear <= 0 {
		p.Status, p.Reasons = "inactive", []string{"此股票未啟用移動停利。"}
	}
	return p
}

// observeStock 補上實際採用的市況比較與指標截止日期。
func (e *Engine) observeStock(cfg *config.Config, snap Snapshot, s *StockSeries, index int) *DecisionObservation {
	if e.observer == nil {
		return nil
	}
	o := &DecisionObservation{StockID: snap.StockID, Status: "evaluated", IndicatorDate: s.Dates[index], Price: observationNumber(snap.TodayPrice), Regime: "bear", RegimeMethod: cfg.RegimeMethod, RegimeWindow: cfg.RegimeMAWindow, Buy: e.observeBuy(cfg, snap)}
	if snap.IsBull {
		o.Regime = "bull"
	}
	if o.RegimeWindow <= 0 {
		o.RegimeWindow = 200
	}
	switch cfg.RegimeMethod {
	case "ma_pos":
		o.RegimeValue, o.RegimeBase = observationNumber(s.ClosePrices[index]), observationNumber(s.maAt(index, o.RegimeWindow))
	case "ma_slope":
		lookback := cfg.RegimeLookback
		if lookback <= 0 {
			lookback = 200
		}
		o.RegimeValue, o.RegimeBase = observationNumber(s.maAt(index, o.RegimeWindow)), observationNumber(s.maAt(index-lookback, o.RegimeWindow))
	case "mom":
		lookback := cfg.RegimeLookback
		if lookback <= 0 {
			lookback = 252
		}
		o.RegimeValue = observationNumber(s.ClosePrices[index])
		if index >= lookback {
			o.RegimeBase = observationNumber(s.ClosePrices[index-lookback])
		}
	}
	return o
}

// finishPhase 填入真實執行後狀態，不重新判斷成交前條件。
func (e *Engine) finishPhase(stockID string, p *DecisionPhase, buy bool) {
	p.CashAfter = e.cash
	p.HeldSharesAfter = e.buildSnapshot(stockID, time.Time{}, 0, 0).HeldShares
	p.ExecutedShares = p.HeldSharesAfter - p.HeldSharesBefore
	if !buy {
		p.ExecutedShares = -p.ExecutedShares
	}
	if p.ExecutedShares > 0 {
		p.Status = "executed"
	}
}

// observeSkipped 保留未評估股票的明確原因，不製造買賣訊號。
func (e *Engine) observeSkipped(stockID, status, reason string) {
	if e.observer == nil {
		return
	}
	held := e.buildSnapshot(stockID, time.Time{}, 0, 0).HeldShares
	p := DecisionPhase{Status: status, Reasons: []string{reason}, Conditions: []DecisionCondition{}, Metrics: []DecisionMetric{}, CashBefore: e.cash, CashAfter: e.cash, HeldSharesBefore: held, HeldSharesAfter: held}
	e.observer(DecisionObservation{StockID: stockID, Status: status, Regime: "unavailable", Buy: p, Sell: p})
}
