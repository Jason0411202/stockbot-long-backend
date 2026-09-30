package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

func copyValues(src map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range src {
		out[k] = v
	}
	return out
}
func (f *fakeState) WithTradingTransaction(ctx context.Context, fn func(context.Context) error) error {
	values := copyValues(f.values)
	var lots []entity.UnrealizedGainsLoss
	var realized []entity.RealizedGainsLoss
	var recorded []entity.EquitySnapshot
	if f.ledger != nil {
		lots = append(lots, f.ledger.lots...)
		realized = append(realized, f.ledger.realized...)
	}
	if f.equity != nil {
		recorded = append(recorded, f.equity.recorded...)
	}
	err := fn(ctx)
	if err == nil {
		err = f.commitErr
	}
	if err != nil {
		f.values = values
		if f.ledger != nil {
			f.ledger.lots = lots
			f.ledger.realized = realized
		}
		if f.equity != nil {
			f.equity.recorded = recorded
		}
	}
	return err
}

func TestAtomicDay_RollbackRetryAndNoPrematureNotification(t *testing.T) {
	for _, failure := range []string{"current_cash", "last_processed_date", "commit"} {
		t.Run(failure, func(t *testing.T) {
			cfg := tradingTestCfg("AAA")
			cfg.DecisionPriceBasis = "open"
			cfg.MonthlyContribution = 2500
			s, _, state, notify, ledger, stock := newTradingFixture(cfg)
			stock.names["AAA"] = "AAA"
			day := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
			state.values[stateKeyWatermark] = "2024-02-29"
			ss := flatSeries(day.AddDate(0, 0, -60), 60, 100)
			series := map[string]*trading.StockSeries{"AAA": ss}
			before := s.engine.Cash()
			if failure == "commit" {
				state.commitErr = errFake
			} else {
				state.failKey = failure
			}
			if err := s.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, true); err == nil {
				t.Fatal("expected injected failure")
			}
			if s.engine.Cash() != before || s.engine.PositionCount("AAA") != 0 || len(ledger.lots) != 0 || len(notify.tradeSent) != 0 || state.values[stateKeyWatermark] != "2024-02-29" {
				t.Fatal("failed day leaked ledger, memory, watermark or notification")
			}
			if _, ok := state.values[stateKeyTotalContributed]; ok {
				t.Fatal("failed contribution persisted")
			}
			state.commitErr = nil
			state.failKey = ""
			if err := s.reloadIfNeeded(context.Background(), series); err != nil {
				t.Fatal(err)
			}
			if err := s.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, true); err != nil {
				t.Fatal(err)
			}
			cash := s.engine.Cash()
			if len(ledger.lots) != 1 || len(notify.tradeSent) != 1 || state.values[stateKeyTotalContributed] != "2500" {
				t.Fatal("retry did not commit exactly once")
			}
			if err := s.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, true); err != nil {
				t.Fatal(err)
			}
			if s.engine.Cash() != cash || len(ledger.lots) != 1 || len(notify.tradeSent) != 1 {
				t.Fatal("duplicate day was applied")
			}
		})
	}
}

func TestCatchUp_StopsAtMissingStockDay(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	cfg.DecisionPriceBasis = "open"
	s, _, state, _, _, _ := newTradingFixture(cfg)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	state.values[stateKeyWatermark] = "2024-01-01"
	series := map[string]*trading.StockSeries{"AAA": flatSeries(start, 3, 100), "BBB": trading.NewStockSeries([]time.Time{start, start.AddDate(0, 0, 2)}, []float64{100, 100}, []float64{100, 100}, nil, nil, nil)}
	if err := s.CatchUp(context.Background(), series); err == nil {
		t.Fatal("missing day accepted")
	}
	if state.values[stateKeyWatermark] != "2024-01-01" {
		t.Fatal("watermark jumped missing day")
	}
}

func TestOpening_StaleHistoryAndPartialPricesFailClosed(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{true: "partial", false: "stale"}[partial], func(t *testing.T) {
			cfg := tradingTestCfg("AAA", "BBB")
			s, _, state, notify, ledger, _ := newTradingFixture(cfg)
			start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			day := start.AddDate(0, 0, 60)
			state.values[stateKeyWatermark] = start.AddDate(0, 0, 59).Format(dateLayout)
			n := 55
			if partial {
				n = 60
			}
			s.series = &fakeSeriesLoader{data: map[string][]entity.StockHistory{"AAA": risingHistory(start, n, 100), "BBB": risingHistory(start, n, 100)}}
			s.realtime.(*fakeRealtime).opens = map[string]float64{"AAA": 150}
			err := s.runOneDayAtOpen(context.Background(), &tradingExecutor{svc: s, notify: true}, day, true)
			if !partial && err == nil {
				t.Fatal("stale data accepted")
			}
			if len(ledger.lots) > 0 || len(notify.tradeSent) > 0 || state.values[stateKeyWatermark] == day.Format(dateLayout) {
				t.Fatal("incomplete prices committed")
			}
		})
	}
}

func TestOpening_CatchesMissedDayBeforeToday(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	s, _, state, _, _, stock := newTradingFixture(cfg)
	stock.names["AAA"] = "AAA"
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	today := start.AddDate(0, 0, 60)
	state.values[stateKeyWatermark] = today.AddDate(0, 0, -2).Format(dateLayout)
	s.series = &fakeSeriesLoader{data: map[string][]entity.StockHistory{"AAA": risingHistory(start, 60, 100)}}
	s.realtime.(*fakeRealtime).opens = map[string]float64{"AAA": 160}
	if err := s.runOneDayAtOpen(context.Background(), &tradingExecutor{svc: s}, today, false); err != nil {
		t.Fatal(err)
	}
	recorded := s.equity.(*fakeEquity).recorded
	found := false
	for _, sp := range recorded {
		if sp.Date == today.AddDate(0, 0, -1).Format(dateLayout) {
			found = true
		}
	}
	if !found || state.values[stateKeyWatermark] != today.Format(dateLayout) {
		t.Fatal("missed day not replayed")
	}
}

func TestClosedEquity_RevaluesFragmentsAndOmitsUnclosedDay(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	eq := &fakeEquity{list: []entity.EquitySnapshot{{Date: "2024-01-02", Cash: 100, CostBasis: 300}, {Date: "2024-01-03", Cash: 250, CostBasis: 200}, {Date: "2024-01-04", Cash: 250, CostBasis: 200}}}
	ledger := &fakeLedger{listU: []entity.UnrealizedGainsLoss{{TransactionDate: "2024-01-02", StockID: "AAA", Shares: 20, InvestmentCost: 200}}, listR: []entity.RealizedGainsLoss{{BuyDate: "2024-01-02", SellDate: "2024-01-03", StockID: "AAA", Shares: 10, InvestmentCost: 100}}}
	series := &fakeSeriesLoader{data: map[string][]entity.StockHistory{"AAA": {{Date: "2024-01-02", ClosePrice: 11}, {Date: "2024-01-03", ClosePrice: 12}}}}
	got, err := NewClosedEquityReader(eq, ledger, series, cfg).ListEquityAsc(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || math.Abs(got[0].TotalEquity-430) > 1e-9 || got[1].TotalEquity != 490 {
		t.Fatalf("wrong close values: %+v", got)
	}
}

func TestMonthlyBackfillDates_MonthEnd(t *testing.T) {
	got := monthlyBackfillDates("20240331", 2)
	if got[1] != "20240201" || got[2] != "20240101" {
		t.Fatalf("month rollover: %v", got)
	}
}

func TestUpdateDatabase_PropagatesFetchFailure(t *testing.T) {
	s, _, _, _, _, _ := newTradingFixture(tradingTestCfg("AAA"))
	s.market.twse = &fakeFetcher{err: errFake}
	if err := s.market.UpdateDatabase(context.Background()); err == nil {
		t.Fatal("fetch error swallowed")
	}
}

type failSecondBuy struct {
	*fakeLedger
	calls int
}

func (f *failSecondBuy) InsertUnrealized(ctx context.Context, lot entity.UnrealizedGainsLoss) error {
	f.calls++
	if f.calls == 2 {
		return errFake
	}
	return f.fakeLedger.InsertUnrealized(ctx, lot)
}

func TestAtomicDay_SecondStockFailureRollsBackFirstStock(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	s, _, state, notify, ledger, _ := newTradingFixture(cfg)
	s.portfolio.ledger = &failSecondBuy{fakeLedger: ledger}
	day := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	state.values[stateKeyWatermark] = "2024-02-29"
	series := map[string]*trading.StockSeries{"AAA": flatSeries(day.AddDate(0, 0, -60), 60, 100), "BBB": flatSeries(day.AddDate(0, 0, -60), 60, 100)}
	if err := s.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90, "BBB": 90}, true); err == nil {
		t.Fatal("expected second stock failure")
	}
	if len(ledger.lots) != 0 || s.engine.Cash() != cfg.InitialCash || len(notify.tradeSent) != 0 || state.values[stateKeyWatermark] != "2024-02-29" {
		t.Fatal("partial portfolio day leaked")
	}
}

func TestRiskCheckpoint_RestoresOpeningPeakWithoutDailyBar(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	s, seed, state, _, ledger, _ := newTradingFixture(cfg)
	day := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	state.values[stateKeyWatermark] = "2024-02-29"
	series := map[string]*trading.StockSeries{"AAA": flatSeries(day.AddDate(0, 0, -60), 60, 100)}
	if err := s.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, false); err != nil {
		t.Fatal(err)
	}
	want := s.engine.RiskState("AAA")
	cash := s.engine.Cash()
	seed.unrealized = append(seed.unrealized, ledger.lots...)
	if err := s.SeedFromDB(context.Background(), series); err != nil {
		t.Fatal(err)
	}
	if s.engine.RiskState("AAA") != want || s.engine.Cash() != cash || want.Peak != 90 {
		t.Fatal("restart lost exact risk anchors")
	}
}

func TestMaximumDrawdown_PreservesWorstLossAfterRecovery(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.InitialCash = 100
	d := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	full := backtest.WindowReport{Dates: []time.Time{d, d.AddDate(0, 0, 1), d.AddDate(0, 0, 2)}, StratCurve: []float64{100, 80, 120}, BHCurve: []float64{100, 80, 120}}
	snaps := []entity.EquitySnapshot{{Date: "2024-01-01", TotalEquity: 100}, {Date: "2024-01-02", TotalEquity: 80}, {Date: "2024-01-03", TotalEquity: 120}}
	pts := buildPerformanceHistory(cfg, full, snaps)
	if pts[2].MaxDrawdown == nil || *pts[2].MaxDrawdown != -20 || pts[2].StratDrawdown != 0 {
		t.Fatalf("maximum drawdown incorrectly recovered: %+v", pts[2])
	}
}

type quoteStock struct {
	*fakeStock
	date  string
	price float64
}

func (q quoteStock) LatestClose(context.Context, string, string) (string, float64, error) {
	return q.date, q.price, nil
}

func TestPortfolio_OpeningFillDoesNotUseYesterdayClose(t *testing.T) {
	ledger := &fakeLedger{listU: []entity.UnrealizedGainsLoss{{TransactionDate: "2026-09-30", StockID: "AAA", Shares: 100, TransactionPrice: 39.1, InvestmentCost: 3910}}}
	stock := quoteStock{fakeStock: newFakeStock(), date: "2026-09-29", price: 38.28}
	svc := NewPortfolioService(ledger, stock, newTestLogger())
	rows, err := svc.UnrealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].PriceBasis != "opening_fill" || rows[0].PriceDate != "2026-09-30" || math.Abs(rows[0].PredictProfitLoss) > 0.01 {
		t.Fatalf("new fill valued at old close: %+v", rows[0])
	}
}

func TestPortfolio_LegacyDatetimeDoesNotOverrideSameDayClose(t *testing.T) {
	ledger := &fakeLedger{listU: []entity.UnrealizedGainsLoss{{TransactionDate: "2026-09-30 00:00:00", StockID: "AAA", Shares: 100, TransactionPrice: 39.1, InvestmentCost: 3910}}}
	svc := NewPortfolioService(ledger, quoteStock{fakeStock: newFakeStock(), date: "2026-09-30", price: 40}, newTestLogger())
	rows, err := svc.UnrealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].PriceBasis != "close" || rows[0].TodayClosePrice != 40 {
		t.Fatalf("same day close overridden: %+v", rows[0])
	}
}
