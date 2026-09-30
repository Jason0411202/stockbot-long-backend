package twse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Calendar uses TWSE's published market closures. Unknown/unavailable years
// return an error: weekday arithmetic alone must never authorize a trade.
type Calendar struct {
	client *http.Client
	url    string
	mu     sync.Mutex
	years  map[int]calendarYear
}
type calendarYear struct {
	closed  map[string]bool
	fetched time.Time
}

func NewCalendar() *Calendar {
	return &Calendar{client: &http.Client{Timeout: 20 * time.Second}, url: "https://www.twse.com.tw/holidaySchedule/holidaySchedule", years: map[int]calendarYear{}}
}

func (c *Calendar) IsTradingDay(ctx context.Context, day time.Time) (bool, error) {
	if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	y, ok := c.years[day.Year()]
	if !ok || time.Since(y.fetched) > 6*time.Hour {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?response=json&date=%d0101", c.url, day.Year()), nil)
		if err != nil {
			return false, err
		}
		resp, err := c.client.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("TWSE calendar HTTP %d", resp.StatusCode)
		}
		var body struct {
			Stat      string     `json:"stat"`
			QueryYear int        `json:"queryYear"`
			Data      [][]string `json:"data"`
		}
		if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, err
		}
		if body.Stat != "ok" || body.QueryYear != day.Year() || len(body.Data) == 0 {
			return false, fmt.Errorf("TWSE calendar unavailable for %d", day.Year())
		}
		y = calendarYear{closed: map[string]bool{}, fetched: time.Now()}
		for _, row := range body.Data {
			if len(row) < 2 {
				return false, fmt.Errorf("invalid TWSE calendar row")
			}
			if _, err = time.Parse("2006-01-02", row[0]); err != nil {
				return false, err
			}
			// The same table also lists first/last trading days; those are open.
			if strings.Contains(row[1], "開始交易日") || strings.Contains(row[1], "最後交易日") {
				continue
			}
			y.closed[row[0]] = true
		}
		c.years[day.Year()] = y
	}
	return !y.closed[day.Format("2006-01-02")], nil
}

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
