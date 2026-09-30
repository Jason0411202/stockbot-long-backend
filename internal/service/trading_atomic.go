package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/metrics"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

type TradingCalendar interface {
	IsTradingDay(context.Context, time.Time) (bool, error)
	PreviousTradingDay(context.Context, time.Time) (time.Time, error)
}
type tradingTransaction interface {
	WithTradingTransaction(context.Context, func(context.Context) error) error
}

type tradeEvent struct {
	stockID, date string
	reason        trading.TradeReason
}

func (e *tradingExecutor) emit(event tradeEvent) {
	if e.deferred {
		e.events = append(e.events, event)
		return
	}
	e.publish(event)
}

func (e *tradingExecutor) publish(event tradeEvent) {
	event.reason = event.reason.InDisplayUnits(displayFactor(e.svc.series, event.stockID, event.date))
	action, title, color := "買入成交", "🟥 買入成交", buyColor
	if event.reason.Action == "sell" {
		action, title, color = "賣出成交", "🟩 賣出成交", sellColor
	}
	e.logTrade(action, event.stockID, event.date, event.reason)
	metrics.IncTrade(event.reason.Action)
	if e.notify && e.svc.notify != nil {
		if err := e.svc.notify.SendTradeEmbed(buildTradeNotification(title, color, event.stockID, event.date, event.reason)); err != nil {
			e.svc.log.WithError(err).Error("發送成交通知失敗")
		}
	}
}

func (s *TradingService) SetCalendar(calendar TradingCalendar) { s.calendar = calendar }

// commitDay publishes memory and notifications only after the whole day commits.
// The database lock, watermark check and persisted cash check fence stale writers.
func (s *TradingService) commitDay(ctx context.Context, day time.Time, series map[string]*trading.StockSeries, opens map[string]float64, notify bool) error {
	uow, ok := s.state.(tradingTransaction)
	if !ok {
		return fmt.Errorf("atomic trading transaction is required")
	}
	expected, err := s.loadWatermark(ctx)
	if err != nil {
		return err
	}
	if !expected.IsZero() && !day.After(expected) {
		return nil
	}
	if s.seeded && !expected.Equal(s.engineDate) {
		s.needsReload = true
		return fmt.Errorf("engine watermark is stale; reload required")
	}
	candidate := *s
	candidate.engine = s.engine.Clone()
	exec := &tradingExecutor{svc: &candidate, notify: notify, deferred: true}
	err = uow.WithTradingTransaction(ctx, func(txctx context.Context) error {
		exec.ctx = txctx
		prev, err := s.loadWatermark(txctx)
		if err != nil {
			return err
		}
		if !prev.Equal(expected) {
			return fmt.Errorf("trading watermark changed; reload required")
		}
		cash, has, err := s.loadCash(txctx)
		if err != nil {
			return err
		}
		if has && cash != s.engine.Cash() {
			return fmt.Errorf("persisted cash changed; reload required")
		}
		contrib := backtest.ContributionDue(prev, day, s.cfg.MonthlyContribution)
		candidate.engine.AddCash(contrib)
		if opens == nil {
			err = candidate.engine.ProcessDay(day, series, exec)
		} else {
			err = candidate.engine.ProcessOpenDecision(day, opens, series, exec)
		}
		if err != nil {
			return err
		}
		if err = candidate.saveCash(txctx, candidate.engine.Cash()); err != nil {
			return err
		}
		if contrib > 0 {
			if err = candidate.addTotalContributed(txctx, contrib); err != nil {
				return err
			}
		}
		for _, id := range s.cfg.TrackStocks {
			b, err := json.Marshal(candidate.engine.RiskState(id))
			if err != nil {
				return err
			}
			if err = s.state.Set(txctx, "risk_"+id, string(b)); err != nil {
				return err
			}
		}
		// Opening marks are provisional. Only completed daily bars become history.
		if opens == nil {
			if err = candidate.writeEquitySnapshot(txctx, day, series); err != nil {
				return err
			}
		}
		return s.saveWatermark(txctx, day)
	})
	if err != nil {
		// A lost COMMIT acknowledgement can mean the DB committed successfully.
		// Force an authoritative reload before *any* later attempt.
		s.needsReload = true
		return err
	}
	s.engine = candidate.engine
	s.engineDate = day
	s.seeded = true
	holding := s.engine.HoldingValueAsOf(series, day)
	if opens != nil {
		marks := make(map[string]float64, len(series))
		for id, ss := range series {
			marks[id], _ = ss.CloseAsOf(day)
		}
		for id, price := range opens {
			marks[id] = price
		}
		holding = s.engine.HoldingValueAt(marks)
	}
	metrics.SetPortfolioSnapshot(s.engine.Cash(), holding, s.engine.Cash()+holding, s.engine.CostBasis())
	metrics.SetLastProcessedDate(day)
	for _, event := range exec.events {
		exec.publish(event)
	}
	return nil
}

func (s *TradingService) writeEquitySnapshot(ctx context.Context, day time.Time, series map[string]*trading.StockSeries) error {
	cash := s.engine.Cash()
	holding := s.engine.HoldingValueAsOf(series, day)
	return s.equity.RecordEquity(ctx, entity.EquitySnapshot{Date: day.Format(dateLayout), Cash: cash, HoldingValue: holding, TotalEquity: cash + holding, CostBasis: s.engine.CostBasis()})
}

// Revalue the latest committed day before advancing past it. At the next
// opening this also repairs a missed post-close run, without replaying trades.
func (s *TradingService) finalizeLatest(ctx context.Context, series map[string]*trading.StockSeries) error {
	day, err := s.loadWatermark(ctx)
	if err != nil || day.IsZero() {
		return err
	}
	for _, id := range s.cfg.TrackStocks {
		ss := series[id]
		if ss == nil {
			return fmt.Errorf("missing series %s", id)
		}
		if _, ok := ss.DateIndex[day.Format(dateLayout)]; !ok && !ss.Suspended(day) {
			return nil
		}
	}
	if err = s.writeEquitySnapshot(ctx, day, series); err != nil {
		return err
	}
	holding := s.engine.HoldingValueAsOf(series, day)
	metrics.SetPortfolioSnapshot(s.engine.Cash(), holding, s.engine.Cash()+holding, s.engine.CostBasis())
	metrics.SetLastProcessedDate(day)
	return nil
}

// A missing bar must stop the complete portfolio day, not silently skip a stock.
func (s *TradingService) validateDay(ctx context.Context, day, prev time.Time, series map[string]*trading.StockSeries) error {
	if s.calendar != nil && !prev.IsZero() {
		for d := prev.AddDate(0, 0, 1); d.Before(day); d = d.AddDate(0, 0, 1) {
			open, err := s.calendar.IsTradingDay(ctx, d)
			if err != nil {
				return err
			}
			if open {
				for _, id := range s.cfg.TrackStocks {
					if series[id] == nil || !series[id].Suspended(d) {
						return fmt.Errorf("unprocessed trading day %s before %s", d.Format(dateLayout), day.Format(dateLayout))
					}
				}
			}
		}
	}
	for _, id := range s.cfg.TrackStocks {
		ss := series[id]
		if ss == nil {
			return fmt.Errorf("missing series for %s", id)
		}
		if ss.Suspended(day) {
			continue
		}
		i, ok := ss.DateIndex[day.Format(dateLayout)]
		if !ok || ss.ClosePrices[i] <= 0 {
			return fmt.Errorf("missing daily bar %s %s", id, day.Format(dateLayout))
		}
		if s.cfg.DecisionPriceBasis == "open" && (i >= len(ss.OpenPrices) || ss.OpenPrices[i] <= 0) {
			return fmt.Errorf("missing open %s %s", id, day.Format(dateLayout))
		}
		if i > 0 && ss.ClosePrices[i-1] > 0 {
			ratio := ss.OpenAt(i) / ss.ClosePrices[i-1]
			if ratio < 0.5 || ratio > 2 {
				return fmt.Errorf("inconsistent normalized bar %s %s; retry official market data", id, day.Format(dateLayout))
			}
		}
	}
	return nil
}

func (s *TradingService) reloadIfNeeded(ctx context.Context, series map[string]*trading.StockSeries) error {
	if s.seeded {
		wm, err := s.loadWatermark(ctx)
		if err != nil {
			return err
		}
		if !wm.Equal(s.engineDate) {
			s.needsReload = true
		}
	}
	if !s.needsReload {
		return nil
	}
	if err := s.SeedFromDB(ctx, series); err != nil {
		return err
	}
	s.needsReload = false
	return nil
}
