// internal/service/statistic_service.go 組裝追蹤股票的統計資料 API 回應。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// noPointSentinel 是「從未出現過更低／更高點」時的天數哨兵值，與 sqls.go 的 LowerPointDays/UpperPointDays 一致。
const noPointSentinel = 36500

// StatisticService 匯整每檔追蹤股票的顯示資料：名稱、今日收盤價，以及距離最近低點／高點的天數指標。
// 它不持有任何 SQL，所有資料存取皆透過 StockStore port 完成。
type StatisticService struct {
	stock StockStore
	cfg   *config.Config
	log   *logrus.Logger
}

// NewStatisticService 建立並回傳一個已完成依賴注入的 StatisticService。
func NewStatisticService(stock StockStore, cfg *config.Config, log *logrus.Logger) *StatisticService {
	return &StatisticService{stock: stock, cfg: cfg, log: log}
}

// lowerPointDays 回傳從今日往前數，第一個收盤價嚴格低於今日收盤的天數距離。
// prices 為由新到舊排列（prices[0] 為今日）；序列為空時回傳 0；
// 沒有任何更低點時回傳哨兵值 36500。
func lowerPointDays(prices []float64) int {
	if len(prices) == 0 {
		return 0
	}
	todayPrice := prices[0]
	for i, price := range prices {
		if price < todayPrice {
			return i
		}
	}
	return noPointSentinel
}

// upperPointDays 回傳從今日往前數，第一個收盤價嚴格高於今日收盤的天數距離。
// prices 為由新到舊排列（prices[0] 為今日）；序列為空時回傳 0；
// 沒有任何更高點時回傳哨兵值 36500。
func upperPointDays(prices []float64) int {
	if len(prices) == 0 {
		return 0
	}
	todayPrice := prices[0]
	for i, price := range prices {
		if price > todayPrice {
			return i
		}
	}
	return noPointSentinel
}

// StockStatisticData 回傳每檔追蹤股票的統計資料 DTO 清單。
// 任一 repository 呼叫發生錯誤時直接回傳錯誤（由 controller 決定如何處理）。
func (s *StatisticService) StockStatisticData(ctx context.Context) ([]dto.StockStatistic, error) {
	today := time.Now().Format("2006-01-02")

	out := make([]dto.StockStatistic, 0, len(s.cfg.TrackStocks))
	for _, stockID := range s.cfg.TrackStocks {
		// 依序查詢股票名稱、今日收盤價與由新到舊的收盤價序列。
		name, err := s.stock.GetStockName(ctx, stockID)
		if err != nil {
			return nil, fmt.Errorf("get stock name for %s: %w", stockID, err)
		}

		todayPrice, err := s.stock.GetPriceAsOf(ctx, stockID, today, "close_price")
		if err != nil {
			return nil, fmt.Errorf("get close price for %s: %w", stockID, err)
		}

		prices, err := s.stock.GetClosePricesDescAsOf(ctx, stockID, today)
		if err != nil {
			return nil, fmt.Errorf("get close prices for %s: %w", stockID, err)
		}

		// 計算低點／高點天數距離並組裝 DTO。
		out = append(out, dto.StockStatistic{
			StockID:        stockID,
			StockName:      name,
			TodayPrice:     todayPrice,
			LowerPointDays: lowerPointDays(prices),
			UpperPointDays: upperPointDays(prices),
		})
	}
	return out, nil
}
