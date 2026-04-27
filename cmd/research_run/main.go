package main

import (
	"fmt"
	"os"
	"time"

	"main/app_context"
	"main/kernals"
	"main/sqls"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

// cmd/research_run 是一個一次性的回測 runner：
//   1) 載入 .env 與 config.yaml
//   2) 連線 DB 並視情況更新股價資料
//   3) 執行 RunBacktest 並印出結果
//   4) 離開
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

	result, err := kernals.RunBacktest(appCtx, appCtx.Cfg.BackTestingDays)
	if err != nil {
		appCtx.Log.Fatalf("RunBacktest 失敗: %v", err)
	}

	elapsed := time.Since(start)
	fmt.Println("=== BACKTEST RESULT ===")
	fmt.Printf("TrackStocks:         %v\n", appCtx.Cfg.TrackStocks)
	fmt.Printf("BackTestingDays:     %d\n", appCtx.Cfg.BackTestingDays)
	fmt.Printf("InitialCash:         %.2f\n", result.InitialCash)
	fmt.Printf("FinalCash:           %.2f\n", result.FinalCash)
	fmt.Printf("FinalHoldingValue:   %.2f\n", result.FinalHoldingValue)
	fmt.Printf("FinalTotal:          %.2f\n", result.FinalTotal)
	fmt.Printf("TotalBuys:           %d\n", result.TotalBuys)
	fmt.Printf("TotalSells:          %d\n", result.TotalSells)
	fmt.Printf("SkippedBuys:         %d\n", result.SkippedBuys)
	fmt.Printf("PnL vs Initial:      %+.2f (%+.2f%%)\n",
		result.FinalTotal-result.InitialCash,
		(result.FinalTotal-result.InitialCash)/result.InitialCash*100)
	fmt.Printf("Runtime:             %s\n", elapsed)
}
