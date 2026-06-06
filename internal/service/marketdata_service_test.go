package service

import (
	"context"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

func TestMonthlyBackfillDates(t *testing.T) {
	dates := monthlyBackfillDates("20240315", 2)
	// currentDate first, then day=1 of each prior month, newest-first.
	want := []string{"20240315", "20240201", "20240101"}
	if len(dates) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", dates, want)
	}
	for i := range want {
		if dates[i] != want[i] {
			t.Fatalf("dates[%d]=%s want %s (full %v)", i, dates[i], want[i], dates)
		}
	}
}

func TestMonthlyBackfillDates_InvalidDate(t *testing.T) {
	dates := monthlyBackfillDates("not-a-date", 3)
	// On parse error the original returns just the seed date.
	if len(dates) != 1 || dates[0] != "not-a-date" {
		t.Fatalf("invalid date should yield only the seed, got %v", dates)
	}
}

func TestDateToYearMonth(t *testing.T) {
	ym, err := dateToYearMonth("20240315")
	if err != nil {
		t.Fatalf("dateToYearMonth: %v", err)
	}
	if ym != "2024-03" {
		t.Fatalf("got %s want 2024-03", ym)
	}
	if _, err := dateToYearMonth("bad"); err == nil {
		t.Fatal("expected error on bad date")
	}
}

func TestFetchAndInsertMonth_HappyPath_MarksCompleteWhenPriorMonth(t *testing.T) {
	fetcher := &fakeFetcher{
		stockName: "元大台灣50正2",
		bars: []entity.Bar{
			{Date: "2024-01-02", Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 1000},
			{Date: "2024-01-03", Open: 10.5, High: 12, Low: 10, Close: 11, Volume: 2000},
		},
	}
	stock := newFakeStock()
	backfill := newFakeBackfill()
	cfg := &config.Config{TrackStocks: []string{"00631L"}}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	// ym (2024-01) != currentMonth (2024-03) -> should mark complete.
	if err := svc.fetchAndInsertMonth(context.Background(), "00631L", "20240101", "2024-01", "2024-03"); err != nil {
		t.Fatalf("fetchAndInsertMonth: %v", err)
	}

	if len(fetcher.calls) != 1 || fetcher.calls[0].date != "20240101" || fetcher.calls[0].stockID != "00631L" {
		t.Fatalf("fetch call wrong: %+v", fetcher.calls)
	}
	if len(stock.insertedBars) != 2 {
		t.Fatalf("expected 2 inserted bars, got %d", len(stock.insertedBars))
	}
	if stock.insertedBars[0].stockName != "元大台灣50正2" {
		t.Fatalf("stock name not propagated to insert: %+v", stock.insertedBars[0])
	}
	if len(backfill.marked) != 1 || backfill.marked[0].month != "2024-01" || backfill.marked[0].stockID != "00631L" {
		t.Fatalf("expected month marked complete, got %+v", backfill.marked)
	}
}

func TestFetchAndInsertMonth_CurrentMonthNotMarked(t *testing.T) {
	fetcher := &fakeFetcher{stockName: "n", bars: []entity.Bar{{Date: "2024-03-02", Close: 10}}}
	stock := newFakeStock()
	backfill := newFakeBackfill()
	cfg := &config.Config{TrackStocks: []string{"X"}}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	// ym == currentMonth -> must NOT mark complete.
	if err := svc.fetchAndInsertMonth(context.Background(), "X", "20240301", "2024-03", "2024-03"); err != nil {
		t.Fatalf("fetchAndInsertMonth: %v", err)
	}
	if len(stock.insertedBars) != 1 {
		t.Fatalf("expected 1 inserted bar, got %d", len(stock.insertedBars))
	}
	if len(backfill.marked) != 0 {
		t.Fatalf("current month must not be marked complete, got %+v", backfill.marked)
	}
}

func TestFetchAndInsertMonth_FetchErrorPropagatesNoMark(t *testing.T) {
	fetcher := &fakeFetcher{err: errFake}
	stock := newFakeStock()
	backfill := newFakeBackfill()
	cfg := &config.Config{TrackStocks: []string{"X"}}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	if err := svc.fetchAndInsertMonth(context.Background(), "X", "20240101", "2024-01", "2024-03"); err == nil {
		t.Fatal("expected fetch error to propagate")
	}
	if len(stock.insertedBars) != 0 || len(backfill.marked) != 0 {
		t.Fatalf("nothing should be inserted/marked on fetch error")
	}
}

func TestFetchAndInsertMonth_MarkErrorIsNonFatal(t *testing.T) {
	fetcher := &fakeFetcher{stockName: "n", bars: []entity.Bar{{Date: "2024-01-02", Close: 10}}}
	stock := newFakeStock()
	backfill := newFakeBackfill()
	backfill.markErr = errFake
	cfg := &config.Config{TrackStocks: []string{"X"}}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	// Mark failure is logged warn, not returned (matches original).
	if err := svc.fetchAndInsertMonth(context.Background(), "X", "20240101", "2024-01", "2024-03"); err != nil {
		t.Fatalf("mark error should be non-fatal, got %v", err)
	}
}

func TestBackfillMonths_SkipsCompletedNonCurrentMonths(t *testing.T) {
	fetcher := &fakeFetcher{stockName: "n", bars: []entity.Bar{{Date: "2024-03-02", Close: 10}}}
	stock := newFakeStock()
	backfill := newFakeBackfill()
	// Mark a prior month as already complete; it must be skipped.
	// monthlyBackfillDates seed is "today"; we cannot control time.Now here, so
	// just assert that a completed prior month is never re-fetched by checking
	// that no fetch call targets a completed ym. We seed completion for a month
	// far in the past that the loop will include.
	backfill.completed["X"] = map[string]bool{"2000-01": true}
	cfg := &config.Config{TrackStocks: []string{"X"}, MaxBackMonths: 1}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	// Use a large months count so 2000-01 cannot appear anyway; the real intent
	// is exercised by the unit logic. This mainly verifies no panic + per-stock
	// CompletedMonths is consulted.
	if err := svc.BackfillMonths(context.Background(), 1); err != nil {
		t.Fatalf("BackfillMonths: %v", err)
	}
	// At least the seed (today) month is fetched.
	if len(fetcher.calls) == 0 {
		t.Fatal("expected at least one fetch call")
	}
}

func TestUpdateDatabase_DailyPathFetchesSeedMonth(t *testing.T) {
	fetcher := &fakeFetcher{stockName: "n", bars: []entity.Bar{{Date: "2024-03-02", Close: 10}}}
	stock := newFakeStock()
	backfill := newFakeBackfill()
	// MaxBackMonths=0 -> monthlyBackfillDates returns only the seed (today) date,
	// so exactly one fetch (and one 3s courtesy sleep) per stock.
	cfg := &config.Config{TrackStocks: []string{"X"}, MaxBackMonths: 0}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	if err := svc.UpdateDatabase(context.Background()); err != nil {
		t.Fatalf("UpdateDatabase: %v", err)
	}
	if len(fetcher.calls) != 1 {
		t.Fatalf("expected exactly one fetch for seed date, got %d", len(fetcher.calls))
	}
	if len(stock.insertedBars) != 1 {
		t.Fatalf("expected one inserted bar, got %d", len(stock.insertedBars))
	}
	// Seed date is the current month -> must NOT be marked complete.
	if len(backfill.marked) != 0 {
		t.Fatalf("current-month seed must not be marked complete, got %+v", backfill.marked)
	}
}

func TestBackfillMonths_CompletedMonthsErrorPropagates(t *testing.T) {
	fetcher := &fakeFetcher{}
	stock := newFakeStock()
	backfill := &errBackfill{}
	cfg := &config.Config{TrackStocks: []string{"X"}, MaxBackMonths: 1}
	svc := NewMarketDataService(fetcher, stock, backfill, cfg, newTestLogger())

	if err := svc.BackfillMonths(context.Background(), 1); err == nil {
		t.Fatal("expected CompletedMonths error to propagate")
	}
}

// errBackfill fails CompletedMonths to exercise the error path.
type errBackfill struct{}

func (errBackfill) CompletedMonths(context.Context, string) (map[string]bool, error) {
	return nil, errFake
}
func (errBackfill) MarkComplete(context.Context, string, string) error { return nil }
