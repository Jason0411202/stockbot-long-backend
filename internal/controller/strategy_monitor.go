// internal/controller/strategy_monitor.go 提供唯讀決策快照的 HTTP 查詢與錯誤回應。
package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// StrategyMonitorReporter 定義讀取最後一次已保存交易決策的服務介面。
type StrategyMonitorReporter interface {
	// StrategyMonitor 讀取最後一次成功提交的決策快照。
	StrategyMonitor(ctx context.Context) (dto.StrategyMonitor, error)
}

// WithStrategyMonitor 在啟動 HTTP 服務前注入唯讀監控服務，保留既有建構函式的相依介面。
func (ctl *Controller) WithStrategyMonitor(monitor StrategyMonitorReporter) *Controller {
	ctl.monitor = monitor
	return ctl
}

// StrategyMonitor 回傳已保存的真實決策；服務未注入或讀取失敗時明確回傳 503。
func (ctl *Controller) StrategyMonitor(c echo.Context) error {
	log := ctl.reqLog(c)
	log.Info("GET /api/get_strategy_monitor")
	if ctl.monitor == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_monitor_unavailable"})
	}

	monitor, err := ctl.monitor.StrategyMonitor(c.Request().Context())
	if err != nil {
		log.WithError(err).Error("StrategyMonitor unavailable")
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_monitor_unavailable"})
	}
	return c.JSONPretty(http.StatusOK, monitor, "  ")
}
