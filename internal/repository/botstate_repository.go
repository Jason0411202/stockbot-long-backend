package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// BotStateRepository exposes the generic key/value BotState table used to
// persist the online engine's watermark and cash. Parsing/formatting of the
// stored string value lives in the service layer.
type BotStateRepository struct {
	db *sql.DB
}

// NewBotStateRepository wires a BotStateRepository to a connection pool.
func NewBotStateRepository(db *sql.DB) *BotStateRepository {
	return &BotStateRepository{db: db}
}

// Get returns the state_value for key. The bool is false when no row exists
// (sql.ErrNoRows), so the caller can fall back to a default.
func (r *BotStateRepository) Get(ctx context.Context, key string) (string, bool, error) {
	const query = "SELECT state_value FROM BotState WHERE state_key = ?;"
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query bot state %q: %w", key, err)
	}
	return value, true, nil
}

// Set upserts the (key, value) pair, overwriting any existing value for key.
func (r *BotStateRepository) Set(ctx context.Context, key, value string) error {
	const query = "INSERT INTO BotState (state_key, state_value) VALUES (?, ?) ON DUPLICATE KEY UPDATE state_value = VALUES(state_value);"
	if _, err := r.db.ExecContext(ctx, query, key, value); err != nil {
		return fmt.Errorf("upsert bot state %q: %w", key, err)
	}
	return nil
}
