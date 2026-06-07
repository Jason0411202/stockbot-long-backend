// internal/repository/stockhistory_repository.go 存取 StockHistory OHLCV 歷史資料表。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// StockHistoryRepository 存取 StockHistory 資料表的讀寫操作。
type StockHistoryRepository struct {
	db *sql.DB
}

// NewStockHistoryRepository 以傳入的連線池建立 StockHistoryRepository。
func NewStockHistoryRepository(db *sql.DB) *StockHistoryRepository {
	return &StockHistoryRepository{db: db}
}

// allowedPriceColumns 列出 GetPriceAsOf 允許查詢的價格欄位白名單。
// priceType 會以字串插值方式拼入 SQL (欄位名稱不能用 ? 佔位),故必須對照此集合驗證以防止 SQL 注入。
var allowedPriceColumns = map[string]struct{}{
	"close_price": {},
	"open_price":  {},
	"high_price":  {},
	"low_price":   {},
}

// GetStockName 回傳 stockID 最近一筆紀錄的 stock_name。
func (r *StockHistoryRepository) GetStockName(ctx context.Context, stockID string) (string, error) {
	const query = "SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1;"
	var stockName string
	err := r.db.QueryRowContext(ctx, query, stockID).Scan(&stockName)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("query stock name for %s: %w", stockID, err)
	}
	return stockName, nil
}

// GetPriceAsOf 回傳 stockID 在 asOf 日期以前最新一筆的 priceType 價格。
// priceType 須通過 allowedPriceColumns 白名單驗證,不合法時直接回傳錯誤,不發出查詢。
func (r *StockHistoryRepository) GetPriceAsOf(ctx context.Context, stockID, asOf, priceType string) (float64, error) {
	if _, ok := allowedPriceColumns[priceType]; !ok {
		return 0, fmt.Errorf("invalid price type %q", priceType)
	}

	// priceType 已通過白名單驗證,安全地插值為欄位名稱。
	query := "SELECT " + priceType + " FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT 1;"
	var price float64
	err := r.db.QueryRowContext(ctx, query, stockID, asOf).Scan(&price)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("query %s for %s as of %s: %w", priceType, stockID, asOf, err)
	}
	return price, nil
}

// GetClosePricesDescAsOf 回傳 stockID 在 asOf 日期以前所有收盤價,依日期降冪排列。
// 呼叫端依此序列計算移動平均等日距指標。
func (r *StockHistoryRepository) GetClosePricesDescAsOf(ctx context.Context, stockID, asOf string) ([]float64, error) {
	const query = "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;"
	rows, err := r.db.QueryContext(ctx, query, stockID, asOf)
	if err != nil {
		return nil, fmt.Errorf("query close prices for %s as of %s: %w", stockID, asOf, err)
	}
	defer rows.Close()

	// 逐列掃描收盤價並收集至切片。
	prices := make([]float64, 0)
	for rows.Next() {
		var price float64
		if err := rows.Scan(&price); err != nil {
			return nil, fmt.Errorf("scan close price: %w", err)
		}
		prices = append(prices, price)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate close prices: %w", err)
	}
	return prices, nil
}

// GetCloseHistoryAsc 回傳 stockID 依日期升冪排列的 (date, close_price) 序列。
// 每筆 entity 僅填入 Date 與 ClosePrice 兩個欄位。
func (r *StockHistoryRepository) GetCloseHistoryAsc(ctx context.Context, stockID string) ([]entity.StockHistory, error) {
	const query = "SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;"
	rows, err := r.db.QueryContext(ctx, query, stockID)
	if err != nil {
		return nil, fmt.Errorf("query close history for %s: %w", stockID, err)
	}
	defer rows.Close()

	// 逐列掃描並收集歷史資料。
	history := make([]entity.StockHistory, 0)
	for rows.Next() {
		var h entity.StockHistory
		if err := rows.Scan(&h.Date, &h.ClosePrice); err != nil {
			return nil, fmt.Errorf("scan close history row: %w", err)
		}
		history = append(history, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate close history: %w", err)
	}
	return history, nil
}

// LoadSeries 批次載入多檔股票的升冪收盤價序列,以 stockID 為鍵回傳 map。
// 每筆 entity 僅填入 Date 與 ClosePrice 兩個欄位。
func (r *StockHistoryRepository) LoadSeries(ctx context.Context, stockIDs []string) (map[string][]entity.StockHistory, error) {
	series := make(map[string][]entity.StockHistory, len(stockIDs))
	// 逐一查詢各股歷史資料並累積至 map。
	for _, stockID := range stockIDs {
		history, err := r.GetCloseHistoryAsc(ctx, stockID)
		if err != nil {
			return nil, fmt.Errorf("load series for %s: %w", stockID, err)
		}
		series[stockID] = history
	}
	return series, nil
}

// InsertBarIgnore 以 INSERT IGNORE 寫入單筆 OHLCV 資料,若該日已存在則靜默略過。
// value/price_change/transactions 等欄位為選用,本方法不寫入,保持 NULL。
func (r *StockHistoryRepository) InsertBarIgnore(ctx context.Context, stockID, stockName string, b entity.Bar) error {
	const query = `INSERT IGNORE INTO StockHistory (stock_id, stock_name, date, volume, open_price, high_price, low_price, close_price) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
	if _, err := r.db.ExecContext(ctx, query, stockID, stockName, b.Date, b.Volume, b.Open, b.High, b.Low, b.Close); err != nil {
		return fmt.Errorf("insert bar for %s on %s: %w", stockID, b.Date, err)
	}
	return nil
}
