package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"main/app_context"
	"main/kernals"
	"main/sqls"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

// cmd/research_run 是一個一次性的回測 runner：
//   1) 載入 .env 與 config.yaml
//   2) 連線 DB 並使用既有歷史資料（不觸發爬蟲）
//   3) 執行 RunBacktest 並印出結果
//   4) 若指定 --output_dir 則寫入 result.json
//   5) 離開
//
// CLI flags（Plan v1 新增）允許在不改動 config.yaml 的前提下覆寫超參數，
// 讓 parallel runner (scripts/run_plan_v1.sh) 能同時跑 baseline vs TF 兩路。
func main() {
	var (
		useTF           = flag.Bool("use_tf", false, "enable Plan v1 Trend-Following branch")
		tfTau           = flag.Float64("tf_tau", 0.02, "TF 多頭判定閾值 (MA20 > (1+tau)*MA60)")
		tfAmountMode    = flag.String("tf_amount_mode", "const", "TF 金額形式: const | cashfrac")
		tfAlpha         = flag.Float64("tf_alpha", 2.0, "TF const 模式: 乘上最大 Pyramid tier 的倍率")
		tfBeta          = flag.Float64("tf_beta", 0.02, "TF cashfrac 模式: 佔當下現金比例")
		backTestingDays = flag.Int("back_testing_days_override", 0, "override config.BackTestingDays (0 = 沿用 config)")
		outputDir       = flag.String("output_dir", "", "若非空，將 BacktestResult 寫成 JSON 至此目錄")
		seed            = flag.Int("seed", 0, "seed (回測目前為 deterministic，此 flag 僅為 runner 介面一致性而保留)")
	)
	flag.Parse()
	_ = seed // 回測流程本身不使用 seed

	start := time.Now()

	if err := godotenv.Load(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 未找到 .env，改用系統環境變數:", err)
	}

	appCtx := app_context.NewAppContext()

	// --- 將 CLI flags 套用到 cfg (僅覆寫使用者明確指定的欄位) ---
	appCtx.Cfg.UseTFBranch = *useTF
	appCtx.Cfg.TFTau = *tfTau
	appCtx.Cfg.TFAmountMode = *tfAmountMode
	appCtx.Cfg.TFAlpha = *tfAlpha
	appCtx.Cfg.TFBeta = *tfBeta
	if *backTestingDays > 0 {
		appCtx.Cfg.BackTestingDays = *backTestingDays
	}

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
	fmt.Printf("UseTFBranch:         %v\n", appCtx.Cfg.UseTFBranch)
	if appCtx.Cfg.UseTFBranch {
		fmt.Printf("TF tau/mode/alpha/beta: %.4f / %s / %.2f / %.4f\n",
			appCtx.Cfg.TFTau, appCtx.Cfg.TFAmountMode, appCtx.Cfg.TFAlpha, appCtx.Cfg.TFBeta)
	}
	fmt.Printf("TrackStocks:         %v\n", appCtx.Cfg.TrackStocks)
	fmt.Printf("BackTestingDays:     %d\n", appCtx.Cfg.BackTestingDays)
	fmt.Printf("InitialCash:         %.2f\n", result.InitialCash)
	fmt.Printf("FinalCash:           %.2f\n", result.FinalCash)
	fmt.Printf("FinalHoldingValue:   %.2f\n", result.FinalHoldingValue)
	fmt.Printf("FinalTotal:          %.2f\n", result.FinalTotal)
	fmt.Printf("TotalBuys:           %d\n", result.TotalBuys)
	fmt.Printf("TotalSells:          %d\n", result.TotalSells)
	fmt.Printf("PnL vs Initial:      %+.2f (%+.2f%%)\n",
		result.FinalTotal-result.InitialCash,
		(result.FinalTotal-result.InitialCash)/result.InitialCash*100)
	fmt.Printf("Runtime:             %s\n", elapsed)

	if *outputDir != "" {
		if err := os.MkdirAll(*outputDir, 0o755); err != nil {
			appCtx.Log.Fatalf("MkdirAll %s 失敗: %v", *outputDir, err)
		}
		outPath := filepath.Join(*outputDir, "result.json")
		payload := struct {
			UseTFBranch       bool    `json:"use_tf_branch"`
			TFTau             float64 `json:"tf_tau"`
			TFAmountMode      string  `json:"tf_amount_mode"`
			TFAlpha           float64 `json:"tf_alpha"`
			TFBeta            float64 `json:"tf_beta"`
			BackTestingDays   int     `json:"back_testing_days"`
			InitialCash       float64 `json:"initial_cash"`
			FinalCash         float64 `json:"final_cash"`
			FinalHoldingValue float64 `json:"final_holding_value"`
			FinalTotal        float64 `json:"final_total"`
			TotalBuys         int     `json:"total_buys"`
			TotalSells        int     `json:"total_sells"`
			RuntimeSeconds    float64 `json:"runtime_seconds"`
		}{
			UseTFBranch:       appCtx.Cfg.UseTFBranch,
			TFTau:             appCtx.Cfg.TFTau,
			TFAmountMode:      appCtx.Cfg.TFAmountMode,
			TFAlpha:           appCtx.Cfg.TFAlpha,
			TFBeta:            appCtx.Cfg.TFBeta,
			BackTestingDays:   appCtx.Cfg.BackTestingDays,
			InitialCash:       result.InitialCash,
			FinalCash:         result.FinalCash,
			FinalHoldingValue: result.FinalHoldingValue,
			FinalTotal:        result.FinalTotal,
			TotalBuys:         result.TotalBuys,
			TotalSells:        result.TotalSells,
			RuntimeSeconds:    elapsed.Seconds(),
		}
		data, err := json.MarshalIndent(&payload, "", "  ")
		if err != nil {
			appCtx.Log.Fatalf("json.MarshalIndent 失敗: %v", err)
		}
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			appCtx.Log.Fatalf("寫入 %s 失敗: %v", outPath, err)
		}
		fmt.Println("wrote", outPath)
	}
}
