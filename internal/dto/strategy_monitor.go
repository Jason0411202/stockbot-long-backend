// internal/dto/strategy_monitor.go 定義原始與歷史重建決策的監控快照。
package dto

import "encoding/json"

// StrategyMonitor 保存整日決策圖表，並明確標示原始或歷史重建來源。
type StrategyMonitor struct {
	Source             string                 `json:"source"`
	StrategyVersion    string                 `json:"strategy_version"`
	Warnings           []string               `json:"warnings"`
	ConfigHash         string                 `json:"config_hash,omitempty"`
	StrategyConfig     json.RawMessage        `json:"strategy_config,omitempty"`
	UnitBook           json.RawMessage        `json:"unit_book,omitempty"`
	SchemaVersion      int                    `json:"schema_version"`
	Availability       string                 `json:"availability"`
	Message            string                 `json:"message"`
	DecisionDate       string                 `json:"decision_date"`
	RecordedAt         string                 `json:"recorded_at"`
	DecisionPriceBasis string                 `json:"decision_price_basis"`
	ExecutionMode      string                 `json:"execution_mode"`
	IndicatorRule      string                 `json:"indicator_rule"`
	Stocks             []StrategyMonitorStock `json:"stocks"`
}

// StrategyMonitorStock 保留單檔決策價格、市況、交易前後持倉與買賣條件。
type StrategyMonitorStock struct {
	StockID       string           `json:"stock_id"`
	Status        string           `json:"status"`
	DecisionPrice *float64         `json:"decision_price"`
	IndicatorDate string           `json:"indicator_date"`
	Regime        string           `json:"regime"`
	RegimeMethod  string           `json:"regime_method"`
	RegimeWindow  int              `json:"regime_window"`
	RegimeValue   *float64         `json:"regime_value"`
	RegimeBase    *float64         `json:"regime_base"`
	Buy           StrategyDecision `json:"buy"`
	Sell          StrategyDecision `json:"sell"`
}

// StrategyDecision 的條件在該買入或賣出階段執行前凍結，成交結果則保留執行後數值。
type StrategyDecision struct {
	Status           string              `json:"status"`
	Reasons          []string            `json:"reasons"`
	Conditions       []StrategyCondition `json:"conditions"`
	Metrics          []StrategyMetric    `json:"metrics"`
	IntendedShares   float64             `json:"intended_shares"`
	ExecutedShares   float64             `json:"executed_shares"`
	CashBefore       float64             `json:"cash_before"`
	CashAfter        float64             `json:"cash_after"`
	HeldSharesBefore float64             `json:"held_shares_before"`
	HeldSharesAfter  float64             `json:"held_shares_after"`
}

// StrategyCondition 是獨立條件；百分比使用 0–100，無適用數值以 null 表示。
type StrategyCondition struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Status        string   `json:"status"`
	Value         *float64 `json:"value"`
	Threshold     *float64 `json:"threshold"`
	Operator      string   `json:"operator"`
	Unit          string   `json:"unit"`
	Description   string   `json:"description"`
	RemainingDays *int     `json:"remaining_days,omitempty"`
	ReleaseDate   string   `json:"release_date,omitempty"`
}

// StrategyMetric 補充金額、均線、峰值及加碼倍率；它不代表額外交易門檻。
type StrategyMetric struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Value       *float64 `json:"value"`
	Unit        string   `json:"unit"`
	Description string   `json:"description"`
}
