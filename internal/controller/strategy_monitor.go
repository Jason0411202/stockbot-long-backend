// internal/controller/strategy_monitor.go 提供唯讀決策快照的 HTTP 查詢與錯誤回應。
package controller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/service"
)

// StrategyMonitorReporter 定義讀取最新已保存原始或重建決策的服務介面。
type StrategyMonitorReporter interface {
	// StrategyMonitor 讀取最新保存的決策快照。
	StrategyMonitor(ctx context.Context) (dto.StrategyMonitor, error)
}

// StrategyHistoryReporter 定義永久歷史日期查詢及背景回補進度介面。
type StrategyHistoryReporter interface {
	StrategyMonitorDate(context.Context, string) (dto.StrategyMonitor, error)
	StrategyHistory(context.Context) (dto.StrategyMonitorHistory, error)
}

// WithStrategyMonitor 在啟動 HTTP 服務前注入唯讀監控服務，保留既有建構函式的相依介面。
func (ctl *Controller) WithStrategyMonitor(monitor StrategyMonitorReporter) *Controller {
	ctl.monitor = monitor
	return ctl
}

// StrategyMonitor 回傳最新或指定日期的既有決策；服務未注入或讀取失敗時回傳 503。
func (ctl *Controller) StrategyMonitor(c echo.Context) error {
	log := ctl.reqLog(c)
	log.Info("GET /api/get_strategy_monitor")
	if ctl.monitor == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_monitor_unavailable"})
	}
	// 指定日期僅讀取既有快照，無日期時維持最新紀錄入口。
	if _, specified := c.QueryParams()["date"]; specified {
		date := c.QueryParam("date")
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.IsZero() || parsed.Format("2006-01-02") != date {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_decision_date"})
		}
		reporter, ok := ctl.monitor.(StrategyHistoryReporter)
		if !ok {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_history_unavailable"})
		}
		monitor, err := reporter.StrategyMonitorDate(c.Request().Context(), date)
		if errors.Is(err, service.ErrStrategyHistoryNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "strategy_history_not_found"})
		}
		if err != nil {
			log.WithError(err).Error("Strategy history unavailable")
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_history_unavailable"})
		}
		return c.JSON(http.StatusOK, monitor)
	}

	monitor, err := ctl.monitor.StrategyMonitor(c.Request().Context())
	if err != nil {
		log.WithError(err).Error("StrategyMonitor unavailable")
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_monitor_unavailable"})
	}
	return c.JSONPretty(http.StatusOK, monitor, "  ")
}

// StrategyMonitorHistory 回傳精簡日期索引及不阻塞讀取的回補進度。
func (ctl *Controller) StrategyMonitorHistory(c echo.Context) error {
	reporter, ok := ctl.monitor.(StrategyHistoryReporter)
	if !ok {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_history_unavailable"})
	}
	history, err := reporter.StrategyHistory(c.Request().Context())
	if err != nil {
		ctl.reqLog(c).WithError(err).Error("Strategy history index unavailable")
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "strategy_history_unavailable"})
	}
	return c.JSON(http.StatusOK, history)
}
