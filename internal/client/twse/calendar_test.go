package twse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCalendar_HolidaysAndTradingDayAnnotations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("date") == "" {
			t.Error("TWSE year must use date=YYYY0101")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"stat":"ok","queryYear":2026,"data":[["2026-09-25","中秋節",""],["2026-09-28","教師節",""],["2026-02-11","農曆春節前最後交易日",""]]}`))
	}))
	defer srv.Close()
	c := NewCalendar()
	c.url = srv.URL
	d := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	prev, err := c.PreviousTradingDay(context.Background(), d)
	if err != nil || prev.Format("2006-01-02") != "2026-09-24" {
		t.Fatalf("previous=%v err=%v", prev, err)
	}
	open, err := c.IsTradingDay(context.Background(), time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC))
	if err != nil || !open {
		t.Fatal("last trading day treated as holiday")
	}
	if _, err = c.IsTradingDay(context.Background(), time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("wrong calendar year accepted")
	}
}
