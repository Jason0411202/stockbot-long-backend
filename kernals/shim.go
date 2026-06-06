package kernals

import (
	"fmt"
	"github.com/Jason0411202/stockbot-long-backend/app_context"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
)

// shim.go 提供向後相容的型別別名與函式轉發,讓既有呼叫端 (main.go / cmd/*) 完全不需改動:
// 重構後 BacktestResult / WalkForwardParams / … 等型別與 Load*/Evaluate* 等純函式皆遷移到
// internal/service/backtest;此處以型別別名 (type alias) 與函式轉發 (var = …) 重新匯出。
// 帶 appCtx (DB I/O) 的 RunBacktest / RunWalkForward 仍實作於本層 (見下),只是內部委派純核心。

// ── 型別別名 (與原 kernals 型別完全相同,呼叫端可無痛沿用) ──
type (
	BacktestResult    = backtest.BacktestResult
	WalkForwardParams = backtest.WalkForwardParams
	SeriesMetrics     = backtest.SeriesMetrics
	WindowReport      = backtest.WindowReport
	AggregateReport   = backtest.AggregateReport
	OOSFold           = backtest.OOSFold
	RollingOOSReport  = backtest.RollingOOSReport
)

// ── 純函式轉發 (CSV 載入 + 各評估 API) ──
var (
	LoadSeriesFromCSV   = backtest.LoadSeriesFromCSV
	EvaluateFullSpan    = backtest.EvaluateFullSpan
	EvaluateWalkForward = backtest.EvaluateWalkForward
	EvaluateRollingOOS  = backtest.EvaluateRollingOOS
)

// RunBacktest 以記憶體為主的方式,對所有追蹤股票做一次回測 (appCtx 版,供 cmd 呼叫)。
// 載入 DB series 後委派給純核心 backtest.RunBacktestOnSeries。
//
// 回測與上線使用同一個引擎 + 決策,差別僅在回測使用 NoopExecutor (不寫 DB / 不發 Discord) 且跑完即停。
// 回測區間固定為 [common issuance, 最後資料日]。
func RunBacktest(appCtx *app_context.AppContext) (*BacktestResult, error) {
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
	return backtest.RunBacktestOnSeries(appCtx.Cfg, series)
}

// RunWalkForward 載入 DB series 後執行 walk-forward 評估 (appCtx 版,供 cmd 呼叫)。
// 委派給純核心 backtest.EvaluateWalkForward。
func RunWalkForward(appCtx *app_context.AppContext, p WalkForwardParams) ([]WindowReport, AggregateReport, error) {
	series, err := loadStockSeries(appCtx)
	if err != nil {
		return nil, AggregateReport{}, err
	}
	if len(series) == 0 {
		return nil, AggregateReport{}, fmt.Errorf("無任何股票歷史資料可供回測")
	}
	return backtest.EvaluateWalkForward(appCtx.Cfg, series, p)
}
