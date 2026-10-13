// internal/repository/strategy_history_repository_test.go 驗證歷史不可變寫入、精簡索引與交易隔離。
package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// TestStrategyHistoryRepository_ImmutableWriteAndRead 驗證日期衝突只保留既有內容並可查詢完整資料。
func TestStrategyHistoryRepository_ImmutableWriteAndRead(t *testing.T) {
	db, mock := newMock(t)
	repo := NewStrategyHistoryRepository(db)
	ctx := context.Background()
	record := entity.StrategyHistoryRecord{DecisionDate: "2024-03-01", Source: "original", ExecutionMode: "live", RecordedAt: "2024-03-01T01:10:00Z", StrategyVersion: "sha:config", Availability: "available", Snapshot: `{"schema_version":1}`}
	insert := "INSERT INTO StrategyMonitorHistory (decision_date, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE decision_date = decision_date;"
	mock.ExpectExec(regexp.QuoteMeta(insert)).WithArgs(record.DecisionDate, record.Source, record.ExecutionMode, record.RecordedAt, record.StrategyVersion, record.Availability, record.Snapshot).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.InsertMissing(ctx, record); err != nil {
		t.Fatal(err)
	}
	query := "SELECT decision_date, source, execution_mode, recorded_at, strategy_version, availability, snapshot_json FROM StrategyMonitorHistory WHERE decision_date = ?;"
	cols := []string{"decision_date", "source", "execution_mode", "recorded_at", "strategy_version", "availability", "snapshot_json"}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(record.DecisionDate).WillReturnRows(sqlmock.NewRows(cols).AddRow(record.DecisionDate, record.Source, record.ExecutionMode, record.RecordedAt, record.StrategyVersion, record.Availability, record.Snapshot))
	got, found, err := repo.GetDate(ctx, record.DecisionDate)
	if err != nil || !found || got != record {
		t.Fatalf("read %+v %v %v", got, found, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("2024-03-02").WillReturnRows(sqlmock.NewRows(cols))
	if _, found, err := repo.GetDate(ctx, "2024-03-02"); err != nil || found {
		t.Fatalf("missing found=%v err=%v", found, err)
	}
	assertMet(t, mock)
}

// TestStrategyHistoryRepository_IndexOmitsLargeSnapshots 驗證完整日期索引不讀取快照本文。
func TestStrategyHistoryRepository_IndexOmitsLargeSnapshots(t *testing.T) {
	db, mock := newMock(t)
	repo := NewStrategyHistoryRepository(db)
	query := "SELECT decision_date, source, execution_mode, recorded_at, strategy_version, availability FROM StrategyMonitorHistory ORDER BY decision_date DESC;"
	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(sqlmock.NewRows([]string{"decision_date", "source", "execution_mode", "recorded_at", "strategy_version", "availability"}).AddRow("2024-03-02", "original", "live", "now", "sha", "available").AddRow("2024-03-01", "reconstructed", "catchup", "now", "sha", "unavailable"))
	rows, err := repo.ListHistory(context.Background())
	if err != nil || len(rows) != 2 || rows[0].DecisionDate != "2024-03-02" || rows[0].Snapshot != "" {
		t.Fatalf("index=%+v err=%v", rows, err)
	}
	assertMet(t, mock)
}

// TestStrategyHistoryRepository_FailureRollsBackWholeTradingTransaction 驗證歷史寫入使用帳本交易而非獨立連線。
func TestStrategyHistoryRepository_FailureRollsBackWholeTradingTransaction(t *testing.T) {
	db, mock := newMock(t)
	state := NewBotStateRepository(db)
	history := NewStrategyHistoryRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO BotState").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT state_value FROM BotState.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("1"))
	mock.ExpectExec("INSERT INTO StrategyMonitorHistory").WillReturnError(errors.New("storage unavailable"))
	mock.ExpectRollback()
	err := state.WithTradingTransaction(context.Background(), func(ctx context.Context) error {
		if runner(ctx, db) == sqlRunner(db) {
			t.Fatal("history escaped SQL transaction")
		}
		return history.InsertMissing(ctx, entity.StrategyHistoryRecord{DecisionDate: "2024-03-01"})
	})
	if err == nil {
		t.Fatal("failed history transaction reported success")
	}
	assertMet(t, mock)
}

// TestHistoryReadSnapshot_DoesNotAcquireTradingLock 驗證一致讀取共用交易且不鎖上線交易列。
func TestHistoryReadSnapshot_DoesNotAcquireTradingLock(t *testing.T) {
	db, mock := newMock(t)
	state := NewBotStateRepository(db)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT state_value FROM BotState WHERE state_key = ?;")).WithArgs("last_processed_date").WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("2024-03-01"))
	mock.ExpectCommit()
	err := state.WithReadSnapshot(context.Background(), func(ctx context.Context) error {
		if runner(ctx, db) == sqlRunner(db) {
			t.Fatal("read escaped consistent snapshot")
		}
		_, _, err := state.Get(ctx, "last_processed_date")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMet(t, mock)
}
