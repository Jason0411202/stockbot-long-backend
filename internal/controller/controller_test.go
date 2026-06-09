// internal/controller/controller_test.go 以 fake service 直接驅動各 handler 方法，驗證狀態碼契約。
package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// controller_test.go drives each handler method directly with echo + fake
// services (no DB, no sqlmock). It asserts the exact status-code contract of
// the original echoframework handlers: 200 on success, 200 + empty on service
// error (swallowed), and 400 on a missing stockId.

// errFake is a sentinel returned by the fakes when an error path is requested.
var errFake = io.ErrUnexpectedEOF

// fakePortfolio implements PortfolioService with canned rows or errors.
type fakePortfolio struct {
	unrealized    []dto.UnrealizedGainLoss
	unrealizedErr error
	realized      []dto.RealizedGainLoss
	realizedErr   error
}

func (f fakePortfolio) UnrealizedGainsLosses(ctx context.Context) ([]dto.UnrealizedGainLoss, error) {
	return f.unrealized, f.unrealizedErr
}

func (f fakePortfolio) RealizedGainsLosses(ctx context.Context) ([]dto.RealizedGainLoss, error) {
	return f.realized, f.realizedErr
}

// fakeStatistic implements StatisticService with canned rows or an error.
type fakeStatistic struct {
	rows []dto.StockStatistic
	err  error
}

func (f fakeStatistic) StockStatisticData(ctx context.Context) ([]dto.StockStatistic, error) {
	return f.rows, f.err
}

// fakeHistory implements StockHistoryService with canned rows or an error.
type fakeHistory struct {
	rows []dto.StockHistoryPoint
	err  error
}

func (f fakeHistory) StockHistoryData(ctx context.Context, stockID string) ([]dto.StockHistoryPoint, error) {
	return f.rows, f.err
}

// fakePerformance implements PerformanceReporter with a canned summary or error.
type fakePerformance struct {
	summary dto.PerformanceSummary
	err     error
}

func (f fakePerformance) Summary(ctx context.Context) (dto.PerformanceSummary, error) {
	return f.summary, f.err
}

// fakeEquity implements EquityHistoryReporter with canned rows or an error.
type fakeEquity struct {
	rows []dto.LiveEquityPoint
	err  error
}

func (f fakeEquity) EquityHistory(ctx context.Context) ([]dto.LiveEquityPoint, error) {
	return f.rows, f.err
}

// fakePerfHistory implements PerformanceHistoryReporter with canned rows or an error.
type fakePerfHistory struct {
	rows []dto.PerformanceHistoryPoint
	err  error
}

func (f fakePerfHistory) History(ctx context.Context) ([]dto.PerformanceHistoryPoint, error) {
	return f.rows, f.err
}

// newController builds a Controller from the supplied fakes + a discard logger.
// The equity / perf-history reporters default to empty fakes; their dedicated
// tests build the Controller directly via New to inject canned rows / errors.
func newController(p PortfolioService, s StatisticService, h StockHistoryService, perf PerformanceReporter) *Controller {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return New(log, p, s, h, perf, fakeEquity{}, fakePerfHistory{})
}

// invoke builds a GET request and runs the handler method, returning the recorder.
func invoke(t *testing.T, h echo.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	if err := h(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

// TestHomeHandler 驗證 Home handler 回傳 200 且 body 為 "Hello, World!"。
func TestHomeHandler(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{})
	// Act
	rec := invoke(t, ctl.Home, "/")
	// Assert
	if rec.Code != http.StatusOK || rec.Body.String() != "Hello, World!" {
		t.Fatalf("home = (%d,%q)", rec.Code, rec.Body.String())
	}
}

// TestUnrealizedHandler_OK 驗證 UnrealizedGainsLosses handler 在 service 正常時回傳 200。
func TestUnrealizedHandler_OK(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{
		unrealized: []dto.UnrealizedGainLoss{{StockID: "00631L", StockName: "n"}},
	}, fakeStatistic{}, fakeHistory{}, fakePerformance{})

	// Act
	rec := invoke(t, ctl.UnrealizedGainsLosses, "/api/get_unrealized_gains_losses")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestUnrealizedHandler_DBErrorReturnsEmpty 驗證 service 失敗時 handler 回傳 200 加空陣列，不外洩錯誤。
func TestUnrealizedHandler_DBErrorReturnsEmpty(t *testing.T) {
	// Arrange — service 失敗 → handler 應回 200 + 空陣列 (不外洩錯誤)。
	ctl := newController(fakePortfolio{unrealizedErr: errFake}, fakeStatistic{}, fakeHistory{}, fakePerformance{})

	// Act
	rec := invoke(t, ctl.UnrealizedGainsLosses, "/api/get_unrealized_gains_losses")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with empty array", rec.Code)
	}
}

// TestRealizedHandler_OK 驗證 RealizedGainsLosses handler 在 service 正常時回傳 200。
func TestRealizedHandler_OK(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{
		realized: []dto.RealizedGainLoss{{StockID: "00631L", StockName: "n"}},
	}, fakeStatistic{}, fakeHistory{}, fakePerformance{})

	// Act + Assert
	if rec := invoke(t, ctl.RealizedGainsLosses, "/x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// TestStockHistoryHandler_RequiresStockId 驗證缺少 stockId 查詢參數時 handler 回傳 400。
func TestStockHistoryHandler_RequiresStockId(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{})

	// Act — 缺 stockId → 400。
	rec := invoke(t, ctl.StockHistoryData, "/api/get_stock_history_data")

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing stockId should be 400, got %d", rec.Code)
	}
}

// TestStockHistoryHandler_OK 驗證提供有效 stockId 時 handler 回傳 200。
func TestStockHistoryHandler_OK(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{
		rows: []dto.StockHistoryPoint{{Date: "2024-01-02", Price: 50.0}},
	}, fakePerformance{})

	// Act + Assert
	if rec := invoke(t, ctl.StockHistoryData, "/x?stockId=00631L"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// TestStockStatisticHandler_DBErrorReturnsEmpty 驗證 service 失敗時 StockStatisticData handler 回傳 200 加空陣列。
func TestStockStatisticHandler_DBErrorReturnsEmpty(t *testing.T) {
	// Arrange — service 失敗 → handler 回 200 空陣列。
	ctl := newController(fakePortfolio{}, fakeStatistic{err: errFake}, fakeHistory{}, fakePerformance{})

	// Act + Assert
	if rec := invoke(t, ctl.StockStatisticData, "/x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// TestPerformanceHandler_OK 驗證 PerformanceSummary handler 在 service 正常時回傳 200。
func TestPerformanceHandler_OK(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{},
		fakePerformance{summary: dto.PerformanceSummary{InitialCash: 100000}})

	// Act + Assert
	if rec := invoke(t, ctl.PerformanceSummary, "/api/get_performance_summary"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestPerformanceHandler_ErrorReturnsEmpty 驗證 service 失敗時 handler 回傳 200 加空物件，不外洩錯誤。
func TestPerformanceHandler_ErrorReturnsEmpty(t *testing.T) {
	// Arrange — service 失敗 → handler 應回 200 + 空物件。
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{err: errFake})

	// Act + Assert
	if rec := invoke(t, ctl.PerformanceSummary, "/api/get_performance_summary"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with empty object", rec.Code)
	}
}

// newControllerWithEquity builds a Controller with a specific equity reporter (empty fakes elsewhere).
func newControllerWithEquity(eq EquityHistoryReporter) *Controller {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return New(log, fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}, eq, fakePerfHistory{})
}

// newControllerWithPerfHistory builds a Controller with a specific perf-history reporter (empty fakes elsewhere).
func newControllerWithPerfHistory(ph PerformanceHistoryReporter) *Controller {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return New(log, fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}, fakeEquity{}, ph)
}

// TestEquityHistoryHandler_OK 驗證 EquityHistory handler 在 service 正常時回傳 200。
func TestEquityHistoryHandler_OK(t *testing.T) {
	// Arrange
	ctl := newControllerWithEquity(fakeEquity{rows: []dto.LiveEquityPoint{{Date: "2024-01-02", TotalEquity: 1000}}})

	// Act + Assert
	if rec := invoke(t, ctl.EquityHistory, "/api/get_equity_history"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestEquityHistoryHandler_ErrorReturnsEmpty 驗證 service 失敗時 handler 回傳 200 加空陣列，不外洩錯誤。
func TestEquityHistoryHandler_ErrorReturnsEmpty(t *testing.T) {
	// Arrange — service 失敗 → handler 應回 200 + 空陣列。
	ctl := newControllerWithEquity(fakeEquity{err: errFake})

	// Act + Assert
	if rec := invoke(t, ctl.EquityHistory, "/api/get_equity_history"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with empty array", rec.Code)
	}
}

// TestPerformanceHistoryHandler_OK 驗證 PerformanceHistory handler 在 service 正常時回傳 200。
func TestPerformanceHistoryHandler_OK(t *testing.T) {
	// Arrange
	ctl := newControllerWithPerfHistory(fakePerfHistory{rows: []dto.PerformanceHistoryPoint{{Date: "2024-01-02", StratEquity: 1000}}})

	// Act + Assert
	if rec := invoke(t, ctl.PerformanceHistory, "/api/get_performance_history"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestPerformanceHistoryHandler_ErrorReturnsEmpty 驗證 service 失敗時 handler 回傳 200 加空陣列，不外洩錯誤。
func TestPerformanceHistoryHandler_ErrorReturnsEmpty(t *testing.T) {
	// Arrange — service 失敗 → handler 應回 200 + 空陣列。
	ctl := newControllerWithPerfHistory(fakePerfHistory{err: errFake})

	// Act + Assert
	if rec := invoke(t, ctl.PerformanceHistory, "/api/get_performance_history"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with empty array", rec.Code)
	}
}
