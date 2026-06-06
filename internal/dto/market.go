package dto

// StockStatistic 是 GET /api/get_stock_statistic_data 的單筆回應。
// lower_point_days / upper_point_days 為 domain indicator 計算結果 (computed)。
type StockStatistic struct {
	StockID        string  `json:"stock_id"`
	StockName      string  `json:"stock_name"`
	TodayPrice     float64 `json:"today_price"`
	LowerPointDays int     `json:"lower_point_days"`
	UpperPointDays int     `json:"upper_point_days"`
}

// StockHistoryPoint 是 GET /api/get_stock_history_data 的單筆回應 (收盤價序列,供畫圖)。
type StockHistoryPoint struct {
	Date  string  `json:"date"`
	Price float64 `json:"price"`
}
