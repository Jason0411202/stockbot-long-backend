package service

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
	"github.com/Jason0411202/stockbot-long-backend/internal/client/twse"
	"github.com/Jason0411202/stockbot-long-backend/internal/repository"
)

// Compile-time assertions that the concrete repositories and the TWSE client
// satisfy the service ports exactly. If a repository signature drifts, this file
// fails to compile rather than failing silently at wiring time.
var (
	_ StockStore    = (*repository.StockHistoryRepository)(nil)
	_ LedgerStore   = (*repository.LedgerRepository)(nil)
	_ BackfillStore = (*repository.BackfillRepository)(nil)
	_ MarketFetcher = (*twse.Client)(nil)

	// Online-trading ports (TradingService).
	_ LedgerSeedStore = (*repository.LedgerRepository)(nil)
	_ SeriesLoader    = (*repository.StockHistoryRepository)(nil)
	_ StateStore      = (*repository.BotStateRepository)(nil)
	_ Notifier        = (*discord.Client)(nil)
)
