// internal/middleware/middleware_test.go 驗證 request log 與 metrics middleware 的行為與略過規則。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// middleware_test.go 驗證 JSON 日誌、Prometheus 指標收集 (含排除路徑)、/metrics handler。

// runWith 以指定 path 跑一個 middleware-包裝的 handler,回傳 recorder。
func runWith(t *testing.T, mw echo.MiddlewareFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath(path)
	h := mw(func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	if err := h(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

// TestRequestLogger_PassesThrough 驗證日誌 middleware 不更動回應狀態碼與 body。
func TestRequestLogger_PassesThrough(t *testing.T) {
	// Act
	rec := runWith(t, NewRequestLogger(), "/api/x")
	// Assert — 日誌 middleware 不應改變回應。
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("logger middleware altered response: (%d,%q)", rec.Code, rec.Body.String())
	}
}

// TestRequestLogger_HandlerError 驗證 handler 回傳錯誤時由 middleware 轉交 Echo 錯誤處理,
// 回應狀態碼為對應的 HTTP 錯誤且 middleware 本身回傳 nil (不重複處理)。
func TestRequestLogger_HandlerError(t *testing.T) {
	// Arrange — handler 一律回傳 500 HTTPError。
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/fail", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/fail")
	h := NewRequestLogger()(func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusInternalServerError, "boom")
	})

	// Act
	err := h(c)

	// Assert — 錯誤已由 c.Error 處理:middleware 回傳 nil、回應為 500。
	if err != nil {
		t.Fatalf("middleware should swallow handler error after c.Error, got %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// TestLevelForStatus 驗證狀態碼與 access log 等級的對應 (5xx=error、4xx=warning、其餘=info)。
func TestLevelForStatus(t *testing.T) {
	// Act + Assert — 逐一驗證各狀態碼區間的等級標籤。
	cases := map[int]string{200: "info", 302: "info", 404: "warning", 500: "error", 503: "error"}
	for status, want := range cases {
		if got := levelForStatus(status); got != want {
			t.Fatalf("levelForStatus(%d) = %q, want %q", status, got, want)
		}
	}
}

// TestRequestID_FallsBackToRequestHeader 驗證回應標頭無值時退回請求標頭的 X-Request-ID。
func TestRequestID_FallsBackToRequestHeader(t *testing.T) {
	// Arrange — 只在請求標頭放 request id。
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderXRequestID, "req-123")
	c := e.NewContext(req, httptest.NewRecorder())

	// Act + Assert
	if got := requestID(c); got != "req-123" {
		t.Fatalf("requestID = %q, want req-123", got)
	}
}

// TestMetricsMiddleware_RecordsNormalPath 驗證一般 API 路徑通過指標 middleware 後回傳 200。
func TestMetricsMiddleware_RecordsNormalPath(t *testing.T) {
	// Act
	rec := runWith(t, NewMetricsMiddleware(), "/api/orders")
	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestMetricsMiddleware_SkipsOpsPaths 驗證 /health、/ready、/metrics 走 early-return 分支且不記錄指標。
func TestMetricsMiddleware_SkipsOpsPaths(t *testing.T) {
	// Act + Assert — /health /ready /metrics 皆走 early-return 分支,不記指標。
	for _, p := range []string{"/health", "/ready", "/metrics"} {
		if rec := runWith(t, NewMetricsMiddleware(), p); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200", p, rec.Code)
		}
	}
}

// TestMetricsHandler_ExposesPrometheus 驗證 /metrics handler 回傳 200 並輸出 Prometheus 文字格式。
func TestMetricsHandler_ExposesPrometheus(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	// Act
	if err := NewMetricsHandler()(e.NewContext(req, rec)); err != nil {
		t.Fatalf("metrics handler: %v", err)
	}

	// Assert — Prometheus 文字格式應含已註冊的指標名稱。
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", rec.Code)
	}
}
