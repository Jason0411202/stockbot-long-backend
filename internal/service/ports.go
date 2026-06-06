// Package service is the business-logic layer. It faithfully preserves the
// ledger / ingestion / statistic behavior that previously lived in sqls.go,
// orchestrating the data-access repositories and the external TWSE client
// through small, locally-defined interface ports.
//
// The ports below are declared where they are consumed (Go idiom: accept
// interfaces, return structs) so each service is unit-testable with in-memory
// fakes. The concrete types in internal/repository and internal/client/twse
// satisfy these signatures exactly.
package service

import (
	"context"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// StockStore is the StockHistory data-access port. Satisfied by
// *repository.StockHistoryRepository.
type StockStore interface {
	GetStockName(ctx context.Context, stockID string) (string, error)
	GetPriceAsOf(ctx context.Context, stockID, asOf, priceType string) (float64, error)
	GetClosePricesDescAsOf(ctx context.Context, stockID, asOf string) ([]float64, error)
	GetCloseHistoryAsc(ctx context.Context, stockID string) ([]entity.StockHistory, error)
	InsertBarIgnore(ctx context.Context, stockID, stockName string, b entity.Bar) error
}

// LedgerStore is the unrealized/realized-ledger data-access port. Satisfied by
// *repository.LedgerRepository.
type LedgerStore interface {
	GetLowestUnrealized(ctx context.Context, stockID, asOf string) (entity.UnrealizedGainsLoss, bool, error)
	InsertUnrealized(ctx context.Context, e entity.UnrealizedGainsLoss) error
	DeleteUnrealized(ctx context.Context, stockID, transactionDate string) error
	UpdateUnrealized(ctx context.Context, stockID, transactionDate string, investmentCost float64, shares int) error
	InsertRealized(ctx context.Context, e entity.RealizedGainsLoss) error
	ListUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error)
	ListRealized(ctx context.Context) ([]entity.RealizedGainsLoss, error)
}

// BackfillStore is the BackfillStatus data-access port. Satisfied by
// *repository.BackfillRepository.
type BackfillStore interface {
	CompletedMonths(ctx context.Context, stockID string) (map[string]bool, error)
	MarkComplete(ctx context.Context, stockID, month string) error
}

// MarketFetcher is the external market-data port. Satisfied by *twse.Client.
type MarketFetcher interface {
	FetchMonth(date, stockID string) ([]entity.Bar, string, error)
}

// LedgerSeedStore is the online-startup seed port over the ledger. It exposes
// the read-only queries TradingService needs to restore in-memory engine state
// (positions + per-stock last-buy date) from the DB. Satisfied by
// *repository.LedgerRepository (same concrete type that backs LedgerStore).
type LedgerSeedStore interface {
	LoadAllUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error)
	LastBuyDateRaw(ctx context.Context, stockID string) (string, bool, error)
}

// SeriesLoader is the StockHistory series-load port used by TradingService to
// build the in-memory price series for the engine. Satisfied by
// *repository.StockHistoryRepository.
type SeriesLoader interface {
	LoadSeries(ctx context.Context, stockIDs []string) (map[string][]entity.StockHistory, error)
}

// StateStore is the generic BotState key/value port used to persist the online
// engine's watermark and cash. Satisfied by *repository.BotStateRepository.
type StateStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// Notifier is the outbound-notification port used by TradingService to send
// per-trade embeds. Satisfied by *client/discord.Client.
type Notifier interface {
	SendEmbed(title, message string, color int) error
}
