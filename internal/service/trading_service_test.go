// internal/service/trading_service_test.go 驗證 TradingService 的序列載入、資料庫種子、追趕回放及執行器買賣路由與通知邏輯。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
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

// fakeSeriesLoader 模擬 SeriesLoader，以記憶體 map 依股票代碼回傳預設的歷史收盤資料。
type fakeSeriesLoader struct {
	data map[string][]entity.StockHistory
	err  error
}

// LoadSeries 回傳指定股票代碼對應的歷史資料，err 非 nil 時回傳錯誤。
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

// fakeState 模擬 StateStore，以記憶體 map 儲存鍵值並記錄所有 Set 呼叫。
type fakeState struct {
	values  map[string]string
	getErr  error
	setErr  error
	setKeys []string
}

// newFakeState 建立並回傳已初始化 values map 的 fakeState 實例。
func newFakeState() *fakeState {
	return &fakeState{values: map[string]string{}}
}

// Get 回傳指定鍵的值與是否存在，getErr 非 nil 時回傳錯誤。
func (f *fakeState) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.values[key]
	return v, ok, nil
}

// Set 記錄鍵名並將值寫入記憶體 map，setErr 非 nil 時回傳錯誤。
func (f *fakeState) Set(_ context.Context, key, value string) error {
	f.setKeys = append(f.setKeys, key)
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	return nil
}

// fakeNotifier 模擬 Notifier，記錄每次 SendEmbed / SendTradeEmbed 呼叫的參數供斷言使用。
type fakeNotifier struct {
	sent      []embedCall
	tradeSent []discord.TradeNotification
	err       error
}

// embedCall 記錄一次 SendEmbed 呼叫的標題、訊息與顏色。
type embedCall struct {
	title   string
	message string
	color   int
}

// SendEmbed 記錄通知呼叫並在 err 非 nil 時回傳錯誤。
func (f *fakeNotifier) SendEmbed(title, message string, color int) error {
	f.sent = append(f.sent, embedCall{title: title, message: message, color: color})
	return f.err
}

// SendTradeEmbed 記錄成交通知呼叫並在 err 非 nil 時回傳錯誤。
func (f *fakeNotifier) SendTradeEmbed(n discord.TradeNotification) error {
	f.tradeSent = append(f.tradeSent, n)
	return f.err
}

// fakeSeed 模擬 LedgerSeedStore，提供未實現損益清單與最後買入日期供 SeedFromDB 測試使用。
type fakeSeed struct {
	unrealized []entity.UnrealizedGainsLoss
	lastBuy    map[string]string // stockID -> raw date ("" or absent = none)
	loadErr    error
	lastBuyErr error
}

// LoadAllUnrealized 回傳預設的未實現損益清單，loadErr 非 nil 時回傳錯誤。
func (f *fakeSeed) LoadAllUnrealized(_ context.Context) ([]entity.UnrealizedGainsLoss, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.unrealized, nil
}

// LastBuyDateRaw 回傳指定股票的最後買入日期原始字串，無記錄或空字串時回傳 false。
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

// tradingTestCfg 建立符合線上演算法設定的 Config，供引擎整合測試使用。
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

// constHistory 建立從 start 起算共 n 筆、收盤價固定為 price 的歷史資料切片。
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

// flatSeries 建立價格完全平坦的 StockSeries，用於驗證追趕回放不產生任何交易。
func flatSeries(start time.Time, n int, price float64) *trading.StockSeries {
	dates := make([]time.Time, n)
	opens := make([]float64, n)
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		dates[i] = start.AddDate(0, 0, i)
		opens[i] = price
		closes[i] = price
	}
	return trading.NewStockSeries(dates, opens, closes, nil, nil, nil)
}

// newTradingFixture 組裝以假實作驅動的 TradingService，回傳服務本體與各假實作供測試斷言使用。
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
	realtime := &fakeRealtime{opens: map[string]float64{}}
	series := &fakeSeriesLoader{data: map[string][]entity.StockHistory{}}

	svc := NewTradingService(engine, portfolio, market, series, seed, state, notify, realtime, cfg, log)
	return svc, seed, state, notify, ledger, stock
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestTradingService_LoadSeries 驗證 loadSeries 將歷史資料正確轉換為 StockSeries，含日期與收盤價序列。
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

// TestTradingService_LoadSeries_DatetimeFallbackAndEmpty 驗證日期時間格式回退解析及無效日期略過，空歷史股票從結果中排除。
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

// TestTradingService_SeedFromDB 驗證 SeedFromDB 從狀態儲存還原現金、從帳冊還原持倉批次及最後買入日期至引擎。
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

// TestTradingService_SeedFromDB_NoCashFallbackAndDatetimeLot 驗證狀態無現金記錄時回退至初始資金，並正確解析日期時間格式的批次。
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

// TestTradingService_CatchUp_FlatSeriesNoTrades 驗證首次啟動且序列完全平坦時追趕回放不產生任何交易，並持久化水位線與現金。
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

// TestTradingService_CatchUp_ResumesFromWatermark 驗證存在水位線時追趕回放從該日期後繼續，並更新水位線至最新日期。
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

// TestTradingService_CatchUp_EmptySeries 驗證傳入空序列時追趕回放不持久化任何狀態。
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

// TestTradingExecutor_OnBuy_RoutesToPortfolioAndNotifies 驗證 OnBuyApplied 將批次寫入投資組合並發送買入通知嵌入訊息。
func TestTradingExecutor_OnBuy_RoutesToPortfolioAndNotifies(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, ledger, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 50.0
	stock.names["AAA"] = "name"

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)
	reason := trading.TradeReason{Action: "buy", Trigger: "dip", Regime: "bull", Price: 50.0, Shares: 10, Amount: 500, CashAfter: 1000}

	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000, reason); err != nil {
		t.Fatalf("OnBuyApplied: %v", err)
	}
	// portfolio wrote one unrealized lot.
	if len(ledger.lots) != 1 || ledger.lots[0].StockID != "AAA" || ledger.lots[0].Shares != 10 {
		t.Fatalf("buy not routed to portfolio: %+v", ledger.lots)
	}
	// notified with the beautified trade embed (buy color + reason field present).
	if len(notify.tradeSent) != 1 || notify.tradeSent[0].Color != buyColor {
		t.Fatalf("buy embed mismatch: %+v", notify.tradeSent)
	}
	if !hasReasonField(notify.tradeSent[0]) {
		t.Fatalf("buy embed missing 交易理由 field: %+v", notify.tradeSent[0].Fields)
	}
}

// hasReasonField 檢查交易通知是否含「交易理由」欄位且內容非空。
func hasReasonField(n discord.TradeNotification) bool {
	for _, f := range n.Fields {
		if f.Name == "📋 交易理由" && f.Value != "" {
			return true
		}
	}
	return false
}

// TestTradingExecutor_OnSell_RoutesToPortfolioAndNotifies 驗證 OnSellApplied 將已實現損益寫入帳冊並發送賣出通知嵌入訊息。
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
	reason := trading.TradeReason{Action: "sell", Trigger: "profit", Regime: "bull", Price: 80.0, Shares: 100, Amount: 8000, CashAfter: 9000, GainPct: 1.0}

	if err := exec.OnSellApplied("AAA", day, 100, 80.0, 9000, reason); err != nil {
		t.Fatalf("OnSellApplied: %v", err)
	}
	// portfolio recorded a realized row and removed the lot.
	if len(ledger.realized) != 1 || ledger.realized[0].Shares != 100 {
		t.Fatalf("sell not routed to portfolio: %+v", ledger.realized)
	}
	if len(notify.tradeSent) != 1 || notify.tradeSent[0].Color != sellColor {
		t.Fatalf("sell embed mismatch: %+v", notify.tradeSent)
	}
	if !hasReasonField(notify.tradeSent[0]) {
		t.Fatalf("sell embed missing 交易理由 field: %+v", notify.tradeSent[0].Fields)
	}
}

// TestTradingExecutor_Silent_WritesButNoNotify 驗證 notify=false 時交易仍寫入資料庫但不發送任何通知。
func TestTradingExecutor_Silent_WritesButNoNotify(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, ledger, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 50.0
	stock.names["AAA"] = "name"

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: false}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)
	reason := trading.TradeReason{Action: "buy", Trigger: "dip", Regime: "bear", Price: 50.0, Shares: 10, Amount: 500, CashAfter: 1000}

	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000, reason); err != nil {
		t.Fatalf("OnBuyApplied silent: %v", err)
	}
	if len(ledger.lots) != 1 {
		t.Fatalf("silent buy should still write DB: %+v", ledger.lots)
	}
	if len(notify.tradeSent) != 0 {
		t.Fatalf("silent executor must not notify, got %+v", notify.tradeSent)
	}
}

// TestTradingExecutor_NotifyFailure_NonFatal 驗證通知發送失敗時買入操作仍成功回傳，錯誤僅記錄日誌。
func TestTradingExecutor_NotifyFailure_NonFatal(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, _, notify, _, stock := newTradingFixture(cfg)
	stock.prices["AAA"] = 50.0
	stock.names["AAA"] = "name"
	notify.err = errFake // SendEmbed fails

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)
	reason := trading.TradeReason{Action: "buy", Trigger: "dip", Regime: "bull", Price: 50.0, Shares: 10, Amount: 500, CashAfter: 1000}

	// A notify failure is logged only — the buy still succeeds.
	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000, reason); err != nil {
		t.Fatalf("OnBuyApplied with failing notifier should still succeed, got %v", err)
	}
}

// TestTradingService_RunOnline_RejectsNonBaseline 驗證策略非 Baseline 時 RunOnline 回傳錯誤拒絕執行。
func TestTradingService_RunOnline_RejectsNonBaseline(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	cfg.ScalingStrategy = "Other"
	svc, _, _, _, _, _ := newTradingFixture(cfg)

	if err := svc.RunOnline(context.Background()); err == nil {
		t.Fatalf("RunOnline should reject non-Baseline strategy")
	}
}

// TestTradingService_WatermarkRoundTrip 驗證水位線的儲存與讀取往返一致，缺失時回傳零值時間。
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

// TestTradingService_CashRoundTrip 驗證現金的儲存與讀取往返一致，缺失時回傳 ok=false。
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

// risingHistory 建立由 start 起算共 n 筆、開盤=收盤皆線性上漲 (base..base+n-1) 的歷史資料 (多頭序列)。
func risingHistory(start time.Time, n int, base float64) []entity.StockHistory {
	out := make([]entity.StockHistory, n)
	for i := 0; i < n; i++ {
		px := base + float64(i)
		out[i] = entity.StockHistory{
			Date:       start.AddDate(0, 0, i).Format("2006-01-02"),
			OpenPrice:  px,
			ClosePrice: px,
		}
	}
	return out
}

// TestTradingService_RunOneDayAtOpen_DecidesAtOpenAndPersists 驗證開盤決策流程:
// 以 T-1 歷史 + 注入即時開盤價,於開盤價成交、寫帳本與發通知,並前進水位線 / 現金。
func TestTradingService_RunOneDayAtOpen_DecidesAtOpenAndPersists(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, state, notify, ledger, stock := newTradingFixture(cfg)
	stock.names["AAA"] = "測試股"

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.series = &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": risingHistory(start, 60, 100), // 收盤 100..159 (多頭)
	}}
	// 注入今日即時開盤價 160 (< MA10(asOf)×1.05 → 觸發逢低買入)。
	svc.realtime.(*fakeRealtime).opens = map[string]float64{"AAA": 160}
	today := start.AddDate(0, 0, 60)

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	if err := svc.runOneDayAtOpen(context.Background(), exec, today, false); err != nil {
		t.Fatalf("runOneDayAtOpen: %v", err)
	}

	// 一筆買入寫入帳本,成交價為注入的開盤價 160 (而非 DB 收盤)。
	if len(ledger.lots) != 1 || ledger.lots[0].TransactionPrice != 160 {
		t.Fatalf("expected one buy lot at open price 160, got %+v", ledger.lots)
	}
	// 發出一則美化的買入通知 (含理由欄位)。
	if len(notify.tradeSent) != 1 || notify.tradeSent[0].Color != buyColor || !hasReasonField(notify.tradeSent[0]) {
		t.Fatalf("expected one buy trade embed with reason, got %+v", notify.tradeSent)
	}
	// 水位線前進至今日、現金已持久化。
	if state.values["last_processed_date"] != today.Format("2006-01-02") {
		t.Fatalf("watermark not advanced to today, got %q", state.values["last_processed_date"])
	}
	if _, ok := state.values["current_cash"]; !ok {
		t.Fatalf("cash not persisted")
	}
}

// TestTradingService_RunOneDayAtOpen_NotReadyDoesNotAdvance 驗證開盤價未就緒且非 force 時不下單、不前進水位線。
func TestTradingService_RunOneDayAtOpen_NotReadyDoesNotAdvance(t *testing.T) {
	cfg := tradingTestCfg("AAA")
	svc, _, state, notify, ledger, _ := newTradingFixture(cfg)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.series = &fakeSeriesLoader{data: map[string][]entity.StockHistory{
		"AAA": risingHistory(start, 60, 100),
	}}
	// 無即時開盤價 (尚未就緒) + force=false → 應略過且不前進水位線。
	svc.realtime.(*fakeRealtime).opens = map[string]float64{}
	today := start.AddDate(0, 0, 60)

	exec := &tradingExecutor{svc: svc, ctx: context.Background(), notify: true}
	if err := svc.runOneDayAtOpen(context.Background(), exec, today, false); err != nil {
		t.Fatalf("runOneDayAtOpen: %v", err)
	}
	if len(ledger.lots) != 0 || len(notify.tradeSent) != 0 {
		t.Fatalf("not-ready should not trade, lots=%+v sent=%+v", ledger.lots, notify.tradeSent)
	}
	if _, ok := state.values["last_processed_date"]; ok {
		t.Fatalf("not-ready must not advance watermark")
	}
}

// TestInOpenDecisionWindow 驗證開盤決策時段 [09:10, 09:30) 的邊界判斷。
func TestInOpenDecisionWindow(t *testing.T) {
	tz, _ := time.LoadLocation("Asia/Taipei")
	at := func(h, m int) time.Time { return time.Date(2026, 6, 8, h, m, 0, 0, tz) }
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(9, 9), false},
		{at(9, 10), true},
		{at(9, 29), true},
		{at(9, 30), false},
		{at(14, 0), false},
	}
	for _, c := range cases {
		if got := inOpenDecisionWindow(c.t); got != c.want {
			t.Fatalf("inOpenDecisionWindow(%s) = %v, want %v", c.t.Format("15:04"), got, c.want)
		}
	}
}
