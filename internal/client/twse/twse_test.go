package twse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// twseSampleJSON 重用 sqls_test.go 的 STOCK_DAY 回傳形狀:
// title 為 "113年01月 00631L 元大台灣50正2 日成交資訊",data 兩列含千分位逗號。
// 欄位序: [0]date [1]volume [2]value [3]open [4]high [5]low [6]close [7]change [8]transactions
const twseSampleJSON = `{"stat":"OK","title":"113年01月 00631L 元大台灣50正2 日成交資訊","data":[["113/01/02","1,000","50,000","50.00","51.00","49.00","50.50","+0.50","100"],["113/01/03","2,000","60,000","50.50","52.00","50.00","51.50","+1.00","120"]]}`

// newTestServer 起一個回傳指定 body 的 httptest 伺服器。
func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchMonth_ParsesTypedBarsAscending(t *testing.T) {
	// Arrange — 以 httptest 取代真實 TWSE 端點。
	srv := newTestServer(t, twseSampleJSON)
	client := NewClient(WithBaseURL(srv.URL))

	// Act
	bars, name, err := client.FetchMonth("20240101", "00631L")

	// Assert
	if err != nil {
		t.Fatalf("FetchMonth: %v", err)
	}
	if name != "元大台灣50正2" {
		t.Fatalf("stockName = %q, want 元大台灣50正2", name)
	}
	if len(bars) != 2 {
		t.Fatalf("len(bars) = %d, want 2", len(bars))
	}

	// 升冪 (舊→新):第一筆為 01/02,第二筆為 01/03。
	first := bars[0]
	if first.Date != "2024-01-02" {
		t.Fatalf("bars[0].Date = %q, want 2024-01-02", first.Date)
	}
	if first.Open != 50.00 || first.High != 51.00 || first.Low != 49.00 || first.Close != 50.50 {
		t.Fatalf("bars[0] OHLC = (%v,%v,%v,%v), want (50,51,49,50.5)",
			first.Open, first.High, first.Low, first.Close)
	}
	if first.Volume != 1000 { // 千分位逗號應被去除
		t.Fatalf("bars[0].Volume = %v, want 1000", first.Volume)
	}

	second := bars[1]
	if second.Date != "2024-01-03" {
		t.Fatalf("bars[1].Date = %q, want 2024-01-03", second.Date)
	}
	if second.Close != 51.50 || second.Volume != 2000 {
		t.Fatalf("bars[1] close/volume = (%v,%v), want (51.5,2000)", second.Close, second.Volume)
	}
}

func TestFetchMonth_StatNotOK(t *testing.T) {
	// Arrange — TWSE 對無資料月份回 stat != "OK"。
	srv := newTestServer(t, `{"stat":"很抱歉，沒有符合條件的資料!"}`)
	client := NewClient(WithBaseURL(srv.URL))

	// Act + Assert
	if _, _, err := client.FetchMonth("20240101", "00631L"); err == nil {
		t.Fatalf("expected error when stat != OK")
	}
}

func TestFetchMonth_MissingDataKey(t *testing.T) {
	// Arrange — stat OK 但缺 data key。
	srv := newTestServer(t, `{"stat":"OK","title":"x"}`)
	client := NewClient(WithBaseURL(srv.URL))

	// Act + Assert
	if _, _, err := client.FetchMonth("20240101", "00631L"); err == nil {
		t.Fatalf("expected error when data key missing")
	}
}

func TestFetchMonth_HTTPError(t *testing.T) {
	// Arrange — 伺服器回 500。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	client := NewClient(WithBaseURL(srv.URL))

	// Act + Assert
	if _, _, err := client.FetchMonth("20240101", "00631L"); err == nil {
		t.Fatalf("expected error on HTTP 500")
	}
}

func TestFetchMonth_SkipsInvalidCloseAndShortRows(t *testing.T) {
	// Arrange — 三列:正常、收盤 "--" (停牌)、欄位不足;應只保留正常的一列。
	const body = `{"stat":"OK","title":"113年01月 00631L 元大台灣50正2 日成交資訊","data":[` +
		`["113/01/02","1,000","50,000","50.00","51.00","49.00","50.50","+0.50","100"],` +
		`["113/01/03","0","0","--","--","--","--","0.00","0"],` +
		`["113/01/04","5"]]}`
	srv := newTestServer(t, body)
	client := NewClient(WithBaseURL(srv.URL))

	// Act
	bars, _, err := client.FetchMonth("20240101", "00631L")

	// Assert — 停牌列 (close<=0) 與短列皆被丟棄。
	if err != nil {
		t.Fatalf("FetchMonth: %v", err)
	}
	if len(bars) != 1 || bars[0].Date != "2024-01-02" {
		t.Fatalf("bars = %+v, want single 2024-01-02 row", bars)
	}
}

func TestFetchMonth_TitleTooShortYieldsEmptyName(t *testing.T) {
	// Arrange — title 欄位數不足,應回空股名而非 panic。
	const body = `{"stat":"OK","title":"短","data":[["113/01/02","1,000","50,000","50.00","51.00","49.00","50.50","+0.50","100"]]}`
	srv := newTestServer(t, body)
	client := NewClient(WithBaseURL(srv.URL))

	// Act
	bars, name, err := client.FetchMonth("20240101", "00631L")

	// Assert
	if err != nil {
		t.Fatalf("FetchMonth: %v", err)
	}
	if name != "" {
		t.Fatalf("stockName = %q, want empty (title too short)", name)
	}
	if len(bars) != 1 {
		t.Fatalf("len(bars) = %d, want 1", len(bars))
	}
}

func TestNewClient_Defaults(t *testing.T) {
	// Arrange + Act
	c := NewClient()

	// Assert — 預設端點與 timeout。
	if c.baseURL != defaultBaseURL {
		t.Fatalf("default baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.httpClient == nil || c.httpClient.Timeout != defaultTimeout {
		t.Fatalf("default httpClient timeout not %v", defaultTimeout)
	}
}

func TestWithHTTPClient_Override(t *testing.T) {
	// Arrange — 注入自訂 http.Client。
	custom := &http.Client{}
	c := NewClient(WithHTTPClient(custom))

	// Assert
	if c.httpClient != custom {
		t.Fatalf("WithHTTPClient did not override http client")
	}
}
