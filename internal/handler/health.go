// internal/handler/health.go 提供 liveness 與 readiness probe handler。
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
		// 固定回傳 200 + {"status":"ok"},表示 process 本身正常運行。
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
}

// NewReadinessHandler 回傳 readiness probe handler
// K8s readinessProbe 用：失敗 → 從 Service 流量名單移除（不重啟）
// 檢查 DB 連線：連不上就不該接收新 request
func NewReadinessHandler(db *sql.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		// db 為 nil 時表示資料庫尚未初始化,回傳 503。
		if db == nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"reason": "database not initialized",
			})
		}
		// Ping 失敗表示資料庫無法連線,回傳 503。
		if err := db.Ping(); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"reason": "database unreachable",
			})
		}
		// 資料庫連線正常,回傳 200 + {"status":"ready"}。
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	}
}
