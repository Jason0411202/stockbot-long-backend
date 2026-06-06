package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/app_context"
	"github.com/Jason0411202/stockbot-long-backend/kernals"
	"github.com/Jason0411202/stockbot-long-backend/sqls"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

// cmd/research_run 是一個一次性的回測 runner：
//  1. 載入 .env 與 config.yaml
//  2. 連線 DB 並視情況更新股價資料
//  3. 執行 RunBacktest 並印出結果
//  4. 離開
//
// 與主程式差別：不啟動 Discord bot、不啟動 Echo server、不進 infinite loop。
func main() {
	start := time.Now()

	if err := godotenv.Load(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 未找到 .env，改用系統環境變數:", err)
	}

	appCtx := app_context.NewAppContext()

	// 只做 DB 連線 + use database，不觸發 TWSE 爬蟲（回測使用既有歷史資料）。
	if err := sqls.ConnectToMariadb(appCtx); err != nil {
		appCtx.Log.Fatalf("ConnectToMariadb 失敗: %v", err)
	}
	if err := sqls.ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		appCtx.Log.Fatalf("ConnectToDatabase 失敗: %v", err)
	}

	result, err := kernals.RunBacktest(appCtx)
	if err != nil {
		appCtx.Log.Fatalf("RunBacktest 失敗: %v", err)
	}

	printResult(os.Stdout, appCtx.Cfg.TrackStocks, appCtx.Cfg.BackTestingMonths, result, time.Since(start))
}

// printResult 格式化回測結果到 out。抽離 main()(連線/exit)讓輸出格式可被測試。
func printResult(out io.Writer, stocks []string, backMonths int, result *kernals.BacktestResult, elapsed time.Duration) {
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
