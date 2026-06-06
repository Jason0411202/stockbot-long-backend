package echoframework

import (
	"os"

	"github.com/Jason0411202/stockbot-long-backend/app_context"
	"github.com/Jason0411202/stockbot-long-backend/internal/handler"
	"github.com/Jason0411202/stockbot-long-backend/internal/middleware"

	"github.com/labstack/echo/v4"
	echoMw "github.com/labstack/echo/v4/middleware"
)

// buildServer 組裝完整的 Echo 物件 (middleware + 業務路由 + 運維路由),但不啟動監聽。
// 抽出此純組裝步驟,讓 routing / health / metrics 接線可被測試 (EchoInit 只多一行 Start)。
func buildServer(appCtx *app_context.AppContext) *echo.Echo {
	e := echo.New() //建立一個 Echo 物件

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
	EchoRouting(e, appCtx) // 設定 routing 規則 (注入 appCtx)

	// --- 運維路由（health check + metrics）---
	e.GET("/health", handler.NewLivenessHandler())          // K8s livenessProbe
	e.GET("/ready", handler.NewReadinessHandler(appCtx.Db)) // K8s readinessProbe
	e.GET("/metrics", middleware.NewMetricsHandler())       // Prometheus 拉指標

	return e
}

func EchoInit(appCtx *app_context.AppContext) {
	// --- Server 啟動（HTTP only，TLS 由外部 Ingress/Caddy 負責）---
	appCtx.Log.Fatal(buildServer(appCtx).Start(":8080"))
}
