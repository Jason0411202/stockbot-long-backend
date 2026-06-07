// internal/service/portfolio_service.go 負責買賣成交落帳與投資組合 API 資料組裝。
package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// PortfolioService 負責帳本業務邏輯：列出未實現損益明細（含即時價格）、列出已實現損益、
// 買入新 lot 及 FIFO 最低成本賣出迴圈。它不持有任何 SQL，所有資料存取皆透過
// LedgerStore 與 StockStore port 完成。
type PortfolioService struct {
	ledger LedgerStore
	stock  StockStore
	log    *logrus.Logger
}

// NewPortfolioService 建立並回傳一個已完成依賴注入的 PortfolioService。
func NewPortfolioService(ledger LedgerStore, stock StockStore, log *logrus.Logger) *PortfolioService {
	return &PortfolioService{ledger: ledger, stock: stock, log: log}
}

// round2 將浮點數四捨五入至小數點後兩位，與原始 sqls.go 的呈現捨入邏輯一致。
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// UnrealizedGainsLosses 列出所有未實現持倉，並為每筆 lot 計算即時損益。
// 每檔股票的當日收盤價僅查詢一次（而非原始 N+1 逐筆查詢），查詢失敗時以 0 代替，
// 行為與原始逐筆處理相同。
func (s *PortfolioService) UnrealizedGainsLosses(ctx context.Context) ([]dto.UnrealizedGainLoss, error) {
	lots, err := s.ledger.ListUnrealized(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unrealized lots: %w", err)
	}

	// 對每檔不同 stockID 查詢當日收盤價，以 map 快取避免重複查詢。
	today := time.Now().Format("2006-01-02")
	prices := make(map[string]float64, len(lots))
	for _, lot := range lots {
		if _, ok := prices[lot.StockID]; ok {
			continue
		}
		price, perr := s.stock.GetPriceAsOf(ctx, lot.StockID, today, "close_price")
		if perr != nil {
			price = 0 // 與原始行為一致:price 查詢失敗時 todayClosePrice=0
		}
		prices[lot.StockID] = price
	}

	// 逐筆計算預估損益並組裝回應 DTO。
	out := make([]dto.UnrealizedGainLoss, 0, len(lots))
	for _, lot := range lots {
		todayClosePrice := prices[lot.StockID]

		nowValue := todayClosePrice * float64(lot.Shares)
		if lot.Shares == 0 && lot.TransactionPrice > 0 { // 相容舊資料 (未記錄股數者)
			nowValue = (todayClosePrice / lot.TransactionPrice) * lot.InvestmentCost
		}
		predictProfitLoss := nowValue - lot.InvestmentCost
		predictProfitRate := 0.0
		if lot.InvestmentCost > 0 {
			predictProfitRate = (predictProfitLoss / lot.InvestmentCost) * 100
		}

		out = append(out, dto.UnrealizedGainLoss{
			TransactionDate:   lot.TransactionDate,
			StockID:           lot.StockID,
			StockName:         lot.StockName,
			TransactionPrice:  lot.TransactionPrice,
			InvestmentCost:    lot.InvestmentCost,
			Shares:            lot.Shares,
			TodayClosePrice:   todayClosePrice,
			NowValue:          round2(nowValue),
			PredictProfitLoss: round2(predictProfitLoss),
			PredictProfitRate: round2(predictProfitRate),
		})
	}
	return out, nil
}

// RealizedGainsLosses 列出所有已實現損益紀錄，並對金額欄位套用兩位小數的呈現捨入。
func (s *PortfolioService) RealizedGainsLosses(ctx context.Context) ([]dto.RealizedGainLoss, error) {
	rows, err := s.ledger.ListRealized(ctx)
	if err != nil {
		return nil, fmt.Errorf("list realized P&L: %w", err)
	}

	// 逐筆套用呈現捨入並組裝回應 DTO。
	out := make([]dto.RealizedGainLoss, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.RealizedGainLoss{
			BuyDate:        r.BuyDate,
			SellDate:       r.SellDate,
			StockID:        r.StockID,
			StockName:      r.StockName,
			PurchasePrice:  r.PurchasePrice,
			SellPrice:      r.SellPrice,
			InvestmentCost: r.InvestmentCost,
			Revenue:        round2(r.Revenue),
			ProfitLoss:     round2(r.ProfitLoss),
			ProfitRate:     round2(r.ProfitRate),
			Shares:         r.Shares,
		})
	}
	return out, nil
}

// BuyShares 依引擎決策的「成交價 price」寫入一筆新的未實現 lot；investmentCost = price * shares。
// price 由呼叫端 (執行器) 傳入引擎實際成交價 (開盤價基準=當日開盤),確保帳本與引擎現金 / 決策一致;
// 不再以 DB 查價,因線上開盤決策當下 DB 尚無當日 K 棒 (查 close 會誤拿前一交易日)。shares 必須大於 0。
func (s *PortfolioService) BuyShares(ctx context.Context, stockID, today string, shares int, price float64) error {
	if shares <= 0 {
		return fmt.Errorf("shares 必須大於 0, got %d", shares)
	}

	// 查詢股票名稱作為 lot 顯示用;成交價由呼叫端傳入,不再以 DB 查價。
	name, err := s.stock.GetStockName(ctx, stockID)
	if err != nil {
		return fmt.Errorf("get stock name for %s: %w", stockID, err)
	}

	// 組裝 lot entity 並寫入未實現帳本。
	cost := price * float64(shares)
	lot := entity.UnrealizedGainsLoss{
		TransactionDate:  today,
		StockID:          stockID,
		StockName:        name,
		TransactionPrice: price,
		InvestmentCost:   cost,
		Shares:           shares,
	}
	if err := s.ledger.InsertUnrealized(ctx, lot); err != nil {
		return fmt.Errorf("insert unrealized lot for %s: %w", stockID, err)
	}
	return nil
}

// SellShares 從成本最低的未實現 lot 開始賣出 targetShares 股（FIFO 最低成本順序），
// 逐筆從帳本刪除或更新 lot，並以引擎決策的「成交價 todayClose」寫入對應的已實現損益紀錄。
// todayClose 由呼叫端 (執行器) 傳入引擎實際成交價 (開盤價基準=當日開盤),確保已實現損益與引擎一致;
// targetShares <= 0 為 no-op；找不到持倉時視為 no-op 並記錄警告。
func (s *PortfolioService) SellShares(ctx context.Context, stockID, today string, targetShares int, todayClose float64) error {
	if targetShares <= 0 {
		return nil
	}

	remaining := targetShares
	for remaining > 0 {
		lot, found, err := s.ledger.GetLowestUnrealized(ctx, stockID, today)
		if err != nil {
			return fmt.Errorf("get lowest unrealized lot for %s: %w", stockID, err)
		}
		if !found {
			s.log.Warn("賣出時找不到持倉: ", stockID)
			return nil // 無庫存可賣，視為 no-op
		}

		if lot.Shares <= 0 {
			// 舊資料 shares=0，無法以股數為單位處理，直接刪除避免死迴圈。
			if err := s.ledger.DeleteUnrealized(ctx, stockID, lot.TransactionDate); err != nil {
				return fmt.Errorf("delete legacy zero-share lot for %s: %w", stockID, err)
			}
			continue
		}

		if lot.Shares <= remaining {
			// 整筆 lot 賣掉
			soldShares := lot.Shares
			revenue := todayClose * float64(soldShares)
			profitLoss := revenue - lot.InvestmentCost
			profitRate := 0.0
			if lot.InvestmentCost > 0 {
				profitRate = (profitLoss / lot.InvestmentCost) * 100
			}
			// 刪除已售出的 lot，並寫入已實現損益紀錄。
			if err := s.ledger.DeleteUnrealized(ctx, stockID, lot.TransactionDate); err != nil {
				return fmt.Errorf("delete full lot for %s: %w", stockID, err)
			}
			realized := entity.RealizedGainsLoss{
				BuyDate:        lot.TransactionDate,
				SellDate:       today,
				StockID:        stockID,
				StockName:      lot.StockName,
				PurchasePrice:  lot.TransactionPrice,
				SellPrice:      todayClose,
				InvestmentCost: lot.InvestmentCost,
				Revenue:        revenue,
				ProfitLoss:     profitLoss,
				ProfitRate:     profitRate,
				Shares:         soldShares,
			}
			if err := s.ledger.InsertRealized(ctx, realized); err != nil {
				return fmt.Errorf("insert realized (full) for %s: %w", stockID, err)
			}
			remaining -= soldShares
		} else {
			// 只賣 lot 的一部分
			soldShares := remaining
			revenue := todayClose * float64(soldShares)
			soldCost := lot.TransactionPrice * float64(soldShares)
			profitLoss := revenue - soldCost
			profitRate := 0.0
			if soldCost > 0 {
				profitRate = (profitLoss / soldCost) * 100
			}
			newShares := lot.Shares - soldShares
			newCost := lot.InvestmentCost - soldCost
			// 更新 lot 剩餘股數與成本，並寫入已實現損益紀錄。
			if err := s.ledger.UpdateUnrealized(ctx, stockID, lot.TransactionDate, newCost, newShares); err != nil {
				return fmt.Errorf("update partial lot for %s: %w", stockID, err)
			}
			realized := entity.RealizedGainsLoss{
				BuyDate:        lot.TransactionDate,
				SellDate:       today,
				StockID:        stockID,
				StockName:      lot.StockName,
				PurchasePrice:  lot.TransactionPrice,
				SellPrice:      todayClose,
				InvestmentCost: soldCost,
				Revenue:        revenue,
				ProfitLoss:     profitLoss,
				ProfitRate:     profitRate,
				Shares:         soldShares,
			}
			if err := s.ledger.InsertRealized(ctx, realized); err != nil {
				return fmt.Errorf("insert realized (partial) for %s: %w", stockID, err)
			}
			remaining = 0
		}
	}
	return nil
}
