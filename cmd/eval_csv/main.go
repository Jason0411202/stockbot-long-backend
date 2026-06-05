package main

// cmd/eval_csv 用本機 CSV 快取 (由 cmd/fetch_data 產生) 對 config.yaml 跑一次 walk-forward 評估,
// 印出策略 vs Buy&Hold 的 scorecard 與賣出觸發次數。完全不依賴 MariaDB / docker。
//
// 用途:驗證/重現目前策略表現。先跑 `go run ./cmd/fetch_data` 抓資料,再跑本工具。
import (
	"flag"
	"fmt"
	"math"
	"os"

	"main/config"
	"main/kernals"
)

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 config 失敗:", err)
		os.Exit(1)
	}
	series, err := kernals.LoadSeriesFromCSV(*dataDir, cfg.TrackStocks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 CSV 失敗 (先跑 go run ./cmd/fetch_data):", err)
		os.Exit(1)
	}
	wfp := kernals.WalkForwardParams{WindowMonths: *window, StepMonths: *step, MinTradeDays: *minDays}
	_, agg, err := kernals.EvaluateWalkForward(cfg, series, wfp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "評估失敗:", err)
		os.Exit(1)
	}
	st, _ := kernals.BacktestStats(cfg, series)

	fmt.Printf("追蹤標的 %v;視窗 %d 月 / 步進 %d 月 / %d 個視窗\n", cfg.TrackStocks, *window, *step, agg.NWindows)
	fmt.Println("─────────────────────────────────────────────")
	fmt.Printf("中位 CAGR     策略 %s   vs  B&H %s\n", pct(agg.MedStratCAGR), pct(agg.MedBHCAGR))
	fmt.Printf("中位 MaxDD    策略 %s   vs  B&H %s\n", pct(agg.MedStratMDD), pct(agg.MedBHMDD))
	fmt.Printf("中位 Calmar   策略 %.2f      vs  B&H %.2f\n", agg.MedStratCalmar, agg.MedBHCalmar)
	fmt.Printf("資金利用率    %s\n", pct(agg.MedStratAvgExp))
	fmt.Printf("Calmar 勝率   %s   |   真擇時勝率(vs 同曝險) %s\n", pct(agg.CalmarWinRate), pct(agg.BlendSkillRate))
	fmt.Printf("賣出觸發      移動停利 %d 次 | 獲利了結 %d 次\n", st.TrailSells, st.ProfitSells)
}

func pct(x float64) string {
	if math.IsNaN(x) {
		return "n/a"
	}
	if math.IsInf(x, 0) {
		return "inf"
	}
	return fmt.Sprintf("%+.1f%%", x*100)
}
