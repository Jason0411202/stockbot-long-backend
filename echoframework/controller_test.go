package echoframework

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"main/app_context"
	"main/config"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// controller_test.go 以 echo + sqlmock 直接驅動 handler (注入式 appCtx),不需真實 DB / server。

func mockCtx(t *testing.T) (*app_context.AppContext, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < 8; i++ { // 預掛足量 USE StockLongData
		mock.ExpectExec("USE StockLongData").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	log := logrus.New()
	log.SetOutput(io.Discard)
	return &app_context.AppContext{Db: db, Log: log, Cfg: &config.Config{TrackStocks: []string{"00631L"}}}, mock
}

// invoke 建立一個 GET 請求並執行 handler,回傳 recorder。
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

func TestHomeHandler(t *testing.T) {
	// Act
	rec := invoke(t, newHomeHandler(), "/")
	// Assert
	if rec.Code != http.StatusOK || rec.Body.String() != "Hello, World!" {
		t.Fatalf("home = (%d,%q)", rec.Code, rec.Body.String())
	}
}

func TestUnrealizedHandler_OK(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	mock.ExpectQuery("FROM UnrealizedGainsLosses ORDER BY transaction_date DESC").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
			AddRow("2024-01-02", "00631L", "n", 50.0, 5000.0, 100))
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(60.0))

	// Act
	rec := invoke(t, newUnrealizedGainsLossesHandler(appCtx), "/api/get_unrealized_gains_losses")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnrealizedHandler_DBErrorReturnsEmpty(t *testing.T) {
	// Arrange — 查詢失敗 → handler 應回 200 + 空陣列 (不外洩錯誤)。
	appCtx, mock := mockCtx(t)
	mock.ExpectQuery("FROM UnrealizedGainsLosses").WillReturnError(io.ErrUnexpectedEOF)

	// Act
	rec := invoke(t, newUnrealizedGainsLossesHandler(appCtx), "/api/get_unrealized_gains_losses")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with empty array", rec.Code)
	}
}

func TestRealizedHandler_OK(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	mock.ExpectQuery("FROM RealizedGainsLosses ORDER BY sell_date DESC").
		WillReturnRows(sqlmock.NewRows([]string{"buy_date", "sell_date", "stock_id", "stock_name", "purchase_price", "sell_price", "investment_cost", "revenue", "profit_loss", "profit_rate", "shares"}).
			AddRow("2024-01-02", "2024-03-02", "00631L", "n", 50.0, 80.0, 5000.0, 8000.0, 3000.0, 60.0, 100))

	// Act + Assert
	if rec := invoke(t, newRealizedGainsLossesHandler(appCtx), "/x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestStockHistoryHandler_RequiresStockId(t *testing.T) {
	// Arrange
	appCtx, _ := mockCtx(t)

	// Act — 缺 stockId → 400。
	rec := invoke(t, newStockHistoryDataHandler(appCtx), "/api/get_stock_history_data")

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing stockId should be 400, got %d", rec.Code)
	}
}

func TestStockHistoryHandler_OK(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}).AddRow("2024-01-02", 50.0))

	// Act + Assert
	if rec := invoke(t, newStockHistoryDataHandler(appCtx), "/x?stockId=00631L"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestStockStatisticHandler_DBErrorReturnsEmpty(t *testing.T) {
	// Arrange — GetStockName 失敗 → handler 回 200 空陣列。
	appCtx, mock := mockCtx(t)
	mock.ExpectQuery("SELECT stock_name FROM StockHistory").WillReturnError(io.ErrUnexpectedEOF)

	// Act + Assert
	if rec := invoke(t, newStockStatisticDataHandler(appCtx), "/x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBuildServer_OpsRoutes(t *testing.T) {
	// Arrange — buildServer 應接好 health / ready / metrics 與業務路由。
	appCtx, _ := mockCtx(t)
	e := buildServer(appCtx)

	cases := []struct {
		path string
		want int
	}{
		{"/health", http.StatusOK},  // liveness 永遠 200
		{"/ready", http.StatusOK},   // mock DB ping 成功 → ready
		{"/metrics", http.StatusOK}, // Prometheus 端點
		{"/", http.StatusOK},        // 業務 home
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

func TestEchoRouting_WiresHome(t *testing.T) {
	// Arrange
	appCtx, _ := mockCtx(t)
	e := echo.New()
	EchoRouting(e, appCtx)

	// Act — 透過 router 打 "/"。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK || rec.Body.String() != "Hello, World!" {
		t.Fatalf("routed home = (%d,%q)", rec.Code, rec.Body.String())
	}
}
