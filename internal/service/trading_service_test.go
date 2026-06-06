package service

import (
	"context"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// trading_service_test.go mirrors kernals/kernals_online_test.go using in-memory
// fakes for SeriesLoader / StateStore / Notifier / LedgerSeedStore, alongside a
// real *trading.Engine and a real *PortfolioService backed by fakes. It covers
// SeedFromDB (cash + positions + last-buy restore), CatchUp (watermark-driven
// replay) and the executor routing buys/sells to the portfolio + notifying.

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakeSeriesLoader is an in-memory SeriesLoader keyed by stockID.
type fakeSeriesLoader struct {
	data map[string][]entity.StockHistory
	err  error
}

func (f *fakeSeriesLoader) LoadSeries(_ context.Context, stockIDs []string) (map[string][]entity.StockHistory, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string][]entity.StockHistory, len(stockIDs))
	for _, id := range stockIDs {
		out[id] = f.data[id]
	}
	return out, nil
}

// fakeState is an in-memory StateStore recording every Set call.
type fakeState struct {
	values  map[string]string
	getErr  error
	setErr  error
	setKeys []string
}

func newFakeState() *fakeState {
	return &fakeState{values: map[string]string{}}
}

func (f *fakeState) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.values[key]
	return v, ok, nil
}

func (f *fakeState) Set(_ context.Context, key, value string) error {
	f.setKeys = append(f.setKeys, key)
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	return nil
}

// fakeNotifier records every embed sent.
type fakeNotifier struct {
	sent []embedCall
	err  error
}

type embedCall struct {
	title   string
	message string
	color   int
}

func (f *fakeNotifier) SendEmbed(title, message string, color int) error {
	f.sent = append(f.sent, embedCall{title: title, message: message, color: color})
	return f.err
}

// fakeSeed is an in-memory LedgerSeedStore for SeedFromDB.
type fakeSeed struct {
	unrealized []entity.UnrealizedGainsLoss
	lastBuy    map[string]string // stockID -> raw date ("" or absent = none)
	loadErr    error
	lastBuyErr error
}

func (f *fakeSeed) LoadAllUnrealized(_ context.Context) ([]entity.UnrealizedGainsLoss, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.unrealized, nil
}

func (f *fakeSeed) LastBuyDateRaw(_ context.Context, stockID string) (string, bool, error) {
	if f.lastBuyErr != nil {
		return "", false, f.lastBuyErr
	}
	v, ok := f.lastBuy[stockID]
	if !ok || v == "" {
		return "", false, nil
	}
	return v, true, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// tradingTestCfg returns a "live algorithm"-style config (mirrors the trading
// package baseCfg) for engine integration. The flat-series tests rely on the same
// no-trade behaviour the kernals online tests asserted.
func tradingTestCfg(stocks ...string) *config.Config {
	if len(stocks) == 0 {
		stocks = []string{"AAA"}
	}
	return &config.Config{
		TrackStocks:             stocks,
		ScalingStrategy:         "Baseline",
		InitialCash:             1_000_000,
		MAWindow:                10,
		RegimeMethod:            "ma_pos",
		RegimeMAWindow:          50,
		CooldownDays:            14,
		BullCooldownDays:        14,
		BullBuyBand:             0.05,
		BuyFracBasis:            "cash",
		BullBuyFrac:             0.20,
		BearBuyFrac:             0.02,
		BuyTierRatio:            2.5,
		BuyDepthBasis:           "peak",
		BuyPeakLookback:         252,
		BaselineBuyTiers:        []config.BaselineBuyTier{{Above: -0.1}, {Above: -0.2}, {Above: -0.3}, {Above: -0.4}},
		BaselineSellThreshold:   1.0,
		SellFracOfPosition:      0.33,
		TrailStopBear:           0.10,
		TrailMinGain:            0.10,
		CooldownBreakWindowDays: 365,
	}
}

// constHistory builds n ascending daily (date, close) bars starting at start.
func constHistory(start time.Time, n int, price float64) []entity.StockHistory {
	out := make([]entity.StockHistory, n)
	for i := 0; i < n; i++ {
		out[i] = entity.StockHistory{
			Date:       start.AddDate(0, 0, i).Format("2006-01-02"),
			ClosePrice: price,
		}
	}
	return out
}

// flatSeries builds a flat *trading.StockSeries (no trades) for catch-up tests.
func flatSeries(start time.Time, n int, price float64) *trading.StockSeries {
	dates := make([]time.Time, n)
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		dates[i] = start.AddDate(0, 0, i)
		closes[i] = price
	}
	return trading.NewStockSeries(dates, closes, nil, nil, nil)
}

// newTradingFixture wires a TradingService with a real engine + real portfolio
// (over fakes) and the injectable seed/series/state/notify fakes.
func newTradingFixture(cfg *config.Config) (*TradingService, *fakeSeed, *fakeState, *fakeNotifier, *fakeLedger, *fakeStock) {
	log := newTestLogger()
	stock := newFakeStock()
	ledger := &fakeLedger{}
	portfolio := NewPortfolioService(ledger, stock, log)
	market := NewMarketDataService(&fakeFetcher{}, stock, newFakeBackfill(), cfg, log)
	engine := trading.NewEngine(cfg)
	seed := &fakeSeed{lastBuy: map[string]string{}}
	state := newFakeState()
	notify := &fakeNotifier{}
	series := &fakeSeriesLoader{data: map[string][]entity.StockHistory{}}

	svc := NewTradingService(engine, portfolio, market, series, seed, state, notify, cfg, log)
	return svc, seed, state, notify, ledger, stock
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestTradingService_LoadSeries(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, _, _, _ := newTradingFixture(cfg)
	svc.series = &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": {
			{Date: "2024-01-02", ClosePrice: 50.0},
			{Date: "2024-01-03", ClosePrice: 51.0},
			{Date: "2024-01-04", ClosePrice: 52.0},
		},
	}}

	series, err := svc.loadSeries(context.Background())
	if err != nil {
		t.Fatalf("loadSeries: %v", err)
	}
	s := series["AAA"]
	if s == nil || len(s.Dates) != 3 || s.ClosePrices[2] != 52.0 {
		t.Fatalf("series misbuilt: %+v", s)
	}
}

func TestTradingService_LoadSeries_DatetimeFallbackAndEmpty(t *testing.T) {
	cfg := tradingTestCfg("AAA", "BBB")
	svc, _, _, _, _, _ := newTradingFixture(cfg)
	svc.series = &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": {
			{Date: "2024-01-02 00:00:00", ClosePrice: 50.0}, // datetime layout fallback
			{Date: "bad-date", ClosePrice: 99.0},            // skipped
			{Date: "2024-01-03", ClosePrice: 51.0},
		},
		"BBB": {}, // empty -> warned + skipped
	}}

	series, err := svc.loadSeries(context.Background())
	if err != nil {
		t.Fatalf("loadSeries: %v", err)
	}
	if _, ok := series["BBB"]; ok {
		t.Fatalf("empty-history stock should be skipped")
	}
	s := series["AAA"]
	if s == nil || len(s.Dates) != 2 { // bad-date dropped
		t.Fatalf("datetime fallback / bad-date drop failed: %+v", s)
	}
}

func TestTradingService_SeedFromDB(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, seed, state, _, _, _ := newTradingFixture(cfg)
	state.values["current_cash"] = "54321"
	seed.unrealized = []entity.UnrealizedGainsLoss{
		{TransactionDate: "2024-01-02", StockID: "AAA", TransactionPrice: 50.0, Shares: 100},
	}
	seed.lastBuy["AAA"] = "2024-01-02"

	if err := svc.SeedFromDB(context.Background()); err != nil {
		t.Fatalf("SeedFromDB: %v", err)
	}
	if svc.engine.Cash() != 54321 {
		t.Fatalf("cash not seeded: %.2f", svc.engine.Cash())
	}
	if lb, ok := svc.engine.LastBuy("AAA"); !ok || lb.Format("2006-01-02") != "2024-01-02" {
		t.Fatalf("lastBuy not seeded: %v %v", lb, ok)
	}
	if svc.engine.PositionCount("AAA") != 1 {
		t.Fatalf("position not seeded: count=%d", svc.engine.PositionCount("AAA"))
	}
}

func TestTradingService_SeedFromDB_NoCashFallbackAndDatetimeLot(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, seed, _, _, _, _ := newTradingFixture(cfg)
	// no current_cash in state -> fall back to InitialCash
	seed.unrealized = []entity.UnrealizedGainsLoss{
		{TransactionDate: "2024-01-02 00:00:00", StockID: "AAA", TransactionPrice: 50.0, Shares: 100}, // datetime
	}
	// no last-buy entry for AAA

	if err := svc.SeedFromDB(context.Background()); err != nil {
		t.Fatalf("SeedFromDB: %v", err)
	}
	if svc.engine.Cash() != cfg.InitialCash {
		t.Fatalf("cash should fall back to InitialCash, got %.2f", svc.engine.Cash())
	}
	if svc.engine.PositionCount("AAA") != 1 {
		t.Fatalf("datetime-format lot not seeded: count=%d", svc.engine.PositionCount("AAA"))
	}
	if _, ok := svc.engine.LastBuy("AAA"); ok {
		t.Fatalf("lastBuy should be absent when no record")
	}
}

func TestTradingService_CatchUp_FlatSeriesNoTrades(t *testing.T) {
	// watermark absent (first start) -> catch-up from common issuance; flat -> no trades.
	cfg := tradingTestCfg("AAA")
	svc, _, state, _, _, _ := newTradingFixture(cfg)
	series := map[string]*trading.StockSeries{
		"AAA": flatSeries(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 60, 100),
	}

	if err := svc.CatchUp(context.Background(), series); err != nil {
		t.Fatalf("CatchUp: %v", err)
	}
	if st := svc.engine.Stats(); st.TotalBuys != 0 {
		t.Fatalf("flat catch-up should not trade, got %+v", st)
	}
	// watermark + cash persisted.
	if _, ok := state.values["last_processed_date"]; !ok {
		t.Fatalf("watermark not persisted")
	}
	if _, ok := state.values["current_cash"]; !ok {
		t.Fatalf("cash not persisted")
	}
}

func TestTradingService_CatchUp_ResumesFromWatermark(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, state, _, _, _ := newTradingFixture(cfg)
	state.values["last_processed_date"] = "2024-01-30"
	series := map[string]*trading.StockSeries{
		"AAA": flatSeries(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 60, 100),
	}

	if err := svc.CatchUp(context.Background(), series); err != nil {
		t.Fatalf("CatchUp resume: %v", err)
	}
	// new watermark must be the last series date (after 2024-01-30).
	wm := state.values["last_processed_date"]
	if wm <= "2024-01-30" {
		t.Fatalf("watermark not advanced past resume point: %q", wm)
	}
}

func TestTradingService_CatchUp_EmptySeries(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, state, _, _, _ := newTradingFixture(cfg)

	if err := svc.CatchUp(context.Background(), map[string]*trading.StockSeries{}); err != nil {
		t.Fatalf("CatchUp empty: %v", err)
	}
	if len(state.setKeys) != 0 {
		t.Fatalf("empty series should not persist state, got %v", state.setKeys)
	}
}

func TestTradingExecutor_OnBuy_RoutesToPortfolioAndNotifies(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, ledger, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 50.0
	stock.names["AAA"] = "name"

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)

	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000); err != nil {
		t.Fatalf("OnBuyApplied: %v", err)
	}
	// portfolio wrote one unrealized lot.
	if len(ledger.lots) != 1 || ledger.lots[0].StockID != "AAA" || ledger.lots[0].Shares != 10 {
		t.Fatalf("buy not routed to portfolio: %+v", ledger.lots)
	}
	// notified with the buy embed (title/color).
	if len(notify.sent) != 1 || notify.sent[0].title != "🔴 買入通知" || notify.sent[0].color != 0xD50000 {
		t.Fatalf("buy embed mismatch: %+v", notify.sent)
	}
}

func TestTradingExecutor_OnSell_RoutesToPortfolioAndNotifies(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, ledger, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 80.0
	// seed a lot to sell against.
	ledger.lots = []entity.UnrealizedGainsLoss{
		{TransactionDate: "2024-01-02", StockID: "AAA", StockName: "n", TransactionPrice: 50.0, InvestmentCost: 5000.0, Shares: 100},
	}

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)

	if err := exec.OnSellApplied("AAA", day, 100, 80.0, 9000); err != nil {
		t.Fatalf("OnSellApplied: %v", err)
	}
	// portfolio recorded a realized row and removed the lot.
	if len(ledger.realized) != 1 || ledger.realized[0].Shares != 100 {
		t.Fatalf("sell not routed to portfolio: %+v", ledger.realized)
	}
	if len(notify.sent) != 1 || notify.sent[0].title != "🟢 賣出通知" || notify.sent[0].color != 0x00C853 {
		t.Fatalf("sell embed mismatch: %+v", notify.sent)
	}
}

func TestTradingExecutor_Silent_WritesButNoNotify(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, ledger, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 50.0
	stock.names["AAA"] = "name"

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: false}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)

	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000); err != nil {
		t.Fatalf("OnBuyApplied silent: %v", err)
	}
	if len(ledger.lots) != 1 {
		t.Fatalf("silent buy should still write DB: %+v", ledger.lots)
	}
	if len(notify.sent) != 0 {
		t.Fatalf("silent executor must not notify, got %+v", notify.sent)
	}
}

func TestTradingExecutor_NotifyFailure_NonFatal(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, _, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 50.0
	stock.names["AAA"] = "name"
	notify.err = errFake // SendEmbed fails

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)

	// A notify failure is logged only — the buy still succeeds.
	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000); err != nil {
		t.Fatalf("OnBuyApplied with failing notifier should still succeed, got %v", err)
	}
}

func TestTradingService_RunOnline_RejectsNonBaseline(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.ScalingStrategy = "Other"
	svc, _, _, _, _, _ := newTradingFixture(cfg)

	if err := svc.RunOnline(context.Background()); err == nil {
		t.Fatalf("RunOnline should reject non-Baseline strategy")
	}
}

func TestTradingService_WatermarkRoundTrip(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, _, _, _ := newTradingFixture(cfg)
	ctx := context.Background()

	// absent -> zero time.
	wm, err := svc.loadWatermark(ctx)
	if err != nil || !wm.IsZero() {
		t.Fatalf("missing watermark should be zero time: %v %v", wm, err)
	}

	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	if err := svc.saveWatermark(ctx, day); err != nil {
		t.Fatalf("saveWatermark: %v", err)
	}
	got, err := svc.loadWatermark(ctx)
	if err != nil || got.Format("2006-01-02") != "2024-03-15" {
		t.Fatalf("watermark round-trip mismatch: %v %v", got, err)
	}
}

func TestTradingService_CashRoundTrip(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, _, _, _ := newTradingFixture(cfg)
	ctx := context.Background()

	// absent -> (0, false).
	if _, ok, err := svc.loadCash(ctx); err != nil || ok {
		t.Fatalf("missing cash should report ok=false: %v %v", ok, err)
	}

	if err := svc.saveCash(ctx, 12345.67); err != nil {
		t.Fatalf("saveCash: %v", err)
	}
	got, ok, err := svc.loadCash(ctx)
	if err != nil || !ok || got != 12345.67 {
		t.Fatalf("cash round-trip mismatch: %v %v %v", got, ok, err)
	}
}
