package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
)

// health_test.go 驗證 K8s liveness / readiness probe。

func invoke(t *testing.T, h echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if err := h(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

func TestLivenessHandler_AlwaysOK(t *testing.T) {
	// Act + Assert — liveness 不檢查外部依賴,恆 200。
	if rec := invoke(t, NewLivenessHandler()); rec.Code != http.StatusOK {
		t.Fatalf("liveness = %d, want 200", rec.Code)
	}
}

func TestReadinessHandler_NilDB(t *testing.T) {
	// Act + Assert — 無 DB → 503。
	if rec := invoke(t, NewReadinessHandler(nil)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness(nil db) = %d, want 503", rec.Code)
	}
}

func TestReadinessHandler_HealthyDB(t *testing.T) {
	// Arrange — ping 成功的 mock DB。
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectPing()

	// Act + Assert
	if rec := invoke(t, NewReadinessHandler(db)); rec.Code != http.StatusOK {
		t.Fatalf("readiness(healthy) = %d, want 200", rec.Code)
	}
}

func TestReadinessHandler_UnreachableDB(t *testing.T) {
	// Arrange — ping 失敗的 mock DB。
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectPing().WillReturnError(http.ErrServerClosed)

	// Act + Assert
	if rec := invoke(t, NewReadinessHandler(db)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness(unreachable) = %d, want 503", rec.Code)
	}
}
