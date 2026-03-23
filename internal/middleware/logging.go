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
	encoder := json.NewEncoder(os.Stdout)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

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
