// internal/service/trading/historical_observation.go 以真實帳本成交推進記憶體狀態並重建當日策略條件。
package trading

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// HistoricalTrade 保存永久記帳單位的當日真實成交；賣單以 BuyDate 指定原始買入批次。
type HistoricalTrade struct {
	StockID string
	Buy     bool
	Shares  int
	Price   float64
	BuyDate time.Time
}

// ObserveHistoricalDay 依追蹤順序重建買前與賣前條件，只套用 actual 的真實成交。
// 呼叫前須餵入當日注資後、成交前的現金、持倉及風險狀態；成功才發布記憶體變更。
// 不呼叫 Executor、Recorder 或外部 DecisionObserver，也不累計交易統計。
func (e *Engine) ObserveHistoricalDay(today time.Time, series map[string]*StockSeries, actual []HistoricalTrade) ([]DecisionObservation, error) {
	// 先驗證帳本與期初現金，再於隔離副本組裝整日觀察。
	trades, err := e.validateHistoricalTrades(today, actual)
	if err != nil {
		return nil, err
	}
	candidate := e.Clone()
	if err := candidate.checkHistoricalCash(); err != nil {
		return nil, err
	}
	observations := make([]DecisionObservation, 0, len(e.cfg.TrackStocks))
	candidate.observer = func(o DecisionObservation) { observations = append(observations, o) }
	equity := 0.0
	if e.cfg.BuyFracBasis == "equity" {
		asOf := today
		if e.cfg.DecisionPriceBasis == "open" {
			asOf = today.AddDate(0, 0, -1)
		}
		equity, err = candidate.historicalEquity(series, asOf)
		if err != nil {
			return nil, err
		}
	}

	// 每檔股票固定先觀察買入、套入真實買單，再觀察並套入真實賣單。
	for _, id := range e.cfg.TrackStocks {
		if err := candidate.observeHistoricalStock(id, today, series[id], trades[id], equity); err != nil {
			return nil, fmt.Errorf("historical observation %s on %s: %w", id, today.Format(time.DateOnly), err)
		}
	}

	// 整日成功才發布狀態，保留原引擎的回呼與交易統計。
	e.cash = candidate.cash
	e.positions = candidate.positions
	e.lastBuy = candidate.lastBuy
	e.lastTrailSell = candidate.lastTrailSell
	e.peakSinceHold = candidate.peakSinceHold
	return observations, nil
}

// validateHistoricalTrades 驗證成交資料與股票歸屬，並保留每檔股票的帳本順序。
func (e *Engine) validateHistoricalTrades(today time.Time, actual []HistoricalTrade) (map[string][]HistoricalTrade, error) {
	// 建立追蹤清單與每股唯一決策價索引。
	if today.IsZero() {
		return nil, fmt.Errorf("historical observation requires a decision date")
	}
	known := make(map[string]bool, len(e.cfg.TrackStocks))
	for _, id := range e.cfg.TrackStocks {
		known[id] = true
	}
	out := make(map[string][]HistoricalTrade)
	prices := make(map[string]float64)

	// 每筆成交須有正股數、有限價格及可辨識的原始買入日期。
	for _, trade := range actual {
		if !known[trade.StockID] {
			return nil, fmt.Errorf("historical trade has untracked stock %q", trade.StockID)
		}
		if trade.Shares <= 0 || !historicalPositive(trade.Price) || !historicalPositive(float64(trade.Shares)*trade.Price) {
			return nil, fmt.Errorf("historical trade %s has invalid shares or price", trade.StockID)
		}
		if price, ok := prices[trade.StockID]; ok && price != trade.Price {
			return nil, fmt.Errorf("historical trades %s have different decision prices", trade.StockID)
		}
		if trade.Buy {
			if !trade.BuyDate.IsZero() && trade.BuyDate.Format(time.DateOnly) != today.Format(time.DateOnly) {
				return nil, fmt.Errorf("historical buy %s does not match decision date", trade.StockID)
			}
		} else if trade.BuyDate.IsZero() || trade.BuyDate.Format(time.DateOnly) > today.Format(time.DateOnly) {
			return nil, fmt.Errorf("historical sell %s has invalid lot date", trade.StockID)
		}
		prices[trade.StockID] = trade.Price
		out[trade.StockID] = append(out[trade.StockID], trade)
	}
	return out, nil
}

// observeHistoricalStock 重用既有條件觀察器，持倉及現金只來自帳本成交。
func (e *Engine) observeHistoricalStock(id string, today time.Time, s *StockSeries, trades []HistoricalTrade, equity float64) error {
	// 缺少當日資料時保留不可用原因；已有成交卻無指標則拒絕重建。
	price, index, status, reason := historicalInputs(today, s, e.cfg.DecisionPriceBasis == "open", trades)
	if reason != "" {
		if len(trades) > 0 {
			return fmt.Errorf("cannot observe recorded trade: %s", reason)
		}
		e.observeSkipped(id, status, reason)
		return nil
	}
	eff := e.cfg.ForStock(id)
	entryMA := s.MA20[index]
	if eff.MAWindow > 0 && eff.MAWindow != 20 {
		entryMA = s.maAt(index, eff.MAWindow)
	}
	build := func() Snapshot {
		snap := e.buildSnapshot(id, today, price, entryMA)
		e.applyGateInputs(&snap, s, index)
		snap.IsBull = eff.RegimeMethod != "" && regimeBull(eff, s, index)
		snap.Cash, snap.Equity = e.cash, equity
		return snap
	}
	observation := e.observeStock(eff, build(), s, index)

	// 真實買單更新現金、持倉及買入日期，不使用現行規則的買入意圖。
	for _, trade := range trades {
		if !trade.Buy {
			continue
		}
		e.cash -= float64(trade.Shares) * trade.Price
		if err := e.checkHistoricalCash(); err != nil {
			return err
		}
		e.positions[id] = append(e.positions[id], lot{date: today, shares: trade.Shares, price: trade.Price})
		e.lastBuy[id] = today
	}
	e.finishPhase(id, &observation.Buy, true)
	if len(e.positions[id]) > 0 && price > e.peakSinceHold[id] {
		e.peakSinceHold[id] = price
	}

	// 賣出條件包含當日剛買入的真實 lot，再依帳本指定批次扣除股數。
	observation.Sell = observeSell(eff, build(), e.cash)
	for _, trade := range trades {
		if trade.Buy {
			continue
		}
		if err := e.applyHistoricalSell(today, trade); err != nil {
			return err
		}
	}
	e.finishPhase(id, &observation.Sell, false)
	e.observer(*observation)
	return nil
}

// historicalInputs 選取帳本決策價或當日行情，開盤基準的指標固定截止前一交易日。
func historicalInputs(today time.Time, s *StockSeries, openBasis bool, trades []HistoricalTrade) (float64, int, string, string) {
	// 驗證行情形狀與停牌狀態，避免把缺值當成有效決策。
	if s == nil || len(s.Dates) == 0 || len(s.Dates) != len(s.ClosePrices) || len(s.MA20) != len(s.Dates) {
		return 0, 0, "unavailable", "缺少完整股票歷史資料。"
	}
	if s.Suspended(today) {
		return 0, 0, "inactive", "該股票於此決策日暫停交易。"
	}
	index := sort.Search(len(s.Dates), func(i int) bool { return s.Dates[i].Format(time.DateOnly) >= today.Format(time.DateOnly) })
	hasBar := index < len(s.Dates) && s.Dates[index].Format(time.DateOnly) == today.Format(time.DateOnly)
	price := 0.0
	if len(trades) > 0 {
		price = trades[0].Price
	} else if hasBar {
		if !openBasis {
			price = s.ClosePrices[index]
		} else if index < len(s.OpenPrices) {
			price = s.OpenPrices[index]
		}
	}
	if !historicalPositive(price) {
		return 0, 0, "unavailable", "缺少當日決策基準的有效價格。"
	}

	// 開盤成交只使用先前收盤；收盤決策必須有當日指標。
	if openBasis {
		index--
	} else if !hasBar {
		return 0, 0, "unavailable", "缺少當日收盤指標資料。"
	}
	if index < 0 || !historicalPositive(s.ClosePrices[index]) {
		return 0, 0, "unavailable", "尚無決策時可見的收盤指標資料。"
	}
	return price, index, "evaluated", ""
}

// applyHistoricalSell 僅扣除指定買入日的真實持倉，並更新出場風險狀態。
func (e *Engine) applyHistoricalSell(today time.Time, trade HistoricalTrade) error {
	// 依原始批次日期扣除，支援部分成交及同日新買入批次。
	remaining := trade.Shares
	positions := make([]lot, 0, len(e.positions[trade.StockID]))
	for _, position := range e.positions[trade.StockID] {
		if remaining > 0 && position.date.Format(time.DateOnly) == trade.BuyDate.Format(time.DateOnly) {
			sold := position.shares
			if sold > remaining {
				sold = remaining
			}
			position.shares -= sold
			remaining -= sold
		}
		if position.shares > 0 {
			positions = append(positions, position)
		}
	}
	if remaining > 0 {
		return fmt.Errorf("historical sell %s exceeds lot %s by %d shares", trade.StockID, trade.BuyDate.Format(time.DateOnly), remaining)
	}

	// 帳本賣出日作為重建的出場冷卻基準，全部出清時重設峰值。
	e.positions[trade.StockID] = positions
	e.cash += float64(trade.Shares) * trade.Price
	if err := e.checkHistoricalCash(); err != nil {
		return err
	}
	e.lastTrailSell[trade.StockID] = today
	if len(positions) == 0 {
		e.peakSinceHold[trade.StockID] = 0
	}
	return nil
}

// checkHistoricalCash 接受日終現金分位取整的一分誤差，拒絕實質借款及非有限數值。
func (e *Engine) checkHistoricalCash() error {
	if math.IsNaN(e.cash) || math.IsInf(e.cash, 0) || e.cash < -0.010000001 {
		return fmt.Errorf("historical cash is invalid: %.8f", e.cash)
	}
	if e.cash < 0 {
		e.cash = 0
	}
	return nil
}

// historicalPositive 判定股價或成交金額是否為有限正數。
func historicalPositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// historicalEquity 以可見收盤估值，缺少持倉行情時不產生虛假的預算基準。
func (e *Engine) historicalEquity(series map[string]*StockSeries, asOf time.Time) (float64, error) {
	// 每個持倉都必須有可見且有效的收盤價。
	equity := e.cash
	for id, positions := range e.positions {
		if len(positions) == 0 {
			continue
		}
		s := series[id]
		if s == nil || len(s.Dates) != len(s.ClosePrices) {
			return 0, fmt.Errorf("historical equity lacks series for %s", id)
		}
		price, ok := s.CloseAsOf(asOf)
		if !ok || !historicalPositive(price) {
			return 0, fmt.Errorf("historical equity lacks visible close for %s", id)
		}
		for _, position := range positions {
			equity += float64(position.shares) * price
		}
	}
	if math.IsNaN(equity) || math.IsInf(equity, 0) {
		return 0, fmt.Errorf("historical equity is not finite")
	}
	return equity, nil
}
