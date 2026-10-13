// internal/dto/strategy_history.go 定義決策歷史日期索引與自動回補進度。
package dto

// StrategyHistoryEntry 提供單日決策的精簡來源及可用狀態。
type StrategyHistoryEntry struct {
	DecisionDate    string `json:"decision_date"`
	Source          string `json:"source"`
	ExecutionMode   string `json:"execution_mode"`
	RecordedAt      string `json:"recorded_at"`
	StrategyVersion string `json:"strategy_version"`
	Availability    string `json:"availability"`
}

// StrategyBackfillStatus 說明背景回補進度及目前資料覆蓋限制。
type StrategyBackfillStatus struct {
	Status    string `json:"status"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Message   string `json:"message"`
}

// StrategyMonitorHistory 提供由新到舊的完整日期索引及自動回補狀態。
type StrategyMonitorHistory struct {
	Entries    []StrategyHistoryEntry `json:"entries"`
	OldestDate string                 `json:"oldest_date"`
	NewestDate string                 `json:"newest_date"`
	Backfill   StrategyBackfillStatus `json:"backfill"`
}
