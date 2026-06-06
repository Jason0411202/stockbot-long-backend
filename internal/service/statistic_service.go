package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// noPointSentinel is the "never crossed" day-count sentinel preserved verbatim
// from sqls.go LowerPointDays/UpperPointDays (return 36500).
const noPointSentinel = 36500

// StatisticService aggregates per-tracked-stock display data: name, today's
// close, and the lower/upper-point day-distance indicators. It reproduces
// GetStockStatisticData (sqls.go 950-981) plus the LowerPointDays (512-551) and
// UpperPointDays (553-592) day-count logic in-memory.
type StatisticService struct {
	stock StockStore
	cfg   *config.Config
	log   *logrus.Logger
}

// NewStatisticService wires a StatisticService to its stock port and config.
func NewStatisticService(stock StockStore, cfg *config.Config, log *logrus.Logger) *StatisticService {
	return &StatisticService{stock: stock, cfg: cfg, log: log}
}

// lowerPointDays returns days back until the first close strictly below today's
// close (prices are newest-first; prices[0] is today). 0 on empty, sentinel
// 36500 when none is lower — matching sqls.go 541-550.
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

// upperPointDays returns days back until the first close strictly above today's
// close. 0 on empty, sentinel 36500 when none is higher — matching sqls.go
// 582-591.
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

// StockStatisticData returns one dto.StockStatistic per tracked stock,
// reproducing GetStockStatisticData. Repository errors are propagated (the
// controller swallows them to a 200 + empty body).
func (s *StatisticService) StockStatisticData(ctx context.Context) ([]dto.StockStatistic, error) {
	today := time.Now().Format("2006-01-02")

	out := make([]dto.StockStatistic, 0, len(s.cfg.TrackStocks))
	for _, stockID := range s.cfg.TrackStocks {
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
