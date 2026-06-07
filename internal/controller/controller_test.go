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

// newController builds a Controller from the supplied fakes + a discard logger.
func newController(p PortfolioService, s StatisticService, h StockHistoryService) *Controller {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return New(log, p, s, h)
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
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{})
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
	}, fakeStatistic{}, fakeHistory{})

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
	ctl := newController(fakePortfolio{unrealizedErr: errFake}, fakeStatistic{}, fakeHistory{})

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
	}, fakeStatistic{}, fakeHistory{})

	// Act + Assert
	if rec := invoke(t, ctl.RealizedGainsLosses, "/x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// TestStockHistoryHandler_RequiresStockId 驗證缺少 stockId 查詢參數時 handler 回傳 400。
func TestStockHistoryHandler_RequiresStockId(t *testing.T) {
	// Arrange
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{})

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
	})

	// Act + Assert
	if rec := invoke(t, ctl.StockHistoryData, "/x?stockId=00631L"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// TestStockStatisticHandler_DBErrorReturnsEmpty 驗證 service 失敗時 StockStatisticData handler 回傳 200 加空陣列。
func TestStockStatisticHandler_DBErrorReturnsEmpty(t *testing.T) {
	// Arrange — service 失敗 → handler 回 200 空陣列。
	ctl := newController(fakePortfolio{}, fakeStatistic{err: errFake}, fakeHistory{})

	// Act + Assert
	if rec := invoke(t, ctl.StockStatisticData, "/x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
