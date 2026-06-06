package kernals

import (
	"fmt"
	"io"
	"main/app_context"
	"main/config"
	"os"
	"sort"
	"time"
)

// backtestWarnSink 是 runBacktestOnSeries 寫 runtime warning 的目的地;
// 預設指向 os.Stderr,測試可注入 buffer 觀察 warning。
var backtestWarnSink io.Writer = os.Stderr

// BacktestResult 是一次回測的數值結果。衡量指標為 FinalTotal = FinalCash + FinalHoldingValue。
// 問題設定含每月注資時,TotalContributed 為期間注入的新資金,投入本金總額 = InitialCash + TotalContributed。
type BacktestResult struct {
	InitialCash       float64
	TotalContributed  float64 // 期間每月注入的新資金合計 (cfg.MonthlyContribution>0 時)
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
//
// 它現在只負責「把 backTestMonths 換算成 [start, end] 區間 (含原本的不足資料 warning)」,
// 實際模擬委派給 runBacktestWindow。
func runBacktestOnSeries(cfg *config.Config, series map[string]*stockSeries, backTestMonths int) (*BacktestResult, error) {
	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		return nil, fmt.Errorf("無任何日期可供回測")
	}
	start := allDates[0]
	end := allDates[len(allDates)-1]
	if backTestMonths > 0 {
		cutoff := end.AddDate(0, -backTestMonths, 0)
		if cutoff.Before(allDates[0]) || cutoff.Equal(allDates[0]) {
			// DB 提供的資料比 back_testing_months 要求的少。即使 config.Load 已經 sanity check 過配置,
			// 真實情況仍可能更短 (TWSE 抓不到、股票上市日晚於 cutoff、手動動過 DB...)。
			fmt.Fprintf(backtestWarnSink,
				"⚠️  back_testing_months=%d 要求往前推到 %s,但 DB 最早資料只到 %s,回測實際使用全部 %d 天\n",
				backTestMonths, cutoff.Format("2006-01-02"),
				allDates[0].Format("2006-01-02"), len(allDates))
		} else {
			start = cutoff
		}
	}
	return runBacktestWindow(cfg, series, start, end)
}

// runBacktestWindow 是「對任意 [start, end] 日期區間」做一次回測的核心 (fresh engine,fresh 現金池)。
// 走查同一份 series,持股以 HoldingValueAsOf(series, end) 收尾,故結束日 < 全序列最後日的視窗也能正確估值。
// 這是 walk-forward 滾動視窗評估的基本單位 —— 每個視窗各自 NewEngine,彼此無狀態洩漏。
func runBacktestWindow(cfg *config.Config, series map[string]*stockSeries, start, end time.Time) (*BacktestResult, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return nil, fmt.Errorf("回測目前僅支援 Scaling_Strategy=Baseline")
	}
	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		return nil, fmt.Errorf("無任何日期可供回測")
	}
	lo := sort.Search(len(allDates), func(i int) bool { return !allDates[i].Before(start) })
	hi := sort.Search(len(allDates), func(i int) bool { return allDates[i].After(end) })
	if lo >= hi {
		return nil, fmt.Errorf("視窗 %s ~ %s 內無交易日",
			start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	windowDates := allDates[lo:hi]

	// 問題設定:每月第一個交易日注入 cfg.MonthlyContribution 新資金 (起始月除外;<=0 為關閉)。
	contribOnDay := contributionAmounts(windowDates, cfg.MonthlyContribution)
	engine := NewEngine(cfg)
	totalContrib := 0.0
	for i, d := range windowDates {
		if contribOnDay[i] > 0 {
			engine.AddCash(contribOnDay[i])
			totalContrib += contribOnDay[i]
		}
		if err := engine.ProcessDay(d, series, noopExecutor{}); err != nil {
			return nil, err
		}
	}

	stats := engine.Stats()
	finalHolding := engine.HoldingValueAsOf(series, end)
	return &BacktestResult{
		InitialCash:       cfg.InitialCash,
		TotalContributed:  totalContrib,
		FinalCash:         engine.Cash(),
		FinalHoldingValue: finalHolding,
		FinalTotal:        engine.Cash() + finalHolding,
		TotalBuys:         stats.TotalBuys,
		TotalSells:        stats.TotalSells,
		SkippedBuys:       stats.SkippedBuys,
	}, nil
}
