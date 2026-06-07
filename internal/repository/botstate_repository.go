// internal/repository/botstate_repository.go 存取 BotState key/value 狀態。
package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// BotStateRepository 存取通用的 key/value BotState 資料表,用於持久化上線引擎的水位線與現金餘額。
// 值的格式化與解析由服務層負責,本層僅做字串的讀寫。
type BotStateRepository struct {
	db *sql.DB
}

// NewBotStateRepository 以傳入的連線池建立 BotStateRepository。
func NewBotStateRepository(db *sql.DB) *BotStateRepository {
	return &BotStateRepository{db: db}
}

// Get 回傳 key 對應的 state_value。當資料列不存在時 ok=false,呼叫端可自行套用預設值。
func (r *BotStateRepository) Get(ctx context.Context, key string) (string, bool, error) {
	const query = "SELECT state_value FROM BotState WHERE state_key = ?;"
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		// 查無資料視為正常情況,回傳 ok=false 而非錯誤。
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query bot state %q: %w", key, err)
	}
	return value, true, nil
}

// Set 以 upsert 方式寫入 (key, value),若 key 已存在則覆蓋舊值。
func (r *BotStateRepository) Set(ctx context.Context, key, value string) error {
	const query = "INSERT INTO BotState (state_key, state_value) VALUES (?, ?) ON DUPLICATE KEY UPDATE state_value = VALUES(state_value);"
	if _, err := r.db.ExecContext(ctx, query, key, value); err != nil {
		return fmt.Errorf("upsert bot state %q: %w", key, err)
	}
	return nil
}
