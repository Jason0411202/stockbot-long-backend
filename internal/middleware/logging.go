// internal/middleware/logging.go 提供 Echo request 結構化日誌 middleware。
package middleware

import (
	"encoding/json"
	"os"
	"time"

	"github.com/labstack/echo/v4"
)

// requestLog 定義 JSON access log 的結構,每個 HTTP request 輸出一行,
// 由 log 收集器 (Alloy / Fluent Bit) 從 stdout 讀取後送往 VictoriaLogs / Elasticsearch。
type requestLog struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Component string `json:"component"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Query     string `json:"query,omitempty"`
	Status    int    `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	BytesOut  int64  `json:"bytes_out"`
	RemoteIP  string `json:"remote_ip"`
	UserAgent string `json:"user_agent"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// NewRequestLogger 回傳 JSON 結構化 access log 的 Echo middleware:
// 每個 HTTP request 輸出一行 JSON 到 stdout,附帶 request_id (與 RequestID middleware 搭配)
// 與 handler 錯誤訊息;5xx 標記為 error 等級、4xx 為 warn、其餘為 info。
func NewRequestLogger() echo.MiddlewareFunc {
	// 建立共用的 JSON encoder,寫入 stdout 供 log 收集器讀取。
	encoder := json.NewEncoder(os.Stdout)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 記錄請求開始時間,用於計算延遲。
			start := time.Now()

			// 先執行後續 handler,再收集回應狀態、錯誤與延遲。
			err := next(c)

			// 有錯誤時先交由 Echo 錯誤處理器產生最終回應,使本行 log 記到真實狀態碼。
			if err != nil {
				c.Error(err)
			}

			// 組裝結構化日誌並以 JSON 格式寫出至 stdout。
			status := c.Response().Status
			entry := requestLog{
				Timestamp: start.Format(time.RFC3339),
				Level:     levelForStatus(status),
				Component: "http_access",
				Method:    c.Request().Method,
				Path:      c.Request().URL.Path,
				Query:     c.Request().URL.RawQuery,
				Status:    status,
				LatencyMs: time.Since(start).Milliseconds(),
				BytesOut:  c.Response().Size,
				RemoteIP:  c.RealIP(),
				UserAgent: c.Request().UserAgent(),
				RequestID: requestID(c),
			}
			if err != nil {
				entry.Error = err.Error()
			}
			encoder.Encode(entry)

			// 錯誤已交由 c.Error 處理,回傳 nil 避免 Echo 重複處理。
			return nil
		}
	}
}

// levelForStatus 依 HTTP 狀態碼對應 log 等級:5xx 為 error、4xx 為 warn、其餘為 info。
func levelForStatus(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warning"
	default:
		return "info"
	}
}

// requestID 取出本次請求的 request id (由 Echo RequestID middleware 產生,
// 優先取回應標頭,回應未設時退回請求標頭;皆無時回傳空字串由 omitempty 略去)。
func requestID(c echo.Context) string {
	if id := c.Response().Header().Get(echo.HeaderXRequestID); id != "" {
		return id
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}
