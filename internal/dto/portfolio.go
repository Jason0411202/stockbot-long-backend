// Package dto holds the typed shapes returned over the HTTP API. They replace
// the previous []map[string]interface{} responses. JSON tags carry the exact
// wire keys the frontend already depends on — they must not change.
package dto

// UnrealizedGainLoss 是 GET /api/get_unrealized_gains_losses 的單筆回應。
// 前 6 欄來自 DB;後 4 欄為依即時收盤價計算的衍生欄位 (computed)。
// 注意:todayClosePrice 是唯一的 camelCase key,必須保持原樣。
type UnrealizedGainLoss struct {
	TransactionDate   string  `json:"transaction_date"`
	StockID           string  `json:"stock_id"`
	StockName         string  `json:"stock_name"`
	TransactionPrice  float64 `json:"transaction_price"`
	InvestmentCost    float64 `json:"investment_cost"`
	Shares            int     `json:"shares"`
	TodayClosePrice   float64 `json:"todayClosePrice"`     // computed; 0 on price-lookup error
	NowValue          float64 `json:"now_value"`           // computed, rounded 2dp
	PredictProfitLoss float64 `json:"predict_profit_loss"` // computed, rounded 2dp
	PredictProfitRate float64 `json:"predict_profit_rate"` // computed (%), rounded 2dp
}

// RealizedGainLoss 是 GET /api/get_realized_gains_losses 的單筆回應。
// 全部欄位來自 DB;revenue/profit_loss/profit_rate 僅在輸出時 round 2dp。
type RealizedGainLoss struct {
	BuyDate        string  `json:"buy_date"`
	SellDate       string  `json:"sell_date"`
	StockID        string  `json:"stock_id"`
	StockName      string  `json:"stock_name"`
	PurchasePrice  float64 `json:"purchase_price"`
	SellPrice      float64 `json:"sell_price"`
	InvestmentCost float64 `json:"investment_cost"`
	Revenue        float64 `json:"revenue"`
	ProfitLoss     float64 `json:"profit_loss"`
	ProfitRate     float64 `json:"profit_rate"`
	Shares         int     `json:"shares"`
}
