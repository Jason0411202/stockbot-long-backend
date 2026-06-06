// Package repository is the data-access layer: SQL only, no business logic.
//
// Each repository holds an injected *sql.DB (an already-scoped pool whose DSN
// targets the StockLongData schema, so no per-call USE is needed) and takes a
// context.Context on every method. Errors are wrapped with fmt.Errorf so the
// caller (service layer) can add domain context.
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// StockHistoryRepository exposes read/write access to the StockHistory table.
type StockHistoryRepository struct {
	db *sql.DB
}

// NewStockHistoryRepository wires a StockHistoryRepository to a connection pool.
func NewStockHistoryRepository(db *sql.DB) *StockHistoryRepository {
	return &StockHistoryRepository{db: db}
}

// allowedPriceColumns whitelists the price columns that GetPriceAsOf may read.
// priceType is interpolated into the SQL text (column names cannot be bound as
// parameters), so it MUST be validated against this fixed set to prevent SQL
// injection.
var allowedPriceColumns = map[string]struct{}{
	"close_price": {},
	"open_price":  {},
	"high_price":  {},
	"low_price":   {},
}

// GetStockName returns the most recent stock_name recorded for stockID.
func (r *StockHistoryRepository) GetStockName(ctx context.Context, stockID string) (string, error) {
	const query = "SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1;"
	var stockName string
	err := r.db.QueryRowContext(ctx, query, stockID).Scan(&stockName)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("query stock name for %s: %w", stockID, err)
	}
	return stockName, nil
}

// GetPriceAsOf returns the latest priceType value on or before asOf for stockID.
// priceType is whitelisted against allowedPriceColumns; any other value is
// rejected with an error before any query is issued.
func (r *StockHistoryRepository) GetPriceAsOf(ctx context.Context, stockID, asOf, priceType string) (float64, error) {
	if _, ok := allowedPriceColumns[priceType]; !ok {
		return 0, fmt.Errorf("invalid price type %q", priceType)
	}

	// priceType is now a validated column name, safe to interpolate.
	query := "SELECT " + priceType + " FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT 1;"
	var price float64
	err := r.db.QueryRowContext(ctx, query, stockID, asOf).Scan(&price)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("query %s for %s as of %s: %w", priceType, stockID, asOf, err)
	}
	return price, nil
}

// GetClosePricesDescAsOf returns every close_price on or before asOf for
// stockID, newest first. The caller computes day-distance indicators from the
// returned slice.
func (r *StockHistoryRepository) GetClosePricesDescAsOf(ctx context.Context, stockID, asOf string) ([]float64, error) {
	const query = "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;"
	rows, err := r.db.QueryContext(ctx, query, stockID, asOf)
	if err != nil {
		return nil, fmt.Errorf("query close prices for %s as of %s: %w", stockID, asOf, err)
	}
	defer rows.Close()

	prices := make([]float64, 0)
	for rows.Next() {
		var price float64
		if err := rows.Scan(&price); err != nil {
			return nil, fmt.Errorf("scan close price: %w", err)
		}
		prices = append(prices, price)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate close prices: %w", err)
	}
	return prices, nil
}

// GetCloseHistoryAsc returns the (date, close_price) series for stockID in
// ascending date order. Only Date and ClosePrice are populated on each entity.
func (r *StockHistoryRepository) GetCloseHistoryAsc(ctx context.Context, stockID string) ([]entity.StockHistory, error) {
	const query = "SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;"
	rows, err := r.db.QueryContext(ctx, query, stockID)
	if err != nil {
		return nil, fmt.Errorf("query close history for %s: %w", stockID, err)
	}
	defer rows.Close()

	history := make([]entity.StockHistory, 0)
	for rows.Next() {
		var h entity.StockHistory
		if err := rows.Scan(&h.Date, &h.ClosePrice); err != nil {
			return nil, fmt.Errorf("scan close history row: %w", err)
		}
		history = append(history, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate close history: %w", err)
	}
	return history, nil
}

// LoadSeries returns the ascending (date, close_price) series for each stockID,
// keyed by stockID. Only Date and ClosePrice are populated. It replaces the DB
// read previously embedded in engine.loadStockSeries.
func (r *StockHistoryRepository) LoadSeries(ctx context.Context, stockIDs []string) (map[string][]entity.StockHistory, error) {
	series := make(map[string][]entity.StockHistory, len(stockIDs))
	for _, stockID := range stockIDs {
		history, err := r.GetCloseHistoryAsc(ctx, stockID)
		if err != nil {
			return nil, fmt.Errorf("load series for %s: %w", stockID, err)
		}
		series[stockID] = history
	}
	return series, nil
}

// InsertBarIgnore inserts one OHLCV bar into StockHistory with INSERT IGNORE so
// re-fetching an already-stored day is a harmless no-op.
//
// The nullable, app-unread columns value/price_change/transactions are
// intentionally omitted (left NULL).
func (r *StockHistoryRepository) InsertBarIgnore(ctx context.Context, stockID, stockName string, b entity.Bar) error {
	const query = `INSERT IGNORE INTO StockHistory (stock_id, stock_name, date, volume, open_price, high_price, low_price, close_price) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
	if _, err := r.db.ExecContext(ctx, query, stockID, stockName, b.Date, b.Volume, b.Open, b.High, b.Low, b.Close); err != nil {
		return fmt.Errorf("insert bar for %s on %s: %w", stockID, b.Date, err)
	}
	return nil
}
