package echoframework

import (
	"main/app_context"
	"main/sqls"
	"net/http"

	"github.com/labstack/echo/v4"
)

// controller.go 的 handler 皆以「建構式 (closure)」形式建立並注入 *app_context.AppContext,
// 不再使用 package-level 全域 appCtx —— 移除 import 期副作用,讓 handler 可被獨立測試。

func newHomeHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.String(http.StatusOK, "Hello, World!")
	}
}

func newUnrealizedGainsLossesHandler(appCtx *app_context.AppContext) echo.HandlerFunc {
	return func(c echo.Context) error {
		appCtx.Log.Info("GET /api/get_unrealized_gains_losses")
		returnValue, err := sqls.GetAllUnrealizedGainsLosses(appCtx)
		if err != nil {
			appCtx.Log.Error("GetAllUnrealizedGainsLosses 發生錯誤:", err)
			return c.JSONPretty(http.StatusOK, []map[string]interface{}{}, "  ")
		}
		return c.JSONPretty(http.StatusOK, returnValue, "  ")
	}
}

func newRealizedGainsLossesHandler(appCtx *app_context.AppContext) echo.HandlerFunc {
	return func(c echo.Context) error {
		appCtx.Log.Info("GET /api/get_realized_gains_losses")
		returnValue, err := sqls.GetAllRealizedGainsLosses(appCtx)
		if err != nil {
			appCtx.Log.Error("GetAllRealizedGainsLosses 發生錯誤:", err)
			return c.JSONPretty(http.StatusOK, []map[string]interface{}{}, "  ")
		}
		return c.JSONPretty(http.StatusOK, returnValue, "  ")
	}
}

func newStockStatisticDataHandler(appCtx *app_context.AppContext) echo.HandlerFunc {
	return func(c echo.Context) error {
		appCtx.Log.Info("GET /api/get_stock_statistic_data")
		returnValue, err := sqls.GetStockStatisticData(appCtx)
		if err != nil {
			appCtx.Log.Error("GetStockStatisticData 發生錯誤:", err)
			return c.JSONPretty(http.StatusOK, []map[string]interface{}{}, "  ")
		}
		return c.JSONPretty(http.StatusOK, returnValue, "  ")
	}
}

func newStockHistoryDataHandler(appCtx *app_context.AppContext) echo.HandlerFunc {
	return func(c echo.Context) error {
		stockId := c.QueryParam("stockId")
		if stockId == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "stockId 參數是必要的"})
		}
		appCtx.Log.Infof("GET /api/get_stock_history_data?stockId=%s", stockId)
		returnValue, err := sqls.GetStockHistoryData(appCtx, stockId)
		if err != nil {
			appCtx.Log.Error("GetStockHistoryData 發生錯誤:", err)
			return c.JSONPretty(http.StatusOK, []map[string]interface{}{}, "  ")
		}
		return c.JSONPretty(http.StatusOK, returnValue, "  ")
	}
}
