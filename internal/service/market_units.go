package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
)

type SplitFetcher interface {
	FetchSplits(context.Context) ([]marketunits.Action, error)
}

// MarketUnits publishes an immutable book only after durable persistence.
// Refreshes/restarts never touch cash, lots, peaks or the trading watermark.
type MarketUnits struct {
	mu          sync.RWMutex
	refreshMu   sync.Mutex
	book        marketunits.Book
	state       StateStore
	fetcher     SplitFetcher
	lastRefresh time.Time
}

const unitsStateKey = "market_units_v1"

func NewMarketUnits(ctx context.Context, state StateStore, fetcher SplitFetcher) (*MarketUnits, error) {
	m := &MarketUnits{book: marketunits.Default(), state: state, fetcher: fetcher}
	raw, ok, err := state.Get(ctx, unitsStateKey)
	if err != nil {
		return nil, err
	}
	if ok {
		if err := json.Unmarshal([]byte(raw), &m.book); err != nil {
			return nil, err
		}
	}
	if err := m.book.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *MarketUnits) UnitBook() marketunits.Book {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return marketunits.Book{Basis: m.book.Basis, Actions: append([]marketunits.Action(nil), m.book.Actions...)}
}

func (m *MarketUnits) RefreshUnits(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if time.Since(m.lastRefresh) < time.Hour {
		return nil
	}
	if m.fetcher == nil {
		return nil
	} // read-only offline evaluator
	actions, err := m.fetcher.FetchSplits(ctx)
	if err != nil {
		return fmt.Errorf("refresh official split data: %w", err)
	}
	book, err := m.UnitBook().Merge(actions)
	if err != nil {
		return err
	}
	b, err := json.Marshal(book)
	if err != nil {
		return err
	}
	if err := m.state.Set(ctx, unitsStateKey, string(b)); err != nil {
		return err
	}
	m.mu.Lock()
	m.book = book
	m.mu.Unlock()
	m.lastRefresh = time.Now()
	return nil
}

type unitBookProvider interface{ UnitBook() marketunits.Book }

func bookFor(source any) marketunits.Book {
	if p, ok := source.(unitBookProvider); ok {
		return p.UnitBook()
	}
	// Fakes without a market adapter already supply accounting-unit prices.
	return marketunits.Book{Basis: marketunits.BasisDate}
}

func displayFactor(source any, id, date string) float64 {
	return bookFor(source).DisplayFactor(id, date)
}
