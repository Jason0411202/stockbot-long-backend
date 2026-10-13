// internal/service/trading/historical_observation_test.go 驗證歷史觀察只採帳本成交且保留正確條件與狀態。
package trading

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// historicalTestSeries 建立包含決策日與次日的可見行情。
func historicalTestSeries(day time.Time, ids ...string) map[string]*StockSeries {
	out := make(map[string]*StockSeries)
	for _, id := range ids {
		out[id] = seriesFrom(day.AddDate(0, 0, -60), constPrices(62, 100))
	}
	return out
}

// TestHistoricalObservation_RecordedBuyOverridesCurrentRules 驗證冷卻中的真實買入仍影響賣出觀察。
func TestHistoricalObservation_RecordedBuyOverridesCurrentRules(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA")
	cfg.InitialCash, cfg.DecisionPriceBasis = 10000, "open"
	engine := NewEngine(cfg)
	engine.SeedPosition("AAA", day.AddDate(0, 0, -20), 10, 100)
	engine.SeedRiskState("AAA", RiskState{LastBuy: day.AddDate(0, 0, -1), Peak: 120})
	callbacks := 0
	engine.SetDecisionObserver(func(DecisionObservation) { callbacks++ })
	engine.SetRecorder(&DayRecorder{OnCashflow: func(time.Time, float64) { callbacks++ }, OnEquity: func(time.Time, float64, float64, float64) { callbacks++ }})
	beforeStats := engine.Stats()
	actual := []HistoricalTrade{{StockID: "AAA", Buy: true, Shares: 3, Price: 90}}
	got, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA"), actual)
	if err != nil || len(got) != 1 {
		t.Fatalf("observations=%+v err=%v", got, err)
	}
	o := got[0]
	if o.Buy.IntendedShares != 0 || o.Buy.ExecutedShares != 3 || o.Buy.Status != "executed" || observedCondition(t, o.Buy, "buy_cooldown").Status != "blocked" {
		t.Fatalf("recorded trade was replaced by current intent: %+v", o.Buy)
	}
	if o.Sell.HeldSharesBefore != 13 || o.Sell.HeldSharesAfter != 13 || o.Sell.ExecutedShares != 0 || o.Sell.IntendedShares != 13 {
		t.Fatalf("sell input contains simulated positions: %+v", o.Sell)
	}
	if c := observedCondition(t, o.Sell, "peak_gain"); c.Value == nil || math.Abs(*c.Value-(120.0/90-1)*100) > 1e-9 {
		t.Fatalf("sell did not include actual new low-cost lot: %+v", c)
	}
	if engine.Cash() != 9730 || o.Sell.CashBefore != 9730 || callbacks != 0 || engine.Stats() != beforeStats {
		t.Fatalf("unexpected side effects cash=%v callbacks=%v stats=%+v", engine.Cash(), callbacks, engine.Stats())
	}
	if last, ok := engine.LastBuy("AAA"); !ok || !last.Equal(day) {
		t.Fatalf("actual buy date was not advanced: %v", last)
	}
	engine.observer(DecisionObservation{})
	engine.rec.OnCashflow(day, 0)
	if callbacks != 2 {
		t.Fatal("historical observation replaced external callbacks")
	}
}

// TestHistoricalObservation_DoesNotApplyHypotheticalBuy 驗證現行規則建議買入但無帳本成交時不增加持倉。
func TestHistoricalObservation_DoesNotApplyHypotheticalBuy(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA")
	cfg.InitialCash, cfg.DecisionPriceBasis = 10000, "open"
	engine := NewEngine(cfg)
	series := historicalTestSeries(day, "AAA")
	series["AAA"].OpenPrices[60] = 90
	got, err := engine.ObserveHistoricalDay(day, series, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Buy.IntendedShares <= 0 || got[0].Buy.ExecutedShares != 0 || got[0].Sell.HeldSharesBefore != 0 || got[0].Sell.Status != "inactive" || engine.Cash() != 10000 || engine.PositionCount("AAA") != 0 {
		t.Fatalf("hypothetical buy changed historical sell state: %+v", got[0])
	}
}

// TestHistoricalObservation_SameDayBuyAndLotSales 驗證先買再賣、指定批次部分出售與完整出清。
func TestHistoricalObservation_SameDayBuyAndLotSales(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA")
	cfg.InitialCash, cfg.DecisionPriceBasis, cfg.TrailReentryCooldownDays = 10000, "open", 42
	engine := NewEngine(cfg)
	old := day.AddDate(0, 0, -20)
	newer := day.AddDate(0, 0, -10)
	engine.SeedPosition("AAA", old, 10, 100)
	engine.SeedPosition("AAA", newer, 7, 80)
	engine.SeedRiskState("AAA", RiskState{Peak: 120})
	series := historicalTestSeries(day, "AAA")
	actual := []HistoricalTrade{
		{StockID: "AAA", Buy: false, Shares: 3, Price: 90, BuyDate: old},
		{StockID: "AAA", Buy: false, Shares: 2, Price: 90, BuyDate: day},
		{StockID: "AAA", Buy: true, Shares: 2, Price: 90},
	}
	got, err := engine.ObserveHistoricalDay(day, series, actual)
	if err != nil {
		t.Fatal(err)
	}
	o := got[0]
	if o.Buy.HeldSharesBefore != 17 || o.Buy.HeldSharesAfter != 19 || o.Sell.HeldSharesBefore != 19 || o.Sell.HeldSharesAfter != 14 || o.Sell.ExecutedShares != 5 || engine.Cash() != 10270 {
		t.Fatalf("same-day phase state incorrect: %+v", o)
	}
	if len(engine.positions["AAA"]) != 2 || engine.positions["AAA"][0].shares != 7 || engine.positions["AAA"][1].shares != 7 || engine.PeakSinceHold("AAA") != 120 {
		t.Fatalf("sold from wrong lot or reset active peak: %+v", engine.positions["AAA"])
	}
	if last, ok := engine.LastTrailSell("AAA"); !ok || !last.Equal(day) {
		t.Fatalf("last sell=%v", last)
	}
	next := day.AddDate(0, 0, 1)
	got, err = engine.ObserveHistoricalDay(next, series, []HistoricalTrade{
		{StockID: "AAA", Shares: 7, Price: 90, BuyDate: old},
		{StockID: "AAA", Shares: 7, Price: 90, BuyDate: newer},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := observedCondition(t, got[0].Buy, "buy_cooldown"); c.RemainingDays == nil || *c.RemainingDays != 13 {
		t.Fatalf("buy cooldown=%+v", c)
	}
	if c := observedCondition(t, got[0].Buy, "reentry_cooldown"); c.RemainingDays == nil || *c.RemainingDays != 41 {
		t.Fatalf("reentry cooldown=%+v", c)
	}
	if got[0].Sell.HeldSharesAfter != 0 || engine.PositionCount("AAA") != 0 || engine.PeakSinceHold("AAA") != 0 || engine.Cash() != 11530 {
		t.Fatalf("full exit failed: %+v cash=%v", got[0], engine.Cash())
	}
}

// TestHistoricalObservation_UsesConfiguredOrderAndSharedCash 驗證跨股共用現金按設定順序流動。
func TestHistoricalObservation_UsesConfiguredOrderAndSharedCash(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("BBB", "AAA")
	cfg.InitialCash, cfg.DecisionPriceBasis = 0, "open"
	engine := NewEngine(cfg)
	old := day.AddDate(0, 0, -20)
	engine.SeedPosition("BBB", old, 10, 100)
	actual := []HistoricalTrade{{StockID: "AAA", Buy: true, Shares: 5, Price: 100}, {StockID: "BBB", Shares: 10, Price: 100, BuyDate: old}}
	got, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA", "BBB"), actual)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].StockID != "BBB" || got[1].StockID != "AAA" || got[1].Buy.CashBefore != 1000 || engine.Cash() != 500 {
		t.Fatalf("did not preserve configured cash order: %+v", got)
	}
}

// TestHistoricalObservation_RecordedAmountsPreserveCash 驗證帳本價格取整不會改變真實成交流水。
func TestHistoricalObservation_RecordedAmountsPreserveCash(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA", "BBB")
	cfg.InitialCash, cfg.DecisionPriceBasis = 28.97, "open"
	engine := NewEngine(cfg)
	old := day.AddDate(0, 0, -20)
	engine.SeedPosition("AAA", old, 40469, 5.65)
	actual := []HistoricalTrade{
		{StockID: "AAA", Shares: 40469, Price: 5.65, Amount: 228562.14, BuyDate: old},
		{StockID: "BBB", Buy: true, Shares: 8000, Price: 5.71, Amount: 45725.88},
	}
	got, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA", "BBB"), actual)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got[0].Sell.CashBefore-28.97) > 1e-8 || math.Abs(got[0].Sell.CashAfter-228591.11) > 1e-8 || math.Abs(got[1].Buy.CashBefore-228591.11) > 1e-8 || math.Abs(got[1].Buy.CashAfter-182865.23) > 1e-8 || math.Abs(engine.Cash()-182865.23) > 1e-8 {
		t.Fatalf("rounded quote replaced actual cash flows: observations=%+v cash=%.8f", got, engine.Cash())
	}
	if *got[0].Price != 5.65 || *got[1].Price != 5.71 || engine.positions["BBB"][0].price != 5.71 || got[0].Sell.ExecutedShares != 40469 || got[1].Buy.ExecutedShares != 8000 {
		t.Fatalf("cash-flow amount changed decision prices, cost or shares: %+v", got)
	}
}

// TestHistoricalObservation_ZeroAmountFallsBackToPrice 驗證未提供現金流時維持股數乘價格的相容行為。
func TestHistoricalObservation_ZeroAmountFallsBackToPrice(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA", "BBB")
	cfg.InitialCash, cfg.DecisionPriceBasis = 10, "open"
	engine := NewEngine(cfg)
	old := day.AddDate(0, 0, -20)
	engine.SeedPosition("AAA", old, 2, 5.5)
	actual := []HistoricalTrade{
		{StockID: "AAA", Shares: 2, Price: 5.5, Amount: 0, BuyDate: old},
		{StockID: "BBB", Buy: true, Shares: 3, Price: 4, Amount: 0},
	}
	got, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA", "BBB"), actual)
	if err != nil || engine.Cash() != 9 || got[0].Sell.CashAfter != 21 || got[1].Buy.CashAfter != 9 {
		t.Fatalf("zero amount fallback failed: err=%v observations=%+v cash=%v", err, got, engine.Cash())
	}
}

// TestHistoricalObservation_InvalidAmountsAreAtomic 驗證無效現金流不改變狀態或觸發外部回呼。
func TestHistoricalObservation_InvalidAmountsAreAtomic(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, buy := range []bool{true, false} {
			cfg := baseCfg("AAA", "BBB")
			cfg.InitialCash, cfg.DecisionPriceBasis = 1000, "open"
			engine := NewEngine(cfg)
			old := day.AddDate(0, 0, -20)
			engine.SeedPosition("BBB", old, 10, 100)
			engine.SeedRiskState("BBB", RiskState{LastBuy: old, Peak: 120})
			callbacks := 0
			engine.SetDecisionObserver(func(DecisionObservation) { callbacks++ })
			engine.SetRecorder(&DayRecorder{OnCashflow: func(time.Time, float64) { callbacks++ }, OnEquity: func(time.Time, float64, float64, float64) { callbacks++ }})
			before := engine.Clone()
			actual := []HistoricalTrade{
				{StockID: "AAA", Buy: true, Shares: 1, Price: 90, Amount: 89.99},
				{StockID: "BBB", Buy: buy, Shares: 1, Price: 90, Amount: amount, BuyDate: old},
			}
			if buy {
				actual[1].BuyDate = day
			}
			got, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA", "BBB"), actual)
			if err == nil || got != nil || callbacks != 0 || !reflect.DeepEqual(engine.Clone(), before) {
				t.Fatalf("invalid amount affected state: amount=%v buy=%v err=%v callbacks=%d", amount, buy, err, callbacks)
			}
		}
	}
}

// TestHistoricalObservation_InvalidTradesAreAtomic 驗證資料錯誤與資金不足不留下部分狀態。
func TestHistoricalObservation_InvalidTradesAreAtomic(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	old := day.AddDate(0, 0, -20)
	cases := map[string][]HistoricalTrade{
		"zero shares":     {{StockID: "AAA", Buy: true, Price: 90}},
		"negative shares": {{StockID: "AAA", Buy: true, Shares: -1, Price: 90}},
		"invalid price":   {{StockID: "AAA", Buy: true, Shares: 1, Price: math.Inf(1)}},
		"unknown stock":   {{StockID: "CCC", Buy: true, Shares: 1, Price: 90}},
		"different price": {{StockID: "AAA", Buy: true, Shares: 1, Price: 90}, {StockID: "AAA", Shares: 1, Price: 91, BuyDate: old}},
		"missing lot":     {{StockID: "AAA", Shares: 1, Price: 90}},
		"wrong lot":       {{StockID: "AAA", Shares: 1, Price: 90, BuyDate: old.AddDate(0, 0, 1)}},
		"excess sale":     {{StockID: "AAA", Shares: 11, Price: 90, BuyDate: old}},
		"future buy":      {{StockID: "AAA", Buy: true, Shares: 1, Price: 90, BuyDate: day.AddDate(0, 0, 1)}},
		"overdraw":        {{StockID: "AAA", Buy: true, Shares: 12, Price: 90}},
		"later failure":   {{StockID: "AAA", Buy: true, Shares: 1, Price: 90}, {StockID: "BBB", Shares: 1, Price: 90, BuyDate: old}},
	}
	for name, actual := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := baseCfg("AAA", "BBB")
			cfg.InitialCash, cfg.DecisionPriceBasis = 1000, "open"
			engine := NewEngine(cfg)
			engine.SeedPosition("AAA", old, 10, 100)
			engine.SeedRiskState("AAA", RiskState{LastBuy: old, Peak: 120})
			before := engine.Clone()
			got, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA", "BBB"), actual)
			if err == nil || got != nil || !reflect.DeepEqual(engine, before) {
				t.Fatalf("invalid history changed state: err=%v got=%+v engine=%+v before=%+v", err, got, engine, before)
			}
		})
	}
}

// TestHistoricalObservation_CashRounding 驗證僅容忍現金分位捨入差異。
func TestHistoricalObservation_CashRounding(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	for _, cash := range []float64{99.991, 99.98, math.NaN()} {
		cfg := baseCfg("AAA")
		cfg.InitialCash, cfg.DecisionPriceBasis = cash, "open"
		engine := NewEngine(cfg)
		_, err := engine.ObserveHistoricalDay(day, historicalTestSeries(day, "AAA"), []HistoricalTrade{{StockID: "AAA", Buy: true, Shares: 1, Price: 100}})
		if cash == 99.991 {
			if err != nil || engine.Cash() != 0 {
				t.Fatalf("cent rounding rejected: cash=%v err=%v", engine.Cash(), err)
			}
		} else if err == nil {
			t.Fatalf("invalid cash %v accepted", cash)
		}
	}
}

// TestHistoricalObservation_MissingPricesAndPriorIndicators 驗證價格缺值與前一交易日指標的語意。
func TestHistoricalObservation_MissingPricesAndPriorIndicators(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	engine := NewEngine(cfg)
	series := map[string]*StockSeries{"AAA": seriesFrom(day.AddDate(0, 0, -60), constPrices(60, 100))}
	got, err := engine.ObserveHistoricalDay(day, series, nil)
	if err != nil || got[0].Status != "unavailable" || got[0].Price != nil {
		t.Fatalf("missing open must be unavailable: %+v err=%v", got, err)
	}
	got, err = engine.ObserveHistoricalDay(day, series, []HistoricalTrade{{StockID: "AAA", Buy: true, Shares: 1, Price: 90}})
	if err != nil || *got[0].Price != 90 || !got[0].IndicatorDate.Equal(day.AddDate(0, 0, -1)) {
		t.Fatalf("recorded opening fill must use prior closes: %+v err=%v", got, err)
	}
	full := historicalTestSeries(day, "AAA")
	full["AAA"].OpenPrices = nil
	got, err = engine.ObserveHistoricalDay(day, full, nil)
	if err != nil || got[0].Status != "unavailable" {
		t.Fatalf("missing opening price fell back to close: %+v err=%v", got, err)
	}
	_, err = engine.ObserveHistoricalDay(day, nil, []HistoricalTrade{{StockID: "AAA", Buy: true, Shares: 1, Price: 90}})
	if err == nil {
		t.Fatal("recorded trade without indicator series must fail")
	}
}

// TestHistoricalObservation_EquityUsesPreviousClose 驗證歷史預算估值不讀取當日未來收盤。
func TestHistoricalObservation_EquityUsesPreviousClose(t *testing.T) {
	day := mustDate(t, "2026-10-13")
	cfg := baseCfg("AAA")
	cfg.InitialCash, cfg.DecisionPriceBasis, cfg.BuyFracBasis = 1000, "open", "equity"
	engine := NewEngine(cfg)
	engine.SeedPosition("AAA", day.AddDate(0, 0, -20), 10, 100)
	series := historicalTestSeries(day, "AAA")
	series["AAA"].ClosePrices[60] = 999
	got, err := engine.ObserveHistoricalDay(day, series, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range got[0].Buy.Metrics {
		if metric.ID == "budget_basis" {
			if metric.Value == nil || *metric.Value != 2000 {
				t.Fatalf("equity includes future data: %+v", metric)
			}
			return
		}
	}
	t.Fatal("missing equity budget metric")
}
