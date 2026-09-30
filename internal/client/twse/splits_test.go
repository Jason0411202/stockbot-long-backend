package twse

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSplitClientUsesOfficialRatioAndTradingSuspension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/TWTCAU" {
			if r.URL.Query().Get("startDate") != "20100101" || r.URL.Query().Get("endDate") == "" {
				t.Error("missing historical range")
			}
			w.Write([]byte(`{"stat":"OK","data":[["115/03/31","00631L","ETF","","443.15","20.14"],["114/02/19","00676R","ETF","反分割","2.04","12.23"]]}`))
		} else {
			w.Write([]byte(`{"stat":"OK","data":[["115/11/11","00662","ETF","分割","115/11/17","5.00000000"]]}`))
		}
	}))
	defer srv.Close()
	c := NewSplitClient()
	c.BaseURL = srv.URL + "/"
	a, err := c.FetchSplits(context.Background())
	if err != nil || len(a) != 3 {
		t.Fatalf("%+v %v", a, err)
	}
	if a[0].Ratio != 22 || math.Abs(a[1].Ratio-1.0/6) > 1e-12 || a[2].SuspendFrom != "2026-11-11" || a[2].Date != "2026-11-17" || a[2].Ratio != 5 {
		t.Fatalf("wrong action: %+v", a)
	}
}

func TestSplitClientRejectsBadReferenceAndHandlesEmptyAnnouncements(t *testing.T) {
	if _, err := referenceRatio(100, 31); err == nil {
		t.Fatal("actual price move inferred as split")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"stat":"很抱歉，沒有符合條件的資料!"}`))
	}))
	defer srv.Close()
	c := NewSplitClient()
	c.BaseURL = srv.URL + "/"
	a, err := c.FetchSplits(context.Background())
	if err != nil || len(a) != 0 {
		t.Fatalf("empty: %v %v", a, err)
	}
}
