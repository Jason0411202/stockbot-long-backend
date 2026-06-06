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

// PortfolioService owns the ledger business logic previously in sqls.go:
// listing unrealized lots with live P&L, listing realized P&L, buying a new lot
// and the FIFO-lowest-cost sell loop. It holds no SQL — it orchestrates the
// LedgerStore and StockStore ports.
type PortfolioService struct {
	ledger LedgerStore
	stock  StockStore
	log    *logrus.Logger
}

// NewPortfolioService wires a PortfolioService to its ledger/stock ports.
func NewPortfolioService(ledger LedgerStore, stock StockStore, log *logrus.Logger) *PortfolioService {
	return &PortfolioService{ledger: ledger, stock: stock, log: log}
}

// round2 mirrors the original sqls.go presentation rounding (math.Round(x*100)/100).
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// UnrealizedGainsLosses lists every unrealized lot enriched with live P&L,
// faithfully reproducing GetAllUnrealizedGainsLosses (sqls.go 844-899).
//
// The original issued one price query per lot (N+1); here the latest close is
// fetched once per distinct stock_id (today's date, "close_price"), with 0 used
// on any lookup error — numerically identical to the original per-row behavior.
func (s *PortfolioService) UnrealizedGainsLosses(ctx context.Context) ([]dto.UnrealizedGainLoss, error) {
	lots, err := s.ledger.ListUnrealized(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unrealized lots: %w", err)
	}

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

// RealizedGainsLosses lists realized P&L rows, reproducing
// GetAllRealizedGainsLosses (sqls.go 901-948): no computation, only the 2dp
// presentation rounding on revenue/profit_loss/profit_rate (941-943).
func (s *PortfolioService) RealizedGainsLosses(ctx context.Context) ([]dto.RealizedGainLoss, error) {
	rows, err := s.ledger.ListRealized(ctx)
	if err != nil {
		return nil, fmt.Errorf("list realized P&L: %w", err)
	}

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

// BuyShares writes one new unrealized lot, reproducing SQLBuyStock
// (sqls.go 646-670): investmentCost = price * shares, with a shares<=0 guard.
func (s *PortfolioService) BuyShares(ctx context.Context, stockID, today string, shares int) error {
	if shares <= 0 {
		return fmt.Errorf("shares 必須大於 0, got %d", shares)
	}

	price, err := s.stock.GetPriceAsOf(ctx, stockID, today, "close_price")
	if err != nil {
		return fmt.Errorf("get close price for %s: %w", stockID, err)
	}
	name, err := s.stock.GetStockName(ctx, stockID)
	if err != nil {
		return fmt.Errorf("get stock name for %s: %w", stockID, err)
	}

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

// SellShares sells targetShares starting from the cheapest unrealized lot,
// porting the FIFO-lowest-cost matching loop of SQLSellStock (sqls.go 674-751)
// formula-for-formula.
func (s *PortfolioService) SellShares(ctx context.Context, stockID, today string, targetShares int) error {
	if targetShares <= 0 {
		return nil
	}

	todayClose, err := s.stock.GetPriceAsOf(ctx, stockID, today, "close_price")
	if err != nil {
		return fmt.Errorf("get close price for %s: %w", stockID, err)
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
