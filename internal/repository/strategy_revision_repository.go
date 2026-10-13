// internal/repository/strategy_revision_repository.go 追加不可變修訂並保留初版歷史紀錄。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// InsertRevision 只新增指定日期與版號的修訂，重複呼叫保持原修訂不變。
func (r *StrategyHistoryRepository) InsertRevision(ctx context.Context, v entity.StrategyHistoryRecord) error {
	const query = "INSERT INTO StrategyMonitorHistoryRevisions (decision_date, revision, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE decision_date = decision_date;"
	if _, err := runner(ctx, r.db).ExecContext(ctx, query, v.DecisionDate, v.Revision, v.Source, v.ExecutionMode, v.RecordedAt, v.StrategyVersion, v.Availability, v.Snapshot); err != nil {
		return fmt.Errorf("insert strategy revision %s %s: %w", v.DecisionDate, v.Revision, err)
	}
	return nil
}

// GetRevision 讀取指定日期及版號，不查詢或修改初版資料表。
func (r *StrategyHistoryRepository) GetRevision(ctx context.Context, date, revision string) (entity.StrategyHistoryRecord, bool, error) {
	const query = "SELECT decision_date, revision, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json FROM StrategyMonitorHistoryRevisions WHERE decision_date = ? AND revision = ?;"
	var v entity.StrategyHistoryRecord
	err := runner(ctx, r.db).QueryRowContext(ctx, query, date, revision).Scan(&v.DecisionDate, &v.Revision, &v.Source, &v.ExecutionMode, &v.RecordedAt, &v.StrategyVersion, &v.Availability, &v.Snapshot)
	if err == sql.ErrNoRows {
		return v, false, nil
	}
	if err != nil {
		return v, false, fmt.Errorf("read strategy revision: %w", err)
	}
	return v, true, nil
}

// ListRevisions 只讀取修訂摘要，HTTP 日期索引不載入快照本文。
func (r *StrategyHistoryRepository) ListRevisions(ctx context.Context, revision string) ([]entity.StrategyHistoryRecord, error) {
	const query = "SELECT decision_date, revision, source, execution_mode, recorded_at, strategy_version, availability FROM StrategyMonitorHistoryRevisions WHERE revision = ? ORDER BY decision_date DESC;"
	rows, err := runner(ctx, r.db).QueryContext(ctx, query, revision)
	if err != nil {
		return nil, fmt.Errorf("list strategy revisions: %w", err)
	}
	defer rows.Close()
	// 修訂列表與初版摘要由服務層依日期合併。
	out := []entity.StrategyHistoryRecord{}
	for rows.Next() {
		var v entity.StrategyHistoryRecord
		if err := rows.Scan(&v.DecisionDate, &v.Revision, &v.Source, &v.ExecutionMode, &v.RecordedAt, &v.StrategyVersion, &v.Availability); err != nil {
			return nil, fmt.Errorf("scan strategy revision: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate strategy revisions: %w", err)
	}
	return out, nil
}
