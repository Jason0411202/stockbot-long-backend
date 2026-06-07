// internal/client/twse/realtime_test.go 以 httptest 驗證 MIS 即時報價的解析與當日開盤過濾。
package twse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// misSampleJSON 模擬 MIS getStockInfo.jsp 回應:兩檔股票,各含 c/n/o/d 欄位。
// 00631L 當日已有開盤價 19.50;00830 開盤尚未成交 (o="-")。
const misSampleJSON = `{"msgArray":[` +
	`{"c":"00631L","n":"元大台灣50正2","o":"19.50","z":"19.60","y":"19.40","d":"20260608"},` +
	`{"c":"00830","n":"國泰費城半導體","o":"-","z":"-","y":"45.00","d":"20260608"}` +
	`],"rtcode":"0000","rtmessage":"OK"}`

// fixedNow 回傳固定「今天」(2026-06-08 09:05 台北),供測試當日過濾。
func fixedNow() time.Time {
	loc, _ := time.LoadLocation("Asia/Taipei")
	return time.Date(2026, 6, 8, 9, 5, 0, 0, loc)
}

// newMISServer 起一個回傳指定 body 的 httptest 伺服器 (模擬 MIS 端點)。
func newMISServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFetchQuotes_ParsesMsgArray 驗證 FetchQuotes 正確解析 msgArray 的代號 / 名稱 / 開盤 / 日期。
func TestFetchQuotes_ParsesMsgArray(t *testing.T) {
	srv := newMISServer(t, misSampleJSON)
	c := NewRealtimeClient(WithRealtimeBaseURL(srv.URL))

	quotes, err := c.FetchQuotes(context.Background(), []string{"00631L", "00830"})
	if err != nil {
		t.Fatalf("FetchQuotes: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("len(quotes) = %d, want 2", len(quotes))
	}
	q := quotes["00631L"]
	if q.Name != "元大台灣50正2" || q.Open != 19.50 || q.Date != "20260608" {
		t.Fatalf("00631L quote = %+v", q)
	}
	// 尚未成交的 o="-" 解析為 0。
	if quotes["00830"].Open != 0 {
		t.Fatalf("00830 open = %v, want 0 (尚未成交)", quotes["00830"].Open)
	}
}

// TestFetchOpens_OnlyTodayWithPrice 驗證 FetchOpens 僅回傳「當日且開盤>0」的股票。
func TestFetchOpens_OnlyTodayWithPrice(t *testing.T) {
	srv := newMISServer(t, misSampleJSON)
	c := NewRealtimeClient(WithRealtimeBaseURL(srv.URL), WithRealtimeNow(fixedNow))

	opens, err := c.FetchOpens(context.Background(), []string{"00631L", "00830"})
	if err != nil {
		t.Fatalf("FetchOpens: %v", err)
	}
	// 00631L 當日有開盤 → 納入;00830 尚未成交 (open=0) → 排除。
	if len(opens) != 1 || opens["00631L"] != 19.50 {
		t.Fatalf("opens = %+v, want only 00631L=19.50", opens)
	}
	if _, ok := opens["00830"]; ok {
		t.Fatalf("00830 未成交不應納入 opens")
	}
}

// TestFetchOpens_StaleDateExcluded 驗證資料停在前一交易日 (date != 今日) 時整批視為未就緒。
func TestFetchOpens_StaleDateExcluded(t *testing.T) {
	// 資料日期為 20260605 (上週五),但「今天」為 20260608 → 應全部排除。
	const staleJSON = `{"msgArray":[{"c":"00631L","n":"元大台灣50正2","o":"19.50","d":"20260605"}],"rtcode":"0000"}`
	srv := newMISServer(t, staleJSON)
	c := NewRealtimeClient(WithRealtimeBaseURL(srv.URL), WithRealtimeNow(fixedNow))

	opens, err := c.FetchOpens(context.Background(), []string{"00631L"})
	if err != nil {
		t.Fatalf("FetchOpens: %v", err)
	}
	if len(opens) != 0 {
		t.Fatalf("stale (非當日) 資料應被排除, got %+v", opens)
	}
}

// TestFetchOpens_HTTPError 驗證 HTTP 500 時回傳錯誤。
func TestFetchOpens_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := NewRealtimeClient(WithRealtimeBaseURL(srv.URL), WithRealtimeNow(fixedNow))

	if _, err := c.FetchOpens(context.Background(), []string{"00631L"}); err == nil {
		t.Fatalf("expected error on HTTP 500")
	}
}

// TestNewRealtimeClient_Defaults 驗證預設端點與 timeout。
func TestNewRealtimeClient_Defaults(t *testing.T) {
	c := NewRealtimeClient()
	if c.baseURL != defaultMISBaseURL {
		t.Fatalf("default baseURL = %q, want %q", c.baseURL, defaultMISBaseURL)
	}
	if c.httpClient == nil || c.httpClient.Timeout != 30*time.Second {
		t.Fatalf("default timeout not 30s")
	}
}
