// internal/service/stockhistory_service.go 提供歷史收盤價 API 的資料轉換。
package service

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// StockHistoryService 提供繪圖用的收盤價時序資料，由 StockStore port 讀取升冪排列的歷史紀錄，
// 並轉換為 dto.StockHistoryPoint 回傳。
type StockHistoryService struct {
	stock StockStore
	log   *logrus.Logger
}

// NewStockHistoryService 建立並回傳一個已完成依賴注入的 StockHistoryService。
func NewStockHistoryService(stock StockStore, log *logrus.Logger) *StockHistoryService {
	return &StockHistoryService{stock: stock, log: log}
}

// StockHistoryData 回傳指定股票由舊到新排列的收盤價序列。
func (s *StockHistoryService) StockHistoryData(ctx context.Context, stockID string) ([]dto.StockHistoryPoint, error) {
	rows, err := s.stock.GetCloseHistoryAsc(ctx, stockID)
	if err != nil {
		return nil, fmt.Errorf("get close history for %s: %w", stockID, err)
	}
	// 逐筆轉換為 DTO 後回傳。
	out := make([]dto.StockHistoryPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.StockHistoryPoint{Date: r.Date, Price: r.ClosePrice})
	}
	return out, nil
}
