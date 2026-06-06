package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// LedgerRepository exposes read/write access to the UnrealizedGainsLosses and
// RealizedGainsLosses tables. It holds no business logic: cost/share figures
// are computed by the caller and merely persisted here.
type LedgerRepository struct {
	db *sql.DB
}

// NewLedgerRepository wires a LedgerRepository to a connection pool.
func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

const unrealizedColumns = "transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares"

func scanUnrealized(rows *sql.Rows) (entity.UnrealizedGainsLoss, error) {
	var e entity.UnrealizedGainsLoss
	err := rows.Scan(&e.TransactionDate, &e.StockID, &e.StockName, &e.TransactionPrice, &e.InvestmentCost, &e.Shares)
	return e, err
}

// LoadAllUnrealized returns every unrealized lot (all six columns). It is used
// at online-engine startup to restore in-memory positions from the DB.
func (r *LedgerRepository) LoadAllUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error) {
	query := "SELECT " + unrealizedColumns + " FROM UnrealizedGainsLosses;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query unrealized lots: %w", err)
	}
	defer rows.Close()

	out := make([]entity.UnrealizedGainsLoss, 0)
	for rows.Next() {
		e, err := scanUnrealized(rows)
		if err != nil {
			return nil, fmt.Errorf("scan unrealized lot: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unrealized lots: %w", err)
	}
	return out, nil
}

// GetLowestUnrealized returns the cheapest unrealized lot for stockID on or
// before asOf (lowest transaction_price, then oldest transaction_date). The
// bool is false when no matching lot exists.
func (r *LedgerRepository) GetLowestUnrealized(ctx context.Context, stockID, asOf string) (entity.UnrealizedGainsLoss, bool, error) {
	query := "SELECT " + unrealizedColumns + " FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ? ORDER BY transaction_price ASC, transaction_date ASC LIMIT 1;"
	rows, err := r.db.QueryContext(ctx, query, stockID, asOf)
	if err != nil {
		return entity.UnrealizedGainsLoss{}, false, fmt.Errorf("query lowest unrealized lot for %s: %w", stockID, err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return entity.UnrealizedGainsLoss{}, false, fmt.Errorf("query lowest unrealized lot for %s: %w", stockID, err)
		}
		return entity.UnrealizedGainsLoss{}, false, nil
	}
	e, err := scanUnrealized(rows)
	if err != nil {
		return entity.UnrealizedGainsLoss{}, false, fmt.Errorf("scan lowest unrealized lot for %s: %w", stockID, err)
	}
	return e, true, nil
}

// InsertUnrealized writes one unrealized lot. The caller supplies stock_name
// and the already-computed investment_cost (no subquery).
func (r *LedgerRepository) InsertUnrealized(ctx context.Context, e entity.UnrealizedGainsLoss) error {
	const query = "INSERT INTO UnrealizedGainsLosses (transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares) VALUES (?, ?, ?, ?, ?, ?);"
	if _, err := r.db.ExecContext(ctx, query, e.TransactionDate, e.StockID, e.StockName, e.TransactionPrice, e.InvestmentCost, e.Shares); err != nil {
		return fmt.Errorf("insert unrealized lot for %s on %s: %w", e.StockID, e.TransactionDate, err)
	}
	return nil
}

// DeleteUnrealized removes one unrealized lot keyed by (stockID, transactionDate).
func (r *LedgerRepository) DeleteUnrealized(ctx context.Context, stockID, transactionDate string) error {
	const query = "DELETE FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ?;"
	if _, err := r.db.ExecContext(ctx, query, stockID, transactionDate); err != nil {
		return fmt.Errorf("delete unrealized lot for %s on %s: %w", stockID, transactionDate, err)
	}
	return nil
}

// UpdateUnrealized persists a partial-sell result: the recomputed
// investment_cost and shares for the lot keyed by (stockID, transactionDate).
func (r *LedgerRepository) UpdateUnrealized(ctx context.Context, stockID, transactionDate string, investmentCost float64, shares int) error {
	const query = "UPDATE UnrealizedGainsLosses SET investment_cost = ?, shares = ? WHERE stock_id = ? AND transaction_date = ?;"
	if _, err := r.db.ExecContext(ctx, query, investmentCost, shares, stockID, transactionDate); err != nil {
		return fmt.Errorf("update unrealized lot for %s on %s: %w", stockID, transactionDate, err)
	}
	return nil
}

// InsertRealized writes one realized P&L row. All figures are computed by the
// caller (trading service) and merely persisted.
func (r *LedgerRepository) InsertRealized(ctx context.Context, e entity.RealizedGainsLoss) error {
	const query = "INSERT INTO RealizedGainsLosses (buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
	if _, err := r.db.ExecContext(ctx, query, e.BuyDate, e.SellDate, e.StockID, e.StockName, e.PurchasePrice, e.SellPrice, e.InvestmentCost, e.Revenue, e.ProfitLoss, e.ProfitRate, e.Shares); err != nil {
		return fmt.Errorf("insert realized P&L for %s (%s->%s): %w", e.StockID, e.BuyDate, e.SellDate, err)
	}
	return nil
}

// ListUnrealized returns the most recent 500 unrealized lots, newest first, for
// the API. The service layer enriches each row with live P&L.
func (r *LedgerRepository) ListUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error) {
	query := "SELECT " + unrealizedColumns + " FROM UnrealizedGainsLosses ORDER BY transaction_date DESC LIMIT 500;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list unrealized lots: %w", err)
	}
	defer rows.Close()

	out := make([]entity.UnrealizedGainsLoss, 0)
	for rows.Next() {
		e, err := scanUnrealized(rows)
		if err != nil {
			return nil, fmt.Errorf("scan unrealized lot: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unrealized lots: %w", err)
	}
	return out, nil
}

// ListRealized returns the most recent 500 realized P&L rows, newest sell first.
func (r *LedgerRepository) ListRealized(ctx context.Context) ([]entity.RealizedGainsLoss, error) {
	const query = "SELECT buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares FROM RealizedGainsLosses ORDER BY sell_date DESC LIMIT 500;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list realized P&L: %w", err)
	}
	defer rows.Close()

	out := make([]entity.RealizedGainsLoss, 0)
	for rows.Next() {
		var e entity.RealizedGainsLoss
		if err := rows.Scan(&e.BuyDate, &e.SellDate, &e.StockID, &e.StockName, &e.PurchasePrice, &e.SellPrice, &e.InvestmentCost, &e.Revenue, &e.ProfitLoss, &e.ProfitRate, &e.Shares); err != nil {
			return nil, fmt.Errorf("scan realized P&L row: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realized P&L: %w", err)
	}
	return out, nil
}

// LastBuyDateRaw returns the raw MAX buy-date string across UnrealizedGainsLosses
// (transaction_date) and RealizedGainsLosses (buy_date) for stockID. The bool is
// false when the value is NULL or empty (the stock was never bought). The caller
// parses the returned string into a time.Time.
func (r *LedgerRepository) LastBuyDateRaw(ctx context.Context, stockID string) (string, bool, error) {
	const query = `
		SELECT MAX(d) FROM (
			SELECT MAX(transaction_date) AS d FROM UnrealizedGainsLosses WHERE stock_id = ?
			UNION ALL
			SELECT MAX(buy_date) AS d FROM RealizedGainsLosses WHERE stock_id = ?
		) t;`
	var dateStr sql.NullString
	err := r.db.QueryRowContext(ctx, query, stockID, stockID).Scan(&dateStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query last buy date for %s: %w", stockID, err)
	}
	if !dateStr.Valid || dateStr.String == "" {
		return "", false, nil
	}
	return dateStr.String, true, nil
}
