package kernals

import (
	"fmt"
	"io"
	"main/app_context"
	"main/config"
	"os"
	"sort"
)

// backtestWarnSink 是 runBacktestOnSeries 寫 runtime warning 的目的地;
// 預設指向 os.Stderr,測試可注入 buffer 觀察 warning。
var backtestWarnSink io.Writer = os.Stderr

// BacktestResult 是一次回測的數值結果。衡量指標為 FinalTotal = FinalCash + FinalHoldingValue。
type BacktestResult struct {
	InitialCash       float64
	FinalCash         float64
	FinalHoldingValue float64
	FinalTotal        float64
	TotalBuys         int
	TotalSells        int
	SkippedBuys       int // 因現金不足而被跳過的買入次數 (防作弊夾取)
}

// RunBacktest 以記憶體為主的方式,對所有追蹤股票做一次回測。
// 回測與上線使用同一個 Engine + DecideBuy / DecideSell,差別僅在:
//   - 回測使用 noopExecutor (不寫 DB / 不發 Discord)
//   - 回測跑完後停止並回報結果;上線會接續每日 loop
func RunBacktest(appCtx *app_context.AppContext, backTestMonths int) (*BacktestResult, error) {
	if appCtx.Cfg.ScalingStrategy != "Baseline" {
		return nil, fmt.Errorf("回測目前僅支援 Scaling_Strategy=Baseline")
	}
	series, err := loadStockSeries(appCtx)
	if err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return nil, fmt.Errorf("無任何股票歷史資料可供回測")
	}
	return runBacktestOnSeries(appCtx.Cfg, series, backTestMonths)
}

// runBacktestOnSeries 為不依賴 DB 與 Discord 的純函式版本,方便做單元測試。
//
// backTestMonths 表示回測往前推幾個「日曆月」(用 time.AddDate(0, -N, 0) 計算 cutoff 日期),
// 不是 N × 22 個交易日的近似。<= 0 表示停用截尾、用全部資料。
func runBacktestOnSeries(cfg *config.Config, series map[string]*stockSeries, backTestMonths int) (*BacktestResult, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return nil, fmt.Errorf("回測目前僅支援 Scaling_Strategy=Baseline")
	}

	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		return nil, fmt.Errorf("無任何日期可供回測")
	}
	if backTestMonths > 0 {
		latest := allDates[len(allDates)-1]
		cutoff := latest.AddDate(0, -backTestMonths, 0)
		if cutoff.Before(allDates[0]) || cutoff.Equal(allDates[0]) {
			// DB 提供的資料比 back_testing_months 要求的少。即使 config.Load 已經 sanity check 過配置,
			// 真實情況仍可能更短 (TWSE 抓不到、股票上市日晚於 cutoff、手動動過 DB...)。
			fmt.Fprintf(backtestWarnSink,
				"⚠️  back_testing_months=%d 要求往前推到 %s,但 DB 最早資料只到 %s,回測實際使用全部 %d 天\n",
				backTestMonths, cutoff.Format("2006-01-02"),
				allDates[0].Format("2006-01-02"), len(allDates))
		} else {
			// 切到第一個 >= cutoff 的索引
			idx := sort.Search(len(allDates), func(i int) bool {
				return !allDates[i].Before(cutoff)
			})
			allDates = allDates[idx:]
		}
	}

	engine := NewEngine(cfg)
	if err := engine.ProcessDates(allDates, series, noopExecutor{}); err != nil {
		return nil, err
	}

	stats := engine.Stats()
	finalHolding := engine.FinalHoldingValue(series)
	return &BacktestResult{
		InitialCash:       cfg.InitialCash,
		FinalCash:         engine.Cash(),
		FinalHoldingValue: finalHolding,
		FinalTotal:        engine.Cash() + finalHolding,
		TotalBuys:         stats.TotalBuys,
		TotalSells:        stats.TotalSells,
		SkippedBuys:       stats.SkippedBuys,
	}, nil
}
