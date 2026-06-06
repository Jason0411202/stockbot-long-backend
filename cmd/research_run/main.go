package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/logging"
	"github.com/Jason0411202/stockbot-long-backend/internal/platform/mariadb"
	"github.com/Jason0411202/stockbot-long-backend/internal/repository"
	"github.com/Jason0411202/stockbot-long-backend/internal/service"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
)

// cmd/research_run 是一個一次性的回測 runner：
//  1. 載入 .env 與 config.yaml
//  2. 連線 DB (使用既有歷史資料,不觸發 TWSE 爬蟲)
//  3. 由 DB 建構價格序列並執行 RunBacktestOnSeries
//  4. 印出結果並離開
//
// 與主程式差別：不啟動 Discord bot、不啟動 Echo server、不進 infinite loop。
func main() {
	start := time.Now()

	log := logging.InitLogger()

	if err := godotenv.Load(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 未找到 .env，改用系統環境變數:", err)
	}

	cfg, err := config.Load(config.Path())
	if err != nil {
		log.Fatalf("載入 config 失敗: %v", err)
	}

	// 只做 DB 連線,不觸發 TWSE 爬蟲（回測使用既有歷史資料）。
	db, err := mariadb.OpenPool(os.Getenv("DB_DSN"))
	if err != nil {
		log.Fatalf("OpenPool 失敗: %v", err)
	}
	defer db.Close()

	stockRepo := repository.NewStockHistoryRepository(db)
	series, err := service.LoadTradingSeries(context.Background(), stockRepo, cfg.TrackStocks)
	if err != nil {
		log.Fatalf("LoadTradingSeries 失敗: %v", err)
	}

	result, err := backtest.RunBacktestOnSeries(cfg, series)
	if err != nil {
		log.Fatalf("RunBacktestOnSeries 失敗: %v", err)
	}

	printResult(os.Stdout, cfg.TrackStocks, cfg.BackTestingMonths, result, time.Since(start))
}

// printResult 格式化回測結果到 out。抽離 main()(連線/exit)讓輸出格式可被測試。
func printResult(out io.Writer, stocks []string, backMonths int, result *backtest.BacktestResult, elapsed time.Duration) {
	totalIn := result.InitialCash + result.TotalContributed
	fmt.Fprintln(out, "=== BACKTEST RESULT ===")
	fmt.Fprintf(out, "TrackStocks:         %v\n", stocks)
	fmt.Fprintf(out, "BackTestingMonths:   %d\n", backMonths)
	fmt.Fprintf(out, "InitialCash:         %.2f\n", result.InitialCash)
	fmt.Fprintf(out, "TotalContributed:    %.2f (每月注資合計)\n", result.TotalContributed)
	fmt.Fprintf(out, "TotalInvested:       %.2f (期初 + 注資)\n", totalIn)
	fmt.Fprintf(out, "FinalCash:           %.2f\n", result.FinalCash)
	fmt.Fprintf(out, "FinalHoldingValue:   %.2f\n", result.FinalHoldingValue)
	fmt.Fprintf(out, "FinalTotal:          %.2f\n", result.FinalTotal)
	fmt.Fprintf(out, "TotalBuys:           %d\n", result.TotalBuys)
	fmt.Fprintf(out, "TotalSells:          %d\n", result.TotalSells)
	fmt.Fprintf(out, "SkippedBuys:         %d\n", result.SkippedBuys)
	pnl := result.FinalTotal - totalIn
	pct := 0.0
	if totalIn != 0 {
		pct = pnl / totalIn * 100
	}
	fmt.Fprintf(out, "PnL vs Invested:     %+.2f (%+.2f%%)\n", pnl, pct)
	fmt.Fprintf(out, "Runtime:             %s\n", elapsed)
}
