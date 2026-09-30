package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// ClosedEquityReader revalues the existing ledger, preserving its historical
// trades and cash. Old snapshots remain available in DB for audit/rollback.
// The same buy can have several realized fragments plus an unsold remainder.
type ClosedEquityReader struct {
	source EquityStore
	ledger LedgerStore
	series SeriesLoader
	cfg    *config.Config
}

func NewClosedEquityReader(source EquityStore, ledger LedgerStore, series SeriesLoader, cfg *config.Config) *ClosedEquityReader {
	return &ClosedEquityReader{source, ledger, series, cfg}
}

func (r *ClosedEquityReader) ListEquityAsc(ctx context.Context) ([]entity.EquitySnapshot, error) {
	snaps, err := r.source.ListEquityAsc(ctx)
	if err != nil {
		return nil, err
	}
	lots, err := r.ledger.ListUnrealized(ctx)
	if err != nil {
		return nil, err
	}
	realized, err := r.ledger.ListRealized(ctx)
	if err != nil {
		return nil, err
	}
	series, err := LoadTradingSeries(ctx, r.series, r.cfg.TrackStocks)
	if err != nil {
		return nil, err
	}
	type change struct {
		date, id string
		shares   int
		cost     float64
	}
	var changes []change
	for _, lot := range lots {
		day, err := parseLedgerDate(lot.TransactionDate)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change{day.Format(dateLayout), lot.StockID, lot.Shares, lot.InvestmentCost})
	}
	for _, lot := range realized {
		buy, err := parseLedgerDate(lot.BuyDate)
		if err != nil {
			return nil, err
		}
		sell, err := parseLedgerDate(lot.SellDate)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change{buy.Format(dateLayout), lot.StockID, lot.Shares, lot.InvestmentCost}, change{sell.Format(dateLayout), lot.StockID, -lot.Shares, -lot.InvestmentCost})
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].date < changes[j].date })
	held := map[string]int{}
	cost := 0.0
	next := 0
	out := make([]entity.EquitySnapshot, 0, len(snaps))
	for _, snap := range snaps {
		for next < len(changes) && changes[next].date <= snap.Date {
			c := changes[next]
			held[c.id] += c.shares
			cost += c.cost
			next++
		}
		value := 0.0
		complete := true
		for _, id := range r.cfg.TrackStocks {
			if held[id] < 0 {
				return nil, fmt.Errorf("negative reconstructed holdings %s on %s", id, snap.Date)
			}
			ss := series[id]
			if ss == nil {
				return nil, fmt.Errorf("missing valuation series %s", id)
			}
			i, ok := ss.DateIndex[snap.Date]
			if !ok {
				day, err := time.Parse(dateLayout, snap.Date)
				if err != nil {
					return nil, err
				}
				if ss.Suspended(day) {
					price, found := ss.CloseAsOf(day)
					if found {
						value += float64(held[id]) * price
						continue
					}
				}
				complete = false
				break
			}
			value += float64(held[id]) * ss.ClosePrices[i]
		}
		if !complete {
			continue
		} // Opening provisional snapshots never masquerade as closes.
		// Historical engine snapshots also contain rounded partial-sale cost
		// drift. The complete ledger fragments are the accounting source here.
		snap.HoldingValue = value
		snap.TotalEquity = snap.Cash + value
		snap.CostBasis = cost
		out = append(out, snap)
	}
	return out, nil
}
