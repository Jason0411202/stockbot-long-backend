// Package server owns Echo construction: the middleware chain, business + ops
// route registration, and Start. It replaces echoframework/entry.go and
// echoframework/routing.go. The server depends on a constructed
// *controller.Controller (handler methods) and a *sql.DB (readiness probe) —
// it does not touch app_context, sqls, or the business services directly.
package server

import (
	"database/sql"
	"os"

	"github.com/labstack/echo/v4"
	echoMw "github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/controller"
	"github.com/Jason0411202/stockbot-long-backend/internal/handler"
	"github.com/Jason0411202/stockbot-long-backend/internal/middleware"
)

// BuildEcho assembles the full Echo object (middleware + business routes + ops
// routes) without starting the listener. Extracting this pure assembly step
// lets routing / health / metrics wiring be tested (Run only adds Start).
func BuildEcho(log *logrus.Logger, db *sql.DB, ctl *controller.Controller) *echo.Echo {
	e := echo.New() // 建立一個 Echo 物件

	// --- 既有 middleware ---
	e.Use(echoMw.CORSWithConfig(echoMw.CORSConfig{ // 設定 CORS
		AllowOrigins: []string{"*"},                                        // 允許所有來源
		AllowMethods: []string{echo.GET, echo.PUT, echo.POST, echo.DELETE}, // 允許的 HTTP 方法
	}))

	// --- 生產環境 middleware ---
	if os.Getenv("LOG_FORMAT") == "json" {
		e.Use(middleware.NewRequestLogger()) // JSON 結構化日誌（Fluent Bit → ELK）
	}
	e.Use(middleware.NewMetricsMiddleware()) // Prometheus metrics 收集

	// --- 既有業務路由 ---
	registerRoutes(e, ctl) // 設定 routing 規則 (注入 controller)

	// --- 運維路由（health check + metrics）---
	e.GET("/health", handler.NewLivenessHandler())    // K8s livenessProbe
	e.GET("/ready", handler.NewReadinessHandler(db))  // K8s readinessProbe
	e.GET("/metrics", middleware.NewMetricsHandler()) // Prometheus 拉指標

	return e
}

// registerRoutes wires the business API routes to the controller's handler
// methods (was echoframework.EchoRouting).
func registerRoutes(e *echo.Echo, ctl *controller.Controller) {
	// 將業務 API 路徑對應至 controller 的 handler 方法。
	e.GET("/", ctl.Home)
	e.GET("/api/get_unrealized_gains_losses", ctl.UnrealizedGainsLosses)
	e.GET("/api/get_realized_gains_losses", ctl.RealizedGainsLosses)
	e.GET("/api/get_stock_statistic_data", ctl.StockStatisticData)
	e.GET("/api/get_stock_history_data", ctl.StockHistoryData)
	e.GET("/api/get_performance_summary", ctl.PerformanceSummary)
}

// Run builds the Echo server and starts listening on :8080 (HTTP only; TLS is
// handled by an external Ingress/Caddy). Was echoframework.EchoInit.
func Run(log *logrus.Logger, db *sql.DB, ctl *controller.Controller) {
	log.Fatal(BuildEcho(log, db, ctl).Start(":8080"))
}
