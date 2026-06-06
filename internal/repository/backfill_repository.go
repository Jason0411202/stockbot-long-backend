package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// BackfillRepository exposes the BackfillStatus table, which tracks which
// (stock_id, month) pairs have had their historical data fully backfilled.
type BackfillRepository struct {
	db *sql.DB
}

// NewBackfillRepository wires a BackfillRepository to a connection pool.
func NewBackfillRepository(db *sql.DB) *BackfillRepository {
	return &BackfillRepository{db: db}
}

// CompletedMonths returns the set of months ("YYYY-MM") already marked complete
// for stockID.
func (r *BackfillRepository) CompletedMonths(ctx context.Context, stockID string) (map[string]bool, error) {
	const query = "SELECT month FROM BackfillStatus WHERE stock_id = ?;"
	rows, err := r.db.QueryContext(ctx, query, stockID)
	if err != nil {
		return nil, fmt.Errorf("query completed months for %s: %w", stockID, err)
	}
	defer rows.Close()

	completed := make(map[string]bool)
	for rows.Next() {
		var month string
		if err := rows.Scan(&month); err != nil {
			return nil, fmt.Errorf("scan completed month: %w", err)
		}
		completed[month] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completed months: %w", err)
	}
	return completed, nil
}

// MarkComplete records that (stockID, month) has been fully backfilled. Uses
// INSERT IGNORE so repeated calls are idempotent.
func (r *BackfillRepository) MarkComplete(ctx context.Context, stockID, month string) error {
	const query = "INSERT IGNORE INTO BackfillStatus (stock_id, month, completed_at) VALUES (?, ?, NOW());"
	if _, err := r.db.ExecContext(ctx, query, stockID, month); err != nil {
		return fmt.Errorf("mark backfill complete for %s %s: %w", stockID, month, err)
	}
	return nil
}
