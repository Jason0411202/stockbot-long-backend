// internal/server/strategy_monitor_test.go 驗證決策監控路由、共用 middleware 與未注入服務的回應。
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// fakeStrategyMonitor 提供固定決策快照供 HTTP 路由組裝測試使用。
type fakeStrategyMonitor struct{}

// StrategyMonitor 回傳固定日期的可用快照，不存取資料庫。
func (fakeStrategyMonitor) StrategyMonitor(context.Context) (dto.StrategyMonitor, error) {
	return dto.StrategyMonitor{SchemaVersion: 1, Availability: "available", DecisionDate: "2026-10-13", Stocks: []dto.StrategyMonitorStock{}}, nil
}

// TestBuildEcho_StrategyMonitorRoute 驗證路由回傳服務快照且經過請求識別 middleware。
func TestBuildEcho_StrategyMonitorRoute(t *testing.T) {
	log := logrus.New()
	log.SetOutput(io.Discard)
	ctl := newTestController().WithStrategyMonitor(fakeStrategyMonitor{})
	e := BuildEcho(log, nil, ctl)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/get_strategy_monitor", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var got dto.StrategyMonitor
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if got.Availability != "available" || got.DecisionDate != "2026-10-13" {
		t.Fatalf("routed snapshot = %#v", got)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("strategy monitor route bypassed request middleware")
	}
}

// TestBuildEcho_StrategyMonitorWithoutReporter 驗證未注入監控服務時仍有路由並回傳 503。
func TestBuildEcho_StrategyMonitorWithoutReporter(t *testing.T) {
	log := logrus.New()
	log.SetOutput(io.Discard)
	e := BuildEcho(log, nil, newTestController())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/get_strategy_monitor", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
}
