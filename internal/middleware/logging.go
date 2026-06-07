// internal/middleware/logging.go 提供 Echo request 結構化日誌 middleware。
package middleware

import (
	"encoding/json"
	"os"
	"time"

	"github.com/labstack/echo/v4"
)

// requestLog 定義 JSON log 的結構
// Fluent Bit 會從 stdout 讀取這些 JSON → 送到 Elasticsearch
type requestLog struct {
	Timestamp string `json:"timestamp"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Query     string `json:"query,omitempty"`
	Status    int    `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	RemoteIP  string `json:"remote_ip"`
	UserAgent string `json:"user_agent"`
}

// NewRequestLogger 回傳 JSON 結構化日誌的 Echo middleware
// 每個 HTTP request 輸出一行 JSON 到 stdout
// 使用標準庫 encoding/json，不引入新依賴
func NewRequestLogger() echo.MiddlewareFunc {
	// 建立共用的 JSON encoder,寫入 stdout 供 Fluent Bit 讀取。
	encoder := json.NewEncoder(os.Stdout)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 記錄請求開始時間,用於計算延遲。
			start := time.Now()

			// 先執行後續 handler,再收集回應狀態與延遲。
			err := next(c)

			// 組裝結構化日誌並以 JSON 格式寫出至 stdout。
			entry := requestLog{
				Timestamp: start.Format(time.RFC3339),
				Method:    c.Request().Method,
				Path:      c.Request().URL.Path,
				Query:     c.Request().URL.RawQuery,
				Status:    c.Response().Status,
				LatencyMs: time.Since(start).Milliseconds(),
				RemoteIP:  c.RealIP(),
				UserAgent: c.Request().UserAgent(),
			}
			encoder.Encode(entry)

			return err
		}
	}
}
