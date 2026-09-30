package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type transactionKey struct{}
type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func runner(ctx context.Context, db *sql.DB) sqlRunner {
	if tx, ok := ctx.Value(transactionKey{}).(*sql.Tx); ok {
		return tx
	}
	return db
}

// WithTradingTransaction serializes writers and binds all ledger/state/equity
// operations to one SQL transaction. Even the first trading day has a lock row.
func (r *BotStateRepository) WithTradingTransaction(ctx context.Context, fn func(context.Context) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO BotState (state_key, state_value) VALUES ('trading_lock', '1');"); err != nil {
		return err
	}
	var lock string
	if err = tx.QueryRowContext(ctx, "SELECT state_value FROM BotState WHERE state_key = 'trading_lock' FOR UPDATE;").Scan(&lock); err != nil {
		return err
	}
	if err = fn(context.WithValue(ctx, transactionKey{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit trading day (reload persisted state before retry): %w", err)
	}
	return nil
}
