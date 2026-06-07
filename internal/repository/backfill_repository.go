// internal/repository/backfill_repository.go 存取 BackfillStatus 回補完成紀錄。
package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// BackfillRepository 存取 BackfillStatus 資料表,追蹤哪些 (stock_id, month) 已完成歷史資料回補。
type BackfillRepository struct {
	db *sql.DB
}

// NewBackfillRepository 以傳入的連線池建立 BackfillRepository。
func NewBackfillRepository(db *sql.DB) *BackfillRepository {
	return &BackfillRepository{db: db}
}

// CompletedMonths 回傳 stockID 已標記完成的月份集合,鍵為 "YYYY-MM" 格式字串。
func (r *BackfillRepository) CompletedMonths(ctx context.Context, stockID string) (map[string]bool, error) {
	const query = "SELECT month FROM BackfillStatus WHERE stock_id = ?;"
	rows, err := r.db.QueryContext(ctx, query, stockID)
	if err != nil {
		return nil, fmt.Errorf("query completed months for %s: %w", stockID, err)
	}
	defer rows.Close()

	// 逐列掃描月份字串並收集至 map。
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

// MarkComplete 將 (stockID, month) 標記為已回補完成,使用 INSERT IGNORE 確保重複呼叫為 no-op。
func (r *BackfillRepository) MarkComplete(ctx context.Context, stockID, month string) error {
	const query = "INSERT IGNORE INTO BackfillStatus (stock_id, month, completed_at) VALUES (?, ?, NOW());"
	if _, err := r.db.ExecContext(ctx, query, stockID, month); err != nil {
		return fmt.Errorf("mark backfill complete for %s %s: %w", stockID, month, err)
	}
	return nil
}
