// internal/service/ports.go 定義 service 層對外部依賴（資料庫、TWSE 客戶端、通知）的介面 port。
//
// 各 port 宣告於使用端（Go 慣例：接受介面、回傳結構），使每個 service 可用 in-memory fake
// 進行單元測試。internal/repository 與 internal/client/twse 的具體型別實作這些介面。
package service

import (
	"context"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// StockStore 是 StockHistory 資料存取 port，由 *repository.StockHistoryRepository 實作。
type StockStore interface {
	GetStockName(ctx context.Context, stockID string) (string, error)
	GetPriceAsOf(ctx context.Context, stockID, asOf, priceType string) (float64, error)
	GetClosePricesDescAsOf(ctx context.Context, stockID, asOf string) ([]float64, error)
	GetCloseHistoryAsc(ctx context.Context, stockID string) ([]entity.StockHistory, error)
	InsertBarIgnore(ctx context.Context, stockID, stockName string, b entity.Bar) error
}

// LedgerStore 是未實現／已實現帳本資料存取 port，由 *repository.LedgerRepository 實作。
type LedgerStore interface {
	GetLowestUnrealized(ctx context.Context, stockID, asOf string) (entity.UnrealizedGainsLoss, bool, error)
	InsertUnrealized(ctx context.Context, e entity.UnrealizedGainsLoss) error
	DeleteUnrealized(ctx context.Context, stockID, transactionDate string) error
	UpdateUnrealized(ctx context.Context, stockID, transactionDate string, investmentCost float64, shares int) error
	InsertRealized(ctx context.Context, e entity.RealizedGainsLoss) error
	ListUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error)
	ListRealized(ctx context.Context) ([]entity.RealizedGainsLoss, error)
}

// BackfillStore 是 BackfillStatus 資料存取 port，由 *repository.BackfillRepository 實作。
type BackfillStore interface {
	CompletedMonths(ctx context.Context, stockID string) (map[string]bool, error)
	MarkComplete(ctx context.Context, stockID, month string) error
}

// MarketFetcher 是外部市場資料 port，由 *twse.Client 實作。
type MarketFetcher interface {
	FetchMonth(date, stockID string) ([]entity.Bar, string, error)
}

// LedgerSeedStore 是線上啟動時用於還原引擎狀態的帳本唯讀 port，
// 提供 TradingService 從 DB 讀取持倉與各股最後買入日所需的查詢方法。
// 由 *repository.LedgerRepository 實作（與 LedgerStore 為同一具體型別）。
type LedgerSeedStore interface {
	LoadAllUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error)
	LastBuyDateRaw(ctx context.Context, stockID string) (string, bool, error)
}

// SeriesLoader 是 TradingService 用於建構引擎記憶體價格序列的 StockHistory 載入 port，
// 由 *repository.StockHistoryRepository 實作。
type SeriesLoader interface {
	LoadSeries(ctx context.Context, stockIDs []string) (map[string][]entity.StockHistory, error)
}

// StateStore 是 BotState 鍵值儲存 port，用於持久化線上引擎的水位線與現金，
// 由 *repository.BotStateRepository 實作。
type StateStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// Notifier 是對外通知 port，TradingService 使用它發送每筆成交的 Discord embed，
// 由 *client/discord.Client 實作。
type Notifier interface {
	SendEmbed(title, message string, color int) error
}
