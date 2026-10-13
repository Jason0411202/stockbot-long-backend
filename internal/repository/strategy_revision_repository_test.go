// internal/repository/strategy_revision_repository_test.go 驗證修訂只追加到獨立表並提供版次索引。
package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// TestStrategyRevisionRepository_AppendOnlyAndRead 驗證不更新初版表並永久取回指定修訂。
func TestStrategyRevisionRepository_AppendOnlyAndRead(t *testing.T) {
	db, mock := newMock(t)
	repo := NewStrategyHistoryRepository(db)
	ctx := context.Background()
	v := entity.StrategyHistoryRecord{DecisionDate: "2023-08-14", Revision: "ledger-cashflow-v2", Source: "reconstructed", ExecutionMode: "catchup", RecordedAt: "now", StrategyVersion: "fixed:hash", Availability: "available", Snapshot: `{"revision":"ledger-cashflow-v2"}`}
	insert := "INSERT INTO StrategyMonitorHistoryRevisions (decision_date, revision, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE decision_date = decision_date;"
	mock.ExpectExec(regexp.QuoteMeta(insert)).WithArgs(v.DecisionDate, v.Revision, v.Source, v.ExecutionMode, v.RecordedAt, v.StrategyVersion, v.Availability, v.Snapshot).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.InsertRevision(ctx, v); err != nil {
		t.Fatal(err)
	}
	query := "SELECT decision_date, revision, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json FROM StrategyMonitorHistoryRevisions WHERE decision_date = ? AND revision = ?;"
	cols := []string{"decision_date", "revision", "source", "execution_mode", "recorded_at", "strategy_version", "availability", "snapshot_json"}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(v.DecisionDate, v.Revision).WillReturnRows(sqlmock.NewRows(cols).AddRow(v.DecisionDate, v.Revision, v.Source, v.ExecutionMode, v.RecordedAt, v.StrategyVersion, v.Availability, v.Snapshot))
	got, found, err := repo.GetRevision(ctx, v.DecisionDate, v.Revision)
	if err != nil || !found || got != v {
		t.Fatalf("revision=%+v found=%v err=%v", got, found, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("2023-08-15", v.Revision).WillReturnRows(sqlmock.NewRows(cols))
	if _, found, err := repo.GetRevision(ctx, "2023-08-15", v.Revision); err != nil || found {
		t.Fatal("missing revision accepted")
	}
	assertMet(t, mock)
}

// TestStrategyRevisionRepository_IndexOmitsPayload 驗證修訂索引只取摘要欄位。
func TestStrategyRevisionRepository_IndexOmitsPayload(t *testing.T) {
	db, mock := newMock(t)
	repo := NewStrategyHistoryRepository(db)
	query := "SELECT decision_date, revision, source, execution_mode, recorded_at, strategy_version, availability FROM StrategyMonitorHistoryRevisions WHERE revision = ? ORDER BY decision_date DESC;"
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("ledger-cashflow-v2").WillReturnRows(sqlmock.NewRows([]string{"decision_date", "revision", "source", "execution_mode", "recorded_at", "strategy_version", "availability"}).AddRow("2023-08-14", "ledger-cashflow-v2", "reconstructed", "catchup", "now", "fixed:hash", "available"))
	rows, err := repo.ListRevisions(context.Background(), "ledger-cashflow-v2")
	if err != nil || len(rows) != 1 || rows[0].Snapshot != "" {
		t.Fatalf("index=%+v err=%v", rows, err)
	}
	assertMet(t, mock)
}
