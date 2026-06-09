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

// EquitySnapshot 對應 EquityHistory table,PK (date)。記錄某交易日收盤後的真實帳戶權益快照
// (現金 + 持股市值),供前端歷史權益折線圖。date 為 "YYYY-MM-DD" 字串 (與引擎水位線同格式)。
// 由線上引擎逐日寫入 (catch-up 回放 + 每日 loop);與回測 equity_curve 不同,此為真實帳本走勢。
type EquitySnapshot struct {
	Date         string  // date          VARCHAR(10) NOT NULL ("YYYY-MM-DD")
	Cash         float64 // cash          DECIMAL(14,2) NOT NULL
	HoldingValue float64 // holding_value DECIMAL(14,2) NOT NULL
	TotalEquity  float64 // total_equity  DECIMAL(14,2) NOT NULL
}
