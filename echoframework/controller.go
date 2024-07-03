package echoframework

import (
	"main/logs"
	"main/sqls"
	"net/http"

	"github.com/labstack/echo/v4"
)

var log = logs.InitLogger()

func home(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!")
}

func get_unrealized_gains_losses(c echo.Context) error {
	if c.Request().Method == "GET" {
		log.Info("GET /api/get_unrealized_gains_losses")
		returnValue, err := sqls.GetAllUnrealizedGainsLosses(log)
		if err != nil {
			log.Error("GetAllUnrealizedGainsLosses 發生錯誤:", err)
			return c.JSONPretty(http.StatusOK, []map[string]interface{}{}, "  ")
		}

		return c.JSONPretty(http.StatusOK, returnValue, "  ")
	} else {
		return c.String(http.StatusMethodNotAllowed, "Method Not Allowed")
	}
}

func get_realized_gains_losses(c echo.Context) error {
	if c.Request().Method == "GET" {
		log.Info("GET /api/get_realized_gains_losses")
		returnValue, err := sqls.GetAllRealizedGainsLosses(log)
		if err != nil {
			log.Error("GetAllRealizedGainsLosses 發生錯誤:", err)
			return c.JSONPretty(http.StatusOK, []map[string]interface{}{}, "  ")
		}

		return c.JSONPretty(http.StatusOK, returnValue, "  ")
	} else {
		return c.String(http.StatusMethodNotAllowed, "Method Not Allowed")
	}
}
