// cmd/server 為長線股票模擬交易系統的主程式 (HTTP server + 上線交易引擎)。
//
// 它取代了舊的 root main.go (app_context + sqls.InitDatabase + discord.InitDiscord +
// echoframework.EchoInit + kernals.DailyCheck),改以分層套件的 constructor wiring
// 組裝:pool → clients → repositories → services → controller → echo + 上線 loop。
//
// 啟動順序與 fatal/log 語意刻意對齊舊的 Init(appCtx) + DailyCheck:
//  1. godotenv.Load(".env")        — 失敗僅 Warn (沿用系統環境變數)
//  2. config.Load                  — 失敗 Fatal
//  3. mariadb.OpenPool + InitSchema — 失敗 Fatal (對應舊 sqls.InitDatabase 的 schema 建立)
//  4. 初始 DB 回補 (BackfillMonths/UpdateDatabase) — 失敗 Fatal (對應舊 InitDatabase 的回補)
//  5. discord.NewClient + SendEmbed boot notice — 失敗僅 Error (非致命,沿用舊 InitDiscord)
//  6. go server.Run                — 背景啟動 Echo HTTP server
//  7. tradingSvc.DailyCheck        — 阻塞的上線交易 loop (對應舊 kernals.DailyCheck)
package main

import (
	"context"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
	"github.com/Jason0411202/stockbot-long-backend/internal/client/twse"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/controller"
	"github.com/Jason0411202/stockbot-long-backend/internal/logging"
	"github.com/Jason0411202/stockbot-long-backend/internal/platform/mariadb"
	"github.com/Jason0411202/stockbot-long-backend/internal/repository"
	"github.com/Jason0411202/stockbot-long-backend/internal/server"
	"github.com/Jason0411202/stockbot-long-backend/internal/service"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// main 依序初始化所有元件並啟動 HTTP server 與上線交易 loop。
func main() {
	log := logging.InitLogger()

	if err := godotenv.Load(".env"); err != nil { // .env (DB / Discord 憑證);未找到僅警告
		log.Warn("未找到 .env 檔案，使用系統環境變數")
	}

	cfg, err := config.Load(config.Path())
	if err != nil {
		log.Fatal("載入 config 錯誤:", err)
	}

	ctx := context.Background()

	// --- 連線池 (取代舊 sqls.ConnectToMariadb;DSN 自動補上 StockLongData) ---
	db, err := mariadb.OpenPool(os.Getenv("DB_DSN"))
	if err != nil {
		log.Fatal("初始化資料庫錯誤:", err)
	}
	defer db.Close()

	// --- schema 建立 (取代舊 sqls.InitDatabase 的 SQLcommend.sql + USE) ---
	if err := mariadb.InitSchema(ctx, db); err != nil {
		log.Fatal("初始化資料庫錯誤:", err)
	}
	log.Info("資料庫與對應 table 建立完成")

	// --- repositories (資料存取) ---
	stockRepo := repository.NewStockHistoryRepository(db)
	ledgerRepo := repository.NewLedgerRepository(db)
	stateRepo := repository.NewBotStateRepository(db)
	backfillRepo := repository.NewBackfillRepository(db)

	// --- clients (外部系統) ---
	twseClient := twse.NewClient()
	realtimeClient := twse.NewRealtimeClient() // 盤中即時開盤價 (MIS),供開盤即時決策
	discordClient, err := discord.NewClient(os.Getenv("DISCORD_BOT_TOKEN"), os.Getenv("DISCORD_BOT_CHANNELID"), log)
	if err != nil {
		log.Error("初始化 Discord 錯誤:", err) // 非致命:沿用舊 InitDiscord 的「Error 後繼續」行為
	}

	// --- services (商業邏輯) ---
	marketSvc := service.NewMarketDataService(twseClient, stockRepo, backfillRepo, cfg, log)
	portfolioSvc := service.NewPortfolioService(ledgerRepo, stockRepo, log)
	statSvc := service.NewStatisticService(stockRepo, cfg, log)
	histSvc := service.NewStockHistoryService(stockRepo, log)
	perfSvc := service.NewPerformanceService(cfg, portfolioSvc, stateRepo, stockRepo, log)
	engine := trading.NewEngine(cfg)
	tradingSvc := service.NewTradingService(engine, portfolioSvc, marketSvc, stockRepo, ledgerRepo, stateRepo, discordClient, realtimeClient, cfg, log)

	// --- 初始 DB 回補 (取代舊 sqls.InitDatabase 的回補邏輯) ---
	if cfg.InitDBBackMonths > cfg.MaxBackMonths {
		if err := marketSvc.BackfillMonths(ctx, cfg.InitDBBackMonths); err != nil {
			log.Fatal("initial BackfillMonths 錯誤:", err)
		}
	} else {
		if err := marketSvc.UpdateDatabase(ctx); err != nil {
			log.Fatal("UpdateDatabase 錯誤:", err)
		}
	}

	// --- 啟動通知 (非致命,沿用舊行為) ---
	if discordClient != nil {
		if err := discordClient.SendEmbed("📢 SYSTEM", "長線股票模擬交易系統 Discord bot 順利啟動", 0x00ff00); err != nil {
			log.Error("發送 Discord 訊息失敗:", err)
		}
	}

	// --- controller + echo (HTTP transport) ---
	ctrl := controller.New(log, portfolioSvc, statSvc, histSvc, perfSvc)
	go server.Run(log, db, ctrl)

	// --- 上線交易 loop (阻塞,取代舊 kernals.DailyCheck) ---
	if err := tradingSvc.DailyCheck(ctx); err != nil {
		log.Fatal("上線交易錯誤:", err)
	}
}
