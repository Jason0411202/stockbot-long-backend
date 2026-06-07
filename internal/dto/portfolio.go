// internal/dto/portfolio.go 定義投資組合損益 API 的回應 DTO。
package dto

// UnrealizedGainLoss 是 GET /api/get_unrealized_gains_losses 的單筆回應。
// 前 6 欄來自 DB;後 4 欄為依即時收盤價計算的衍生欄位 (computed)。
// 注意:todayClosePrice 是唯一的 camelCase key,必須保持原樣。
type UnrealizedGainLoss struct {
	TransactionDate   string  `json:"transaction_date"`    // 交易日期 (YYYY-MM-DD)
	StockID           string  `json:"stock_id"`            // 股票代號
	StockName         string  `json:"stock_name"`          // 股票名稱
	TransactionPrice  float64 `json:"transaction_price"`   // 買入成交價
	InvestmentCost    float64 `json:"investment_cost"`     // 買入總成本 (= 成交價 × 股數)
	Shares            int     `json:"shares"`              // 持有股數
	TodayClosePrice   float64 `json:"todayClosePrice"`     // 即時收盤價;查詢失敗時為 0
	NowValue          float64 `json:"now_value"`           // 持股現值 (= 今收 × 股數),四捨五入 2 位
	PredictProfitLoss float64 `json:"predict_profit_loss"` // 未實現損益 (= 現值 - 成本),四捨五入 2 位
	PredictProfitRate float64 `json:"predict_profit_rate"` // 未實現損益率 (%),四捨五入 2 位
}

// RealizedGainLoss 是 GET /api/get_realized_gains_losses 的單筆回應。
// 全部欄位來自 DB;revenue/profit_loss/profit_rate 僅在輸出時 round 2dp。
type RealizedGainLoss struct {
	BuyDate        string  `json:"buy_date"`        // 買入日期 (YYYY-MM-DD)
	SellDate       string  `json:"sell_date"`       // 賣出日期 (YYYY-MM-DD)
	StockID        string  `json:"stock_id"`        // 股票代號
	StockName      string  `json:"stock_name"`      // 股票名稱
	PurchasePrice  float64 `json:"purchase_price"`  // 買入成交價
	SellPrice      float64 `json:"sell_price"`      // 賣出成交價
	InvestmentCost float64 `json:"investment_cost"` // 買入總成本
	Revenue        float64 `json:"revenue"`         // 賣出總收入,四捨五入 2 位
	ProfitLoss     float64 `json:"profit_loss"`     // 已實現損益 (= 收入 - 成本),四捨五入 2 位
	ProfitRate     float64 `json:"profit_rate"`     // 已實現損益率 (%),四捨五入 2 位
	Shares         int     `json:"shares"`          // 賣出股數
}
