// Package controller holds the HTTP transport layer: Echo handler methods that
// log, delegate to a service, and shape the response. It replaces the closure
// handlers in echoframework/controller.go. The controller depends only on small
// consumer-side service interfaces + *logrus.Logger — no sqls, no app_context,
// no internal/service. This keeps the transport layer free of business logic.
package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// PortfolioService is the consumer-side view of the ledger orchestration the
// controller needs: live-P&L unrealized lots and realized P&L rows.
type PortfolioService interface {
	UnrealizedGainsLosses(ctx context.Context) ([]dto.UnrealizedGainLoss, error)
	RealizedGainsLosses(ctx context.Context) ([]dto.RealizedGainLoss, error)
}

// StatisticService is the consumer-side view of the per-tracked-stock display
// data (name, today's close, lower/upper-point day distances).
type StatisticService interface {
	StockStatisticData(ctx context.Context) ([]dto.StockStatistic, error)
}

// StockHistoryService is the consumer-side view of the close-price series used
// for charting one stock.
type StockHistoryService interface {
	StockHistoryData(ctx context.Context, stockID string) ([]dto.StockHistoryPoint, error)
}

// PerformanceReporter is the consumer-side view of the strategy performance
// summary: principal breakdown + live portfolio state + backtest metrics.
type PerformanceReporter interface {
	Summary(ctx context.Context) (dto.PerformanceSummary, error)
}

// EquityHistoryReporter is the consumer-side view of the live daily equity
// history: the real account's cash / holding / total-equity time series.
type EquityHistoryReporter interface {
	EquityHistory(ctx context.Context) ([]dto.LiveEquityPoint, error)
}

// Controller wires the five business services + a logger into Echo handler
// methods. Dependencies are constructor-injected so handlers stay unit-testable
// with fakes (no DB, no server).
type Controller struct {
	log         *logrus.Logger
	portfolio   PortfolioService
	statistic   StatisticService
	history     StockHistoryService
	performance PerformanceReporter
	equity      EquityHistoryReporter
}

// New constructs a Controller from its logger and the five business services.
func New(log *logrus.Logger, p PortfolioService, s StatisticService, h StockHistoryService, perf PerformanceReporter, eq EquityHistoryReporter) *Controller {
	return &Controller{log: log, portfolio: p, statistic: s, history: h, performance: perf, equity: eq}
}

// Home replies with the static landing string (always 200).
func (ctl *Controller) Home(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!")
}

// UnrealizedGainsLosses returns the unrealized lots enriched with live P&L.
// On service error it logs and returns 200 + an empty typed slice (error
// swallowed, not surfaced), preserving the original handler behavior.
func (ctl *Controller) UnrealizedGainsLosses(c echo.Context) error {
	ctl.log.Info("GET /api/get_unrealized_gains_losses")
	// 呼叫 service 取得未實現損益列表;發生錯誤時回傳空陣列維持原有行為。
	rows, err := ctl.portfolio.UnrealizedGainsLosses(c.Request().Context())
	if err != nil {
		ctl.log.Error("GetAllUnrealizedGainsLosses 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.UnrealizedGainLoss{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}

// RealizedGainsLosses returns the realized P&L rows. On service error it logs
// and returns 200 + an empty typed slice.
func (ctl *Controller) RealizedGainsLosses(c echo.Context) error {
	ctl.log.Info("GET /api/get_realized_gains_losses")
	// 呼叫 service 取得已實現損益列表;發生錯誤時回傳空陣列維持原有行為。
	rows, err := ctl.portfolio.RealizedGainsLosses(c.Request().Context())
	if err != nil {
		ctl.log.Error("GetAllRealizedGainsLosses 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.RealizedGainLoss{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}

// StockStatisticData returns per-tracked-stock display data. On service error
// it logs and returns 200 + an empty typed slice.
func (ctl *Controller) StockStatisticData(c echo.Context) error {
	ctl.log.Info("GET /api/get_stock_statistic_data")
	// 呼叫 service 取得各追蹤股票的顯示資料;發生錯誤時回傳空陣列維持原有行為。
	rows, err := ctl.statistic.StockStatisticData(c.Request().Context())
	if err != nil {
		ctl.log.Error("GetStockStatisticData 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.StockStatistic{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}

// PerformanceSummary returns the strategy performance summary (principal
// breakdown + live portfolio state + backtest metrics). On service error it logs
// and returns 200 + an empty typed object (error swallowed), matching the other
// handlers' lenient contract.
func (ctl *Controller) PerformanceSummary(c echo.Context) error {
	ctl.log.Info("GET /api/get_performance_summary")
	// 呼叫 service 取得績效摘要;發生錯誤時回傳空物件維持前端寬鬆契約。
	summary, err := ctl.performance.Summary(c.Request().Context())
	if err != nil {
		ctl.log.Error("PerformanceSummary 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, dto.PerformanceSummary{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, summary, "  ")
}

// EquityHistory returns the live daily equity time series (cash / holding /
// total equity) for the frontend's historical equity chart. On service error it
// logs and returns 200 + an empty typed slice, matching the other handlers'
// lenient contract.
func (ctl *Controller) EquityHistory(c echo.Context) error {
	ctl.log.Info("GET /api/get_equity_history")
	// 呼叫 service 取得每日權益歷史;發生錯誤時回傳空陣列維持前端寬鬆契約。
	rows, err := ctl.equity.EquityHistory(c.Request().Context())
	if err != nil {
		ctl.log.Error("EquityHistory 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.LiveEquityPoint{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}

// StockHistoryData returns the close-price series for the stockId query param.
// A missing stockId is a 400 client error; a service error is swallowed to a
// 200 + empty typed slice (same as the original handler).
func (ctl *Controller) StockHistoryData(c echo.Context) error {
	// 讀取必要的 stockId query 參數;缺少時回傳 400。
	stockID := c.QueryParam("stockId")
	if stockID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "stockId 參數是必要的"})
	}
	ctl.log.Infof("GET /api/get_stock_history_data?stockId=%s", stockID)
	// 呼叫 service 取得收盤價歷史序列;發生錯誤時回傳空陣列維持原有行為。
	rows, err := ctl.history.StockHistoryData(c.Request().Context(), stockID)
	if err != nil {
		ctl.log.Error("GetStockHistoryData 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.StockHistoryPoint{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}
