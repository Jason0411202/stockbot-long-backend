package handler

import (
	"database/sql"
	"net/http"

	"github.com/labstack/echo/v4"
)

// NewLivenessHandler 回傳 liveness probe handler
// K8s livenessProbe 用：失敗 → 重啟 Pod
// 不檢查外部依賴（DB 掛了重啟 app 也沒用）
func NewLivenessHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
}

// NewReadinessHandler 回傳 readiness probe handler
// K8s readinessProbe 用：失敗 → 從 Service 流量名單移除（不重啟）
// 檢查 DB 連線：連不上就不該接收新 request
func NewReadinessHandler(db *sql.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		if db == nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"reason": "database not initialized",
			})
		}
		if err := db.Ping(); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"reason": "database unreachable",
			})
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	}
}
