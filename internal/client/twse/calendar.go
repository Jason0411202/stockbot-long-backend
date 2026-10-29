// internal/client/twse/calendar.go 依 TWSE 公告休市表與每月市場成交資訊判定台股實際交易日。
package twse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/helper"
)

// taipeiZone 為交易所時區 (UTC+8,無日光節約時間)。
var taipeiZone = time.FixedZone("Asia/Taipei", 8*60*60)

const (
	// calendarTTL 為公告休市表快取的有效期間。
	calendarTTL = 6 * time.Hour
	// sessionPublishedHour 為交易日隔日 (台灣時間) 確定可取得該日市場成交資訊的時點。
	sessionPublishedHour = 8
	// provisionalTTL 為暫定臨時休市 (尚未確定) 的成交資訊快取有效期間,逾時即重新查證。
	provisionalTTL = 5 * time.Minute
	// closureSettleAfter 為成交資訊抓取時點晚於公布時點多久後,缺列的平日即確定為臨時休市。
	closureSettleAfter = 24 * time.Hour
	// sessionFetchGap 為相鄰兩次市場成交資訊查詢之間的最短間隔。
	sessionFetchGap = 3 * time.Second
)

// Calendar 依 TWSE 公告休市表判定休市日;颱風等臨時休市不會列於公告休市表,
// 已過公布時點的平日另以官方每月市場成交資訊 (FMTQIK) 確認是否實際開市。
// 官方資料無法確認時一律回傳錯誤:不能只憑星期推算就放行交易。
type Calendar struct {
	client      *http.Client
	url         string
	sessionsURL string
	now         func() time.Time
	fetchGap    time.Duration
	mu          sync.Mutex
	years       map[int]calendarYear
	months      map[string]sessionMonth
	lastFetch   time.Time
}

// calendarYear 為單一年度公告休市日集合與抓取時間。
type calendarYear struct {
	closed  map[string]bool
	fetched time.Time
}

// sessionMonth 為單月市場成交資訊所列的實際開市日集合、該月最晚交易日與抓取時間。
type sessionMonth struct {
	open    map[string]bool
	latest  string
	fetched time.Time
}

// settled 回傳 date 在此快取中的判定是否已確定:列於成交資訊 (開市)、同月已有更晚的交易日,
// 或抓取時點已晚於 date 的公布時點 closureSettleAfter 以上 (休市)。
func (m sessionMonth) settled(date string, published time.Time) bool {
	return m.open[date] || m.latest > date || m.fetched.Sub(published) >= closureSettleAfter
}

// NewCalendar 建立指向 TWSE 正式端點的交易日曆。
func NewCalendar() *Calendar {
	return &Calendar{
		client:      &http.Client{Timeout: 20 * time.Second},
		url:         "https://www.twse.com.tw/holidaySchedule/holidaySchedule",
		sessionsURL: "https://www.twse.com.tw/rwd/zh/afterTrading/FMTQIK",
		now:         time.Now,
		fetchGap:    sessionFetchGap,
		years:       map[int]calendarYear{},
		months:      map[string]sessionMonth{},
	}
}

// IsTradingDay 回傳 day 是否為實際開市日:週末與公告休市日為休市;已過公布時點的平日
// 若未列於當月市場成交資訊,視為臨時休市。官方資料無法確認時回傳錯誤。
func (c *Calendar) IsTradingDay(ctx context.Context, day time.Time) (bool, error) {
	// 週末不需查詢任何官方資料。
	if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// 先查公告休市表,再以市場成交資訊排除臨時休市。
	closed, err := c.scheduledClosure(ctx, day)
	if err != nil || closed {
		return false, err
	}
	closed, err = c.unscheduledClosure(ctx, day)
	if err != nil {
		return false, err
	}
	return !closed, nil
}

// scheduledClosure 回傳 day 是否列於 TWSE 公告休市表;各年度休市表逾 calendarTTL 即重新抓取。
func (c *Calendar) scheduledClosure(ctx context.Context, day time.Time) (bool, error) {
	y, ok := c.years[day.Year()]
	if !ok || c.now().Sub(y.fetched) > calendarTTL {
		var err error
		if y, err = c.fetchSchedule(ctx, day.Year()); err != nil {
			return false, err
		}
		c.years[day.Year()] = y
	}
	return y.closed[day.Format("2006-01-02")], nil
}

// fetchSchedule 抓取 year 的 TWSE 公告休市表,回傳休市日集合 (開始/最後交易日註記不算休市)。
func (c *Calendar) fetchSchedule(ctx context.Context, year int) (calendarYear, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?response=json&date=%d0101", c.url, year), nil)
	if err != nil {
		return calendarYear{}, fmt.Errorf("build TWSE calendar request %d: %w", year, err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return calendarYear{}, fmt.Errorf("TWSE calendar %d: %w", year, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return calendarYear{}, fmt.Errorf("TWSE calendar HTTP %d", resp.StatusCode)
	}

	// 解碼並驗證回應年度。
	var body struct {
		Stat      string     `json:"stat"`
		QueryYear int        `json:"queryYear"`
		Data      [][]string `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return calendarYear{}, fmt.Errorf("decode TWSE calendar %d: %w", year, err)
	}
	if body.Stat != "ok" || body.QueryYear != year || len(body.Data) == 0 {
		return calendarYear{}, fmt.Errorf("TWSE calendar unavailable for %d", year)
	}

	// 收集休市日;同一張表也列出開始/最後交易日,這些日期照常開市。
	y := calendarYear{closed: map[string]bool{}, fetched: c.now()}
	for _, row := range body.Data {
		if len(row) < 2 {
			return calendarYear{}, fmt.Errorf("invalid TWSE calendar row")
		}
		if _, err = time.Parse("2006-01-02", row[0]); err != nil {
			return calendarYear{}, fmt.Errorf("invalid TWSE calendar date %q: %w", row[0], err)
		}
		if strings.Contains(row[1], "開始交易日") || strings.Contains(row[1], "最後交易日") {
			continue
		}
		y.closed[row[0]] = true
	}
	return y, nil
}

// unscheduledClosure 回傳 day 是否為未列於公告休市表的臨時休市 (如颱風停止交易)。
// day 隔日 08:00 (台灣時間) 前成交資訊尚未確定公布,一律回傳 false,由呼叫端要求該日資料存在。
func (c *Calendar) unscheduledClosure(ctx context.Context, day time.Time) (bool, error) {
	published := time.Date(day.Year(), day.Month(), day.Day()+1, sessionPublishedHour, 0, 0, 0, taipeiZone)
	now := c.now()
	if now.Before(published) {
		return false, nil
	}

	// 快取早於公布時點,或暫定休市已逾 provisionalTTL 時重新查證;已確定的判定沿用快取。
	month := day.Format("200601")
	date := day.Format("2006-01-02")
	m, ok := c.months[month]
	if !ok || m.fetched.Before(published) || (!m.settled(date, published) && now.Sub(m.fetched) > provisionalTTL) {
		var err error
		if m, err = c.fetchSessions(ctx, month); err != nil {
			return false, err
		}
		c.months[month] = m
	}
	return !m.open[date], nil
}

// fetchSessions 抓取 month ("YYYYMM") 的官方市場成交資訊,回傳該月實際開市日集合。
// 當月「沒有符合條件的資料」代表尚無任何交易日;其餘非 OK 狀態、月份不符、日期無法解析皆回傳錯誤。
func (c *Calendar) fetchSessions(ctx context.Context, month string) (sessionMonth, error) {
	// 間隔前次查詢後送出請求,並以查詢當下時點作為快取的抓取時間;時間戳參數避開 CDN 舊快取。
	if err := c.waitFetchGap(ctx); err != nil {
		return sessionMonth{}, fmt.Errorf("wait TWSE market sessions %s: %w", month, err)
	}
	fetched := c.now()
	url := fmt.Sprintf("%s?response=json&date=%s01&_=%d", c.sessionsURL, month, fetched.UnixMilli())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sessionMonth{}, fmt.Errorf("build TWSE market sessions request %s: %w", month, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return sessionMonth{}, fmt.Errorf("TWSE market sessions %s: %w", month, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sessionMonth{}, fmt.Errorf("TWSE market sessions %s HTTP %d", month, resp.StatusCode)
	}

	// 解碼並驗證回應狀態與查詢月份。
	var body struct {
		Stat string  `json:"stat"`
		Date string  `json:"date"`
		Data [][]any `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return sessionMonth{}, fmt.Errorf("decode TWSE market sessions %s: %w", month, err)
	}
	m := sessionMonth{open: map[string]bool{}, fetched: fetched}
	if strings.Contains(body.Stat, "沒有符合條件的資料") && month == fetched.In(taipeiZone).Format("200601") {
		return m, nil
	}
	if body.Stat != "OK" || !strings.HasPrefix(body.Date, month) || len(body.Data) == 0 {
		return sessionMonth{}, fmt.Errorf("TWSE market sessions unavailable for %s: %s", month, body.Stat)
	}

	// 逐列將民國日期轉為西元日期,並確認每一列都屬於查詢月份。
	for _, row := range body.Data {
		var raw string
		if len(row) > 0 {
			if s, ok := row[0].(string); ok {
				raw = s
			}
		}
		date, err := sessionDate(raw)
		if err != nil {
			return sessionMonth{}, fmt.Errorf("TWSE market sessions %s: %w", month, err)
		}
		if date.Format("200601") != month {
			return sessionMonth{}, fmt.Errorf("TWSE market session %q outside %s", raw, month)
		}
		key := date.Format("2006-01-02")
		m.open[key] = true
		if key > m.latest {
			m.latest = key
		}
	}
	return m, nil
}

// sessionDate 將市場成交資訊的民國日期 (如 "115/10/22") 解析為西元日期。
func sessionDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if strings.Count(raw, "/") != 2 {
		return time.Time{}, fmt.Errorf("invalid session date %q", raw)
	}
	ad, err := helper.ROCToAD(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid session date %q: %w", raw, err)
	}
	return time.Parse("2006-01-02", ad)
}

// waitFetchGap 確保與上一次市場成交資訊查詢至少相隔 fetchGap,等待期間可被 ctx 取消。
func (c *Calendar) waitFetchGap(ctx context.Context) error {
	if wait := c.fetchGap - time.Since(c.lastFetch); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	c.lastFetch = time.Now()
	return nil
}

// PreviousTradingDay 回傳 day 之前最近的實際開市日,最多往前搜尋 40 日。
func (c *Calendar) PreviousTradingDay(ctx context.Context, day time.Time) (time.Time, error) {
	for d, n := day.AddDate(0, 0, -1), 0; n < 40; d, n = d.AddDate(0, 0, -1), n+1 {
		open, err := c.IsTradingDay(ctx, d)
		if err != nil {
			return time.Time{}, err
		}
		if open {
			return d, nil
		}
	}
	return time.Time{}, fmt.Errorf("no prior trading day before %s", day.Format("2006-01-02"))
}
