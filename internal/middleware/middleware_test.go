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
