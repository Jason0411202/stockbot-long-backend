package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
)

type fakeSplitFetcher struct {
	actions []marketunits.Action
	err     error
}

func (f fakeSplitFetcher) FetchSplits(context.Context) ([]marketunits.Action, error) {
	return f.actions, f.err
}

func TestUnitBookDurableBeforePublishAndRestartIdempotent(t *testing.T) {
	ctx := context.Background()
	state := newFakeState()
	f := fakeSplitFetcher{actions: []marketunits.Action{{StockID: "AAA", Date: "2026-10-01", Ratio: 4}}}
	u, err := NewMarketUnits(ctx, state, f)
	if err != nil {
		t.Fatal(err)
	}
	state.failKey = unitsStateKey
	if err = u.RefreshUnits(ctx); err == nil || u.UnitBook().Factor("AAA", "2026-10-02") != 1 {
		t.Fatal("failed write leaked new units")
	}
	state.failKey = ""
	if err = u.RefreshUnits(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewMarketUnits(ctx, state, f)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.RefreshUnits(ctx); err != nil {
		t.Fatal(err)
	}
	if restarted.UnitBook().Factor("AAA", "2026-10-02") != 4 || len(restarted.UnitBook().Actions) != 2 {
		t.Fatal("restart doubled unit adjustment")
	}
}

func TestRestoringSortedCatalogDoesNotInheritDefaultSuspension(t *testing.T) {
	state := newFakeState()
	state.values[unitsStateKey] = `{"basis":"2026-09-30","actions":[{"stock_id":"0050","date":"2025-06-18","ratio":4},{"stock_id":"00631L","date":"2026-03-31","suspend_from":"2026-03-25","ratio":22}]}`
	units, err := NewMarketUnits(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	book := units.UnitBook()
	if book.Actions[0].SuspendFrom != "" || book.Actions[1].SuspendFrom != "2026-03-25" {
		t.Fatalf("default date leaked into restored catalog: %+v", book.Actions)
	}
}

type unitSeriesLoader struct {
	*fakeSeriesLoader
	book marketunits.Book
}

func (l unitSeriesLoader) UnitBook() marketunits.Book { return l.book }

func TestOpeningAutoResumesSplitAfterSuspensionAndRestart(t *testing.T) {
	ctx := context.Background()
	cfg := tradingTestCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	s, seed, state, _, ledger, stock := newTradingFixture(cfg)
	stock.names["AAA"] = "AAA"
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	history := risingHistory(day.AddDate(0, 0, -62), 60, 100)
	// Last close is 159 on 10/03, followed by two announced suspension days.
	b := marketunits.Book{Basis: marketunits.BasisDate, Actions: []marketunits.Action{{StockID: "AAA", Date: "2026-10-06", SuspendFrom: "2026-10-04", Ratio: 4}}}
	s.series = unitSeriesLoader{&fakeSeriesLoader{data: map[string][]entity.StockHistory{"AAA": history}}, b}
	state.values[stateKeyWatermark] = "2026-10-03"
	seed.lastBuy["AAA"] = "2026-09-29"
	seed.unrealized = []entity.UnrealizedGainsLoss{{StockID: "AAA", StockName: "AAA", TransactionDate: "2026-09-29", TransactionPrice: 150, Shares: 10, InvestmentCost: 1500}}
	ledger.lots = append(ledger.lots, seed.unrealized...)
	series, err := s.loadSeries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SeedFromDB(ctx, series); err != nil {
		t.Fatal(err)
	}
	before := s.engine.Cash()
	peak := s.engine.RiskState("AAA").Peak
	s.realtime.(*fakeRealtime).opens = map[string]float64{"AAA": 39.75} // 159 accounting units
	if err = s.runOneDayAtOpen(ctx, &tradingExecutor{svc: s}, day, false); err != nil {
		t.Fatal(err)
	}
	if state.values[stateKeyWatermark] != "2026-10-06" || s.engine.Cash() != before || s.engine.CostBasis() != 1500 || s.engine.RiskState("AAA").Peak != peak {
		t.Fatal("unit change altered ledger/risk or blocked resume")
	}
	if err = s.SeedFromDB(ctx, series); err != nil {
		t.Fatal(err)
	}
	if err = s.runOneDayAtOpen(ctx, &tradingExecutor{svc: s}, day, false); err != nil {
		t.Fatal(err)
	}
	if len(ledger.lots) != 1 || ledger.lots[0].Shares != 10 || s.engine.Cash() != before {
		t.Fatal("duplicate split after restart")
	}
}

type unitQuoteStock struct {
	quoteStock
	book marketunits.Book
}

func (q unitQuoteStock) UnitBook() marketunits.Book { return q.book }

func TestDisplayRebasesPricesAndQuantitiesButNeverMoney(t *testing.T) {
	// Forward and reverse splits use the same API projection.
	for _, ratio := range []float64{4, 1.0 / 7} {
		date := "2026-10-02"
		// Factor after the basis. Testing a later display date directly also
		// verifies the fractional conversion used by all portfolio endpoints.
		book := marketunits.Book{Basis: marketunits.BasisDate, Actions: []marketunits.Action{{StockID: "AAA", Date: "2026-10-01", Ratio: ratio}}}
		f := book.DisplayFactor("AAA", "2026-10-02")
		if math.Abs((70/f)*(103*f)-7210) > 1e-8 {
			t.Fatal("display lost ownership")
		}
		ledger := &fakeLedger{listU: []entity.UnrealizedGainsLoss{{StockID: "AAA", TransactionDate: "2026-09-01", TransactionPrice: 60, Shares: 103, InvestmentCost: 6180}}}
		stock := unitQuoteStock{quoteStock{newFakeStock(), date, 70}, book}
		svc := NewPortfolioService(ledger, stock, newTestLogger())
		svc.now = func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
		rows, err := svc.UnrealizedGainsLosses(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		actualFactor := book.DisplayFactor("AAA", date)
		if math.Abs(rows[0].TodayClosePrice*rows[0].Shares-7210) > 1e-8 || rows[0].NowValue != 7210 || rows[0].InvestmentCost != 6180 || rows[0].Shares != 103*actualFactor {
			t.Fatalf("wrong display: %+v", rows)
		}
	}
}
