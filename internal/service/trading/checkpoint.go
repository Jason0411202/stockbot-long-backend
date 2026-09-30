package trading

import "time"

// Clone creates an isolated candidate day; publish it only after persistence.
// Recorders belong to offline backtests and are deliberately not copied.
func (e *Engine) Clone() *Engine {
	c := *e
	c.rec = nil
	c.positions = make(map[string][]lot, len(e.positions))
	for k, v := range e.positions {
		c.positions[k] = append([]lot(nil), v...)
	}
	c.lastBuy = cloneMap(e.lastBuy)
	c.lastTrailSell = cloneMap(e.lastTrailSell)
	c.peakSinceHold = cloneMap(e.peakSinceHold)
	return &c
}

func cloneMap[T any](src map[string]T) map[string]T {
	out := make(map[string]T, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// RiskState preserves exact decision anchors, including an intraday opening
// peak which is not yet available in the daily history at restart time.
type RiskState struct {
	LastBuy       time.Time `json:"last_buy"`
	LastTrailSell time.Time `json:"last_trail_sell"`
	Peak          float64   `json:"peak"`
}

func (e *Engine) RiskState(id string) RiskState {
	return RiskState{e.lastBuy[id], e.lastTrailSell[id], e.peakSinceHold[id]}
}

func (e *Engine) SeedRiskState(id string, s RiskState) {
	if !s.LastBuy.IsZero() {
		e.lastBuy[id] = s.LastBuy
	}
	if !s.LastTrailSell.IsZero() {
		e.lastTrailSell[id] = s.LastTrailSell
	}
	e.peakSinceHold[id] = s.Peak
}

// HoldingValueAt uses the supplied opening marks for a provisional valuation.
func (e *Engine) HoldingValueAt(prices map[string]float64) float64 {
	total := 0.0
	for id, lots := range e.positions {
		for _, l := range lots {
			total += prices[id] * float64(l.shares)
		}
	}
	return total
}
