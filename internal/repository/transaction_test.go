package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

func TestTradingTransaction_RollsBackLedgerWhenCashWriteFails(t *testing.T) {
	db, mock := newMock(t)
	state := NewBotStateRepository(db)
	ledger := NewLedgerRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO BotState").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT state_value FROM BotState.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("1"))
	mock.ExpectExec("INSERT INTO UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO BotState").WillReturnError(errors.New("disk failure"))
	mock.ExpectRollback()
	err := state.WithTradingTransaction(context.Background(), func(ctx context.Context) error {
		if _, ok := runner(ctx, db).(interface{ Commit() error }); !ok {
			t.Fatal("repository escaped transaction")
		}
		if err := ledger.InsertUnrealized(ctx, entity.UnrealizedGainsLoss{TransactionDate: "2026-09-30", StockID: "00631L", StockName: "ETF", Shares: 10, TransactionPrice: 39.1, InvestmentCost: 391}); err != nil {
			return err
		}
		return state.Set(ctx, "current_cash", "100")
	})
	if err == nil {
		t.Fatal("failed transaction reported success")
	}
	assertMet(t, mock)
}
