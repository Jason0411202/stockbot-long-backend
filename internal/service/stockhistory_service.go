package service

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// StockHistoryService serves the close-price series for charting, reproducing
// GetStockHistoryData (sqls.go 983-1011): (date, close_price) ascending → dto.
type StockHistoryService struct {
	stock StockStore
	log   *logrus.Logger
}

// NewStockHistoryService wires a StockHistoryService to its stock port.
func NewStockHistoryService(stock StockStore, log *logrus.Logger) *StockHistoryService {
	return &StockHistoryService{stock: stock, log: log}
}

// StockHistoryData returns the ascending close-price series for one stock.
func (s *StockHistoryService) StockHistoryData(ctx context.Context, stockID string) ([]dto.StockHistoryPoint, error) {
	rows, err := s.stock.GetCloseHistoryAsc(ctx, stockID)
	if err != nil {
		return nil, fmt.Errorf("get close history for %s: %w", stockID, err)
	}
	out := make([]dto.StockHistoryPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.StockHistoryPoint{Date: r.Date, Price: r.ClosePrice})
	}
	return out, nil
}
