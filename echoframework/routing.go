package echoframework

import (
	"main/app_context"

	"github.com/labstack/echo/v4"
)

// EchoRouting 註冊業務 API 路由,並把 appCtx 注入各 handler。
func EchoRouting(e *echo.Echo, appCtx *app_context.AppContext) {
	e.GET("/", newHomeHandler())
	e.GET("/api/get_unrealized_gains_losses", newUnrealizedGainsLossesHandler(appCtx))
	e.GET("/api/get_realized_gains_losses", newRealizedGainsLossesHandler(appCtx))
	e.GET("/api/get_stock_statistic_data", newStockStatisticDataHandler(appCtx))
	e.GET("/api/get_stock_history_data", newStockHistoryDataHandler(appCtx))
}
