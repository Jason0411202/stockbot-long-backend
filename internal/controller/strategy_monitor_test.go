// internal/controller/strategy_monitor_test.go 驗證決策快照 HTTP 傳輸、請求脈絡與錯誤契約。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// strategyMonitorFunc 將測試函式轉成監控服務，便於驗證呼叫次數與請求脈絡。
type strategyMonitorFunc func(context.Context) (dto.StrategyMonitor, error)

// StrategyMonitor 委派至測試函式，回傳預設快照或錯誤。
func (f strategyMonitorFunc) StrategyMonitor(ctx context.Context) (dto.StrategyMonitor, error) {
	return f(ctx)
}

// TestStrategyMonitor_PreservesSnapshotAndRequestContext 驗證 handler 完整保留快照並傳遞原始請求脈絡。
func TestStrategyMonitor_PreservesSnapshotAndRequestContext(t *testing.T) {
	drawdown, threshold := 3.0, 10.0
	want := dto.StrategyMonitor{
		SchemaVersion: 1,
		Availability:  "available",
		DecisionDate:  "2026-10-13",
		Stocks: []dto.StrategyMonitorStock{{
			StockID: "00631L",
			Status:  "evaluated",
			Buy:     dto.StrategyDecision{Status: "blocked"},
			Sell: dto.StrategyDecision{
				Status: "blocked",
				Conditions: []dto.StrategyCondition{{
					ID: "trailing_drawdown", Status: "blocked", Value: &drawdown,
					Threshold: &threshold, Unit: "percent", Operator: "gte",
				}},
			},
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}).
		WithStrategyMonitor(strategyMonitorFunc(func(gotCtx context.Context) (dto.StrategyMonitor, error) {
			calls++
			if gotCtx != ctx {
				t.Error("service did not receive the request context")
			}
			return want, nil
		}))

	req := httptest.NewRequest(http.MethodGet, "/api/get_strategy_monitor", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	if err := ctl.StrategyMonitor(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d, service calls = %d; want 200 and 1", rec.Code, calls)
	}
	var got dto.StrategyMonitor
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot changed in transport: got %#v, want %#v", got, want)
	}
}

// TestStrategyMonitor_Unavailable 驗證服務缺失與儲存錯誤皆回傳固定 503 錯誤且不洩漏內部細節。
func TestStrategyMonitor_Unavailable(t *testing.T) {
	cases := []struct {
		name     string
		reporter StrategyMonitorReporter
	}{
		{name: "missing dependency"},
		{
			name: "storage error",
			reporter: strategyMonitorFunc(func(context.Context) (dto.StrategyMonitor, error) {
				return dto.StrategyMonitor{Availability: "available"}, errors.New("private database details")
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}).
				WithStrategyMonitor(tc.reporter)
			rec := invoke(t, ctl.StrategyMonitor, "/api/get_strategy_monitor")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if len(got) != 1 || got["error"] != "strategy_monitor_unavailable" {
				t.Fatalf("error response = %#v", got)
			}
		})
	}
}
