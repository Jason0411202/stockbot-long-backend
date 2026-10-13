// internal/repository/strategy_history_repository.go 永久保存決策快照並提供日期索引。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// StrategyHistoryRepository 以同一交易連線保存不可變的每日決策。
type StrategyHistoryRepository struct{ db *sql.DB }

// NewStrategyHistoryRepository 建立歷史紀錄儲存器。
func NewStrategyHistoryRepository(db *sql.DB) *StrategyHistoryRepository {
	return &StrategyHistoryRepository{db: db}
}

// InsertMissing 只新增尚未存在的日期，保留所有既有原始及重建紀錄。
func (r *StrategyHistoryRepository) InsertMissing(ctx context.Context, v entity.StrategyHistoryRecord) error {
	const query = "INSERT INTO StrategyMonitorHistory (decision_date, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE decision_date = decision_date;"
	if _, err := runner(ctx, r.db).ExecContext(ctx, query, v.DecisionDate, v.Source, v.ExecutionMode, v.RecordedAt, v.StrategyVersion, v.Availability, v.Snapshot); err != nil {
		return fmt.Errorf("insert strategy history %s: %w", v.DecisionDate, err)
	}
	return nil
}

// GetDate 讀取指定日期的完整紀錄。
func (r *StrategyHistoryRepository) GetDate(ctx context.Context, date string) (entity.StrategyHistoryRecord, bool, error) {
	const query = "SELECT decision_date, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json FROM StrategyMonitorHistory WHERE decision_date = ?;"
	var v entity.StrategyHistoryRecord
	err := runner(ctx, r.db).QueryRowContext(ctx, query, date).Scan(&v.DecisionDate, &v.Source, &v.ExecutionMode, &v.RecordedAt, &v.StrategyVersion, &v.Availability, &v.Snapshot)
	if err == sql.ErrNoRows {
		return v, false, nil
	}
	if err != nil {
		return v, false, fmt.Errorf("read strategy history %s: %w", date, err)
	}
	return v, true, nil
}

// ListHistory 只查詢摘要欄位，依日期由新到舊提供完整日期索引。
func (r *StrategyHistoryRepository) ListHistory(ctx context.Context) ([]entity.StrategyHistoryRecord, error) {
	const query = "SELECT decision_date, source, execution_mode, recorded_at, strategy_version, availability FROM StrategyMonitorHistory ORDER BY decision_date DESC;"
	rows, err := runner(ctx, r.db).QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list strategy history: %w", err)
	}
	defer rows.Close()
	// 摘要不載入快照本文，歷年日期索引保持精簡。
	out := []entity.StrategyHistoryRecord{}
	for rows.Next() {
		var v entity.StrategyHistoryRecord
		if err := rows.Scan(&v.DecisionDate, &v.Source, &v.ExecutionMode, &v.RecordedAt, &v.StrategyVersion, &v.Availability); err != nil {
			return nil, fmt.Errorf("scan strategy history: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate strategy history: %w", err)
	}
	return out, nil
}
