// internal/entity/state.go 定義服務跨重啟狀態與資料回補狀態模型。
package entity

import "time"

// BotState 對應 BotState table,PK (state_key)。泛用 key/value,持久化上線引擎狀態
// (last_processed_date watermark、current_cash)。
type BotState struct {
	StateKey   string // state_key   VARCHAR(64) NOT NULL
	StateValue string // state_value VARCHAR(256) NOT NULL
}

// BackfillStatus 對應 BackfillStatus table,PK (stock_id, month)。
// 標記某股票某月份 ("YYYY-MM") 的歷史資料已完整回補。
type BackfillStatus struct {
	StockID     string    // stock_id     VARCHAR(10) NOT NULL
	Month       string    // month        VARCHAR(7) NOT NULL ("YYYY-MM")
	CompletedAt time.Time // completed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
}
