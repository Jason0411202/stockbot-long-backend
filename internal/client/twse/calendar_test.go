// internal/client/twse/calendar_test.go 驗證交易日曆的公告休市、臨時休市與官方資料失效判定。
package twse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// holidays2026 為 2026 年公告休市表 (含最後交易日註記) 的測試片段。
const holidays2026 = `{"stat":"ok","queryYear":2026,"data":[["2026-02-11","農曆春節前最後交易日",""],["2026-09-25","中秋節",""],["2026-09-28","教師節",""],["2026-10-09","國慶日",""],["2026-10-10","國慶日",""],["2026-10-25","臺灣光復暨金門古寧頭大捷紀念日",""],["2026-10-26","臺灣光復暨金門古寧頭大捷紀念日",""],["2026-12-25","行憲紀念日",""]]}`

// noSessionData 為 TWSE 查無成交資訊時的回應。
const noSessionData = `{"stat":"很抱歉，沒有符合條件的資料!"}`

// fakeExchange 模擬 TWSE 公告休市表與每月市場成交資訊端點,並記錄成交資訊查詢次數。
type fakeExchange struct {
	mu       sync.Mutex
	sessions map[string]string // "YYYYMM" → FMTQIK 回應內容
	calls    atomic.Int32
}

// setSessions 設定 month ("YYYYMM") 的市場成交資訊回應。
func (f *fakeExchange) setSessions(month, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[month] = body
}

// sessionsJSON 以西元日期清單組出 FMTQIK 成功回應 (日期轉為民國格式)。
func sessionsJSON(month string, days ...string) string {
	rows := make([]string, 0, len(days))
	for _, d := range days {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			panic(err)
		}
		rows = append(rows, fmt.Sprintf(`["%d/%02d/%02d","1","2","3","4","5"]`, t.Year()-1911, int(t.Month()), t.Day()))
	}
	return fmt.Sprintf(`{"stat":"OK","date":"%s01","data":[%s]}`, month, strings.Join(rows, ","))
}

// newTestCalendar 建立指向假交易所的 Calendar,時鐘由 now 決定且不做查詢間隔等待。
func newTestCalendar(t *testing.T, ex *fakeExchange, now *time.Time) *Calendar {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/holidaySchedule":
			if !strings.HasSuffix(date, "0101") {
				t.Errorf("TWSE year must use date=YYYY0101, got %q", date)
			}
			w.Write([]byte(holidays2026))
		case "/FMTQIK":
			ex.calls.Add(1)
			if len(date) != 8 || !strings.HasSuffix(date, "01") {
				t.Errorf("TWSE month must use date=YYYYMM01, got %q", date)
			}
			if r.URL.Query().Get("_") == "" {
				t.Error("market sessions request must carry a cache-busting timestamp")
			}
			ex.mu.Lock()
			body, ok := ex.sessions[date[:6]]
			ex.mu.Unlock()
			if !ok {
				body = noSessionData
			}
			if body == "HTTP 500" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write([]byte(body))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewCalendar()
	c.url = srv.URL + "/holidaySchedule"
	c.sessionsURL = srv.URL + "/FMTQIK"
	c.now = func() time.Time { return *now }
	c.fetchGap = 0
	return c
}

// taipei 回傳台灣時間的時間點。
func taipei(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, taipeiZone)
}

// day 回傳以 UTC 午夜表示的交易日 (與水位線、價格序列的日期格式一致)。
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// mustTradingDay 斷言 IsTradingDay(d) 無錯誤且結果為 want。
func mustTradingDay(t *testing.T, c *Calendar, d time.Time, want bool) {
	t.Helper()
	open, err := c.IsTradingDay(context.Background(), d)
	if err != nil || open != want {
		t.Fatalf("IsTradingDay(%s) = %v, %v; want %v", d.Format("2006-01-02"), open, err, want)
	}
}

// mustCalls 斷言市場成交資訊累計查詢次數。
func mustCalls(t *testing.T, ex *fakeExchange, want int32) {
	t.Helper()
	if got := ex.calls.Load(); got != want {
		t.Fatalf("market sessions fetched %d times, want %d", got, want)
	}
}

// TestCalendar_HolidaysAndTradingDayAnnotations 驗證公告休市日被略過、最後交易日註記視為開市、年度不符回錯。
func TestCalendar_HolidaysAndTradingDayAnnotations(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202609", sessionsJSON("202609", "2026-09-23", "2026-09-24"))
	ex.setSessions("202602", sessionsJSON("202602", "2026-02-10", "2026-02-11"))
	now := taipei(2026, 9, 29, 9, 10)
	c := newTestCalendar(t, ex, &now)

	prev, err := c.PreviousTradingDay(context.Background(), day(2026, 9, 29))
	if err != nil || prev.Format("2006-01-02") != "2026-09-24" {
		t.Fatalf("previous=%v err=%v", prev, err)
	}
	mustTradingDay(t, c, day(2026, 2, 11), true)
	if _, err = c.IsTradingDay(context.Background(), day(2027, 1, 4)); err == nil {
		t.Fatal("wrong calendar year accepted")
	}
}

// TestCalendar_WeekendAndHolidaySkipSessions 驗證週末與公告休市日直接判為休市,不查詢成交資訊。
func TestCalendar_WeekendAndHolidaySkipSessions(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	now := taipei(2026, 10, 29, 9, 10)
	c := newTestCalendar(t, ex, &now)

	for _, d := range []time.Time{day(2026, 10, 24), day(2026, 10, 25), day(2026, 10, 26)} {
		mustTradingDay(t, c, d, false)
	}
	mustCalls(t, ex, 0)
}

// TestCalendar_UnscheduledClosureFromMarketSessions 重現 2026-10-23 颱風停市:公告休市表未列該日,
// 但市場成交資訊無該日交易,前一交易日須跳過該日回到 10-22。
func TestCalendar_UnscheduledClosureFromMarketSessions(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-20", "2026-10-21", "2026-10-22"))
	now := taipei(2026, 10, 27, 9, 10)
	c := newTestCalendar(t, ex, &now)

	mustTradingDay(t, c, day(2026, 10, 23), false)
	prev, err := c.PreviousTradingDay(context.Background(), day(2026, 10, 27))
	if err != nil || prev.Format("2006-01-02") != "2026-10-22" {
		t.Fatalf("previous=%v err=%v", prev, err)
	}
	mustCalls(t, ex, 1)
}

// TestCalendar_ClosureOnDayBeforeOpening 驗證臨時休市隔日的開盤時段 (休市判定尚屬暫定),前一交易日即跳過休市日。
func TestCalendar_ClosureOnDayBeforeOpening(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-20", "2026-10-21"))
	now := taipei(2026, 10, 23, 9, 10)
	c := newTestCalendar(t, ex, &now)

	prev, err := c.PreviousTradingDay(context.Background(), day(2026, 10, 23))
	if err != nil || prev.Format("2006-01-02") != "2026-10-21" {
		t.Fatalf("previous=%v err=%v", prev, err)
	}
}

// TestCalendar_UnpublishedDayStaysScheduledOpen 驗證公布時點 (隔日 08:00) 前,平日依公告休市表視為開市且不查詢成交資訊。
func TestCalendar_UnpublishedDayStaysScheduledOpen(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-21"))
	now := taipei(2026, 10, 23, 7, 59)
	c := newTestCalendar(t, ex, &now)

	mustTradingDay(t, c, day(2026, 10, 22), true)
	mustTradingDay(t, c, day(2026, 10, 23), true)
	mustCalls(t, ex, 0)
}

// TestCalendar_PublicationBoundary 驗證公布時點以絕對時間比較:UTC 表示的台灣 08:00 整點即查詢成交資訊,跨年亦同。
func TestCalendar_PublicationBoundary(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-21"))
	ex.setSessions("202612", sessionsJSON("202612", "2026-12-30"))
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC) // 台灣 2026-10-23 08:00
	c := newTestCalendar(t, ex, &now)

	mustTradingDay(t, c, day(2026, 10, 22), false)
	mustCalls(t, ex, 1)

	now = taipei(2026, 12, 31, 23, 59).Add(8 * time.Hour) // 台灣 2027-01-01 07:59
	mustTradingDay(t, c, day(2026, 12, 31), true)
	mustCalls(t, ex, 1)
	now = taipei(2027, 1, 1, 8, 0)
	mustTradingDay(t, c, day(2026, 12, 31), false)
	mustCalls(t, ex, 2)
}

// TestCalendar_RefetchesSessionsAfterPublication 驗證快取早於某日公布時點時會重抓,其後已確定的判定沿用快取。
func TestCalendar_RefetchesSessionsAfterPublication(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-21"))
	now := taipei(2026, 10, 22, 20, 0)
	c := newTestCalendar(t, ex, &now)

	mustTradingDay(t, c, day(2026, 10, 21), true)
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-21", "2026-10-22"))
	now = taipei(2026, 10, 23, 9, 10)
	mustTradingDay(t, c, day(2026, 10, 22), true)
	now = now.Add(7 * 24 * time.Hour)
	mustTradingDay(t, c, day(2026, 10, 21), true)
	mustTradingDay(t, c, day(2026, 10, 22), true)
	mustCalls(t, ex, 2)
}

// TestCalendar_ProvisionalClosureRefetched 驗證尚未確定的休市判定逾 provisionalTTL 即重新查證,延遲公布的交易日可於開盤時段內恢復。
func TestCalendar_ProvisionalClosureRefetched(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-21"))
	now := taipei(2026, 10, 23, 9, 10)
	c := newTestCalendar(t, ex, &now)

	mustTradingDay(t, c, day(2026, 10, 22), false)
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-21", "2026-10-22"))
	now = now.Add(provisionalTTL - time.Second)
	mustTradingDay(t, c, day(2026, 10, 22), false)
	mustCalls(t, ex, 1)
	now = now.Add(2 * time.Second)
	mustTradingDay(t, c, day(2026, 10, 22), true)
	mustCalls(t, ex, 2)
}

// TestCalendar_SettledClosureNotRefetched 驗證同月已有更晚交易日,或抓取時已逾公布時點 24 小時,休市判定即確定不再重抓。
func TestCalendar_SettledClosureNotRefetched(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-22", "2026-10-27"))
	now := taipei(2026, 10, 28, 9, 10)
	c := newTestCalendar(t, ex, &now)

	mustTradingDay(t, c, day(2026, 10, 23), false)
	now = now.Add(time.Hour)
	mustTradingDay(t, c, day(2026, 10, 23), false)
	mustCalls(t, ex, 1)

	ex.setSessions("202610", sessionsJSON("202610", "2026-10-28", "2026-10-29"))
	now = taipei(2026, 11, 2, 9, 10)
	mustTradingDay(t, c, day(2026, 10, 30), false)
	now = now.Add(time.Hour)
	mustTradingDay(t, c, day(2026, 10, 30), false)
	mustCalls(t, ex, 2)
}

// TestCalendar_CurrentMonthWithoutSessions 驗證當月第一個平日即臨時休市時,當月「查無資料」視為尚無交易日。
func TestCalendar_CurrentMonthWithoutSessions(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202611", sessionsJSON("202611", "2026-11-27", "2026-11-30"))
	now := taipei(2026, 12, 2, 9, 10)
	c := newTestCalendar(t, ex, &now)

	prev, err := c.PreviousTradingDay(context.Background(), day(2026, 12, 2))
	if err != nil || prev.Format("2006-01-02") != "2026-11-30" {
		t.Fatalf("previous=%v err=%v", prev, err)
	}
}

// TestCalendar_FailedFetchNotCached 驗證成交資訊查詢失敗不寫入快取,下一次呼叫可恢復。
func TestCalendar_FailedFetchNotCached(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", "HTTP 500")
	now := taipei(2026, 10, 27, 9, 10)
	c := newTestCalendar(t, ex, &now)

	if _, err := c.IsTradingDay(context.Background(), day(2026, 10, 23)); err == nil {
		t.Fatal("HTTP failure accepted")
	}
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-22"))
	mustTradingDay(t, c, day(2026, 10, 23), false)
	mustCalls(t, ex, 2)
}

// TestCalendar_MarketSessionsUnavailableFailsClosed 驗證成交資訊無法確認時回傳錯誤,不推定臨時休市。
func TestCalendar_MarketSessionsUnavailableFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"past month no data", noSessionData},
		{"http error", "HTTP 500"},
		{"empty month", `{"stat":"OK","date":"20261001","data":[]}`},
		{"month mismatch", sessionsJSON("202609", "2026-09-30")},
		{"row outside month", `{"stat":"OK","date":"20261001","data":[["115/09/30","1"]]}`},
		{"malformed date", `{"stat":"OK","date":"20261001","data":[["20261022","1"]]}`},
		{"non-string date", `{"stat":"OK","date":"20261001","data":[[1151022,"1"]]}`},
		{"empty row", `{"stat":"OK","date":"20261001","data":[[]]}`},
		{"malformed json", `{"stat":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &fakeExchange{sessions: map[string]string{}}
			ex.setSessions("202610", tt.body)
			now := taipei(2026, 11, 3, 9, 10)
			c := newTestCalendar(t, ex, &now)

			if open, err := c.IsTradingDay(context.Background(), day(2026, 10, 23)); err == nil {
				t.Fatalf("unverifiable market sessions accepted: open=%v", open)
			}
		})
	}
}

// TestCalendar_FetchPacing 驗證相鄰兩次成交資訊查詢至少間隔 fetchGap,且等待可被 ctx 取消。
func TestCalendar_FetchPacing(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202609", sessionsJSON("202609", "2026-09-24"))
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-22"))
	now := taipei(2026, 10, 29, 9, 10)
	c := newTestCalendar(t, ex, &now)
	c.fetchGap = 100 * time.Millisecond

	start := time.Now()
	mustTradingDay(t, c, day(2026, 9, 24), true)
	mustTradingDay(t, c, day(2026, 10, 22), true)
	if elapsed := time.Since(start); elapsed < c.fetchGap {
		t.Fatalf("two fetches took %v, want at least %v", elapsed, c.fetchGap)
	}

	c.fetchGap = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.IsTradingDay(ctx, day(2026, 8, 31)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait returned %v, want context.Canceled", err)
	}
}

// TestCalendar_PreviousTradingDayErrors 驗證前一交易日查詢會傳遞官方資料錯誤,且搜尋上限內無開市日時回錯。
func TestCalendar_PreviousTradingDayErrors(t *testing.T) {
	ex := &fakeExchange{sessions: map[string]string{}}
	ex.setSessions("202610", "HTTP 500")
	now := taipei(2026, 10, 29, 9, 10)
	c := newTestCalendar(t, ex, &now)
	if _, err := c.PreviousTradingDay(context.Background(), day(2026, 10, 29)); err == nil {
		t.Fatal("market sessions failure not propagated")
	}

	ex.setSessions("202609", `{"stat":"OK","date":"20260901","data":[["115/09/01","1"]]}`)
	ex.setSessions("202610", sessionsJSON("202610", "2026-10-30"))
	now = taipei(2026, 11, 3, 9, 10)
	c = newTestCalendar(t, ex, &now)
	if _, err := c.PreviousTradingDay(context.Background(), day(2026, 10, 30)); err == nil {
		t.Fatal("40-day search without any session accepted")
	}
}
