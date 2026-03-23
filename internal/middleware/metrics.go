package middleware

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HttpRequestsTotal 計算 HTTP request 總數（只會往上加的 Counter）
	// labels 讓你在 Grafana 可以過濾：只看 POST、只看 5xx 等
	HttpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// HttpRequestDuration 用來算 p50/p95/p99 延遲的分布統計
	HttpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "path"},
	)
)

// NewMetricsMiddleware 回傳 Prometheus metrics 收集用的 Echo middleware
// 排除 /metrics、/health、/ready 避免噪音
func NewMetricsMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Path()
			if path == "/metrics" || path == "/health" || path == "/ready" {
				return next(c)
			}

			start := time.Now()
			err := next(c)
			duration := time.Since(start).Seconds()
			status := strconv.Itoa(c.Response().Status)

			HttpRequestsTotal.WithLabelValues(c.Request().Method, path, status).Inc()
			HttpRequestDuration.WithLabelValues(c.Request().Method, path).Observe(duration)

			return err
		}
	}
}

// NewMetricsHandler 回傳 /metrics endpoint 的 handler
// Prometheus 每 15 秒來 GET /metrics 拉取所有指標
func NewMetricsHandler() echo.HandlerFunc {
	return echo.WrapHandler(promhttp.Handler())
}
