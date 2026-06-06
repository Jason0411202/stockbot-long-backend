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

// Controller wires the three business services + a logger into Echo handler
// methods. Dependencies are constructor-injected so handlers stay unit-testable
// with fakes (no DB, no server).
type Controller struct {
	log       *logrus.Logger
	portfolio PortfolioService
	statistic StatisticService
	history   StockHistoryService
}

// New constructs a Controller from its logger and the three business services.
func New(log *logrus.Logger, p PortfolioService, s StatisticService, h StockHistoryService) *Controller {
	return &Controller{log: log, portfolio: p, statistic: s, history: h}
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
	rows, err := ctl.statistic.StockStatisticData(c.Request().Context())
	if err != nil {
		ctl.log.Error("GetStockStatisticData 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.StockStatistic{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}

// StockHistoryData returns the close-price series for the stockId query param.
// A missing stockId is a 400 client error; a service error is swallowed to a
// 200 + empty typed slice (same as the original handler).
func (ctl *Controller) StockHistoryData(c echo.Context) error {
	stockID := c.QueryParam("stockId")
	if stockID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "stockId 參數是必要的"})
	}
	ctl.log.Infof("GET /api/get_stock_history_data?stockId=%s", stockID)
	rows, err := ctl.history.StockHistoryData(c.Request().Context(), stockID)
	if err != nil {
		ctl.log.Error("GetStockHistoryData 發生錯誤:", err)
		return c.JSONPretty(http.StatusOK, []dto.StockHistoryPoint{}, "  ")
	}
	return c.JSONPretty(http.StatusOK, rows, "  ")
}
