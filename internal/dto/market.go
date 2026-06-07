// internal/dto/market.go 定義市場資料與歷史價格 API 的回應 DTO。
package dto

// StockStatistic 是 GET /api/get_stock_statistic_data 的單筆回應。
// lower_point_days / upper_point_days 為 domain indicator 計算結果 (computed)。
type StockStatistic struct {
	StockID        string  `json:"stock_id"`         // 股票代號
	StockName      string  `json:"stock_name"`       // 股票名稱
	TodayPrice     float64 `json:"today_price"`      // 今日收盤價
	LowerPointDays int     `json:"lower_point_days"` // 距上一個低點的交易日數 (computed)
	UpperPointDays int     `json:"upper_point_days"` // 距上一個高點的交易日數 (computed)
}

// StockHistoryPoint 是 GET /api/get_stock_history_data 的單筆回應 (收盤價序列,供畫圖)。
type StockHistoryPoint struct {
	Date  string  `json:"date"`  // 交易日期 (YYYY-MM-DD)
	Price float64 `json:"price"` // 該日收盤價
}
