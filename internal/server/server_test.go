// internal/server/server_test.go 驗證 BuildEcho 路由組裝及各端點的狀態碼回傳。
package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/controller"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// server_test.go verifies the Echo assembly: BuildEcho wires the ops routes
// (/health, /ready, /metrics) and business routes (/), and registerRoutes maps
// / to the controller's Home handler. The controller is built from fakes; the
// readiness probe uses a pingable sqlmock DB so /ready returns 200.

// fakePortfolio / fakeStatistic / fakeHistory satisfy the controller service
// interfaces with empty, error-free responses (the server test only checks
// routing/status, not payloads).
type fakePortfolio struct{}

func (fakePortfolio) UnrealizedGainsLosses(ctx context.Context) ([]dto.UnrealizedGainLoss, error) {
	return nil, nil
}

func (fakePortfolio) RealizedGainsLosses(ctx context.Context) ([]dto.RealizedGainLoss, error) {
	return nil, nil
}

type fakeStatistic struct{}

func (fakeStatistic) StockStatisticData(ctx context.Context) ([]dto.StockStatistic, error) {
	return nil, nil
}

type fakeHistory struct{}

func (fakeHistory) StockHistoryData(ctx context.Context, stockID string) ([]dto.StockHistoryPoint, error) {
	return nil, nil
}

type fakePerformance struct{}

func (fakePerformance) Summary(ctx context.Context) (dto.PerformanceSummary, error) {
	return dto.PerformanceSummary{}, nil
}

type fakeEquity struct{}

func (fakeEquity) EquityHistory(ctx context.Context) ([]dto.LiveEquityPoint, error) {
	return nil, nil
}

type fakePerfHistory struct{}

func (fakePerfHistory) History(ctx context.Context) ([]dto.PerformanceHistoryPoint, error) {
	return nil, nil
}

// newTestController builds a Controller from the fakes + a discard logger.
func newTestController() *controller.Controller {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return controller.New(log, fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}, fakeEquity{}, fakePerfHistory{})
}

// TestBuildEcho_OpsRoutes 驗證 BuildEcho 正確掛載 /health、/ready、/metrics 及業務路由，並回傳預期狀態碼。
func TestBuildEcho_OpsRoutes(t *testing.T) {
	// Arrange — pingable mock DB so /ready returns 200; controller from fakes.
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectPing()

	log := logrus.New()
	log.SetOutput(io.Discard)
	e := BuildEcho(log, db, newTestController())

	cases := []struct {
		path string
		want int
	}{
		{"/health", http.StatusOK},                      // liveness 永遠 200
		{"/ready", http.StatusOK},                       // mock DB ping 成功 → ready
		{"/metrics", http.StatusOK},                     // Prometheus 端點
		{"/", http.StatusOK},                            // 業務 home
		{"/api/get_equity_history", http.StatusOK},      // 實盤每日權益歷史
		{"/api/get_performance_history", http.StatusOK}, // 統一日期軸績效歷史
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			// Act
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
			// Assert
			if rec.Code != c.want {
				t.Fatalf("%s = %d, want %d", c.path, rec.Code, c.want)
			}
		})
	}
}

// TestRegisterRoutes_WiresHome 驗證 registerRoutes 將 "/" 對應至 Home handler 並回傳正確 body。
func TestRegisterRoutes_WiresHome(t *testing.T) {
	// Arrange
	e := echo.New()
	registerRoutes(e, newTestController())

	// Act — 透過 router 打 "/"。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK || rec.Body.String() != "Hello, World!" {
		t.Fatalf("routed home = (%d,%q)", rec.Code, rec.Body.String())
	}
}
