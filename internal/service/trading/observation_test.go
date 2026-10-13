// internal/service/trading/observation_test.go 驗證監控對齊決策邊界且不影響交易結果。
package trading

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
)

// observedCondition 取得指定條件並在不存在時中止測試。
func observedCondition(t *testing.T, p DecisionPhase, id string) DecisionCondition {
	t.Helper()
	for _, c := range p.Conditions {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("missing condition %s", id)
	return DecisionCondition{}
}

// TestObservation_BuyBoundaries 驗證嚴格進場邊界與日曆冷卻邊界。
func TestObservation_BuyBoundaries(t *testing.T) {
	cfg := baseCfg("AAA")
	cfg.TrailReentryCooldownDays = 42
	engine := NewEngine(cfg)
	day := mustDate(t, "2026-10-13")
	snap := Snapshot{StockID: "AAA", Today: day, TodayPrice: 100, MA20: 100, Cash: 10000, RecentPeak: 100, LowestHeldPrice: -1}
	if c := observedCondition(t, engine.observeBuy(cfg, snap), "entry_price"); c.Status != "blocked" || c.Operator != "lt" {
		t.Fatalf("strict price boundary lost: %+v", c)
	}
	snap.TodayPrice = math.Nextafter(100, 0)
	snap.HasLastBuy, snap.LastBuyDate = true, day.Add(-14*24*time.Hour+time.Hour)
	p := engine.observeBuy(cfg, snap)
	if c := observedCondition(t, p, "buy_cooldown"); c.Status != "blocked" || c.RemainingDays == nil || *c.RemainingDays != 1 {
		t.Fatalf("partial calendar day must remain blocked: %+v", c)
	}
	snap.LastBuyDate = day.AddDate(0, 0, -14)
	engine.SeedLastTrailSell("AAA", day.AddDate(0, 0, -41))
	p = engine.observeBuy(cfg, snap)
	if c := observedCondition(t, p, "buy_cooldown"); c.Status != "passed" || *c.RemainingDays != 0 || c.ReleaseDate != "2026-10-13" {
		t.Fatalf("exact buy cooldown should pass: %+v", c)
	}
	if c := observedCondition(t, p, "reentry_cooldown"); c.Status != "blocked" || *c.RemainingDays != 1 || c.ReleaseDate != "2026-10-14" || p.IntendedShares != 0 {
		t.Fatalf("reentry gate mismatch: %+v / %+v", c, p)
	}
	engine.SeedLastTrailSell("AAA", day.AddDate(0, 0, -42))
	if p = engine.observeBuy(cfg, snap); p.Status != "eligible" || p.IntendedShares <= 0 {
		t.Fatalf("exact reentry boundary should pass: %+v", p)
	}
}

// TestObservation_SellBoundaries 驗證浮點邊界沿用價格比較，不自行四捨五入判斷。
func TestObservation_SellBoundaries(t *testing.T) {
	cfg := baseCfg("AAA")
	cfg.TrailStopBear, cfg.TrailMinGain = .08, .10
	snap := Snapshot{TodayPrice: 120 * (1 - cfg.TrailStopBear), LowestHeldPrice: 100, PeakSinceHold: 120, HeldShares: 10}
	for _, price := range []float64{snap.TodayPrice, math.Nextafter(snap.TodayPrice, math.Inf(1)), math.Nextafter(snap.TodayPrice, 0)} {
		snap.TodayPrice = price
		got := observeSell(cfg, snap, 1000)
		intent := DecideSell(cfg, snap)
		if (got.Status == "eligible") != intent.Should {
			t.Fatalf("sell boundary %.17g: status %s vs intent %+v", price, got.Status, intent)
		}
	}
	snap.IsBull = true
	if p := observeSell(cfg, snap, 1000); p.Status != "blocked" || observedCondition(t, p, "bear_regime").Status != "blocked" {
		t.Fatalf("bull should not sell: %+v", p)
	}
	snap.HeldShares, snap.LowestHeldPrice, snap.PeakSinceHold = 0, -1, 0
	if p := observeSell(cfg, snap, 1000); p.Status != "inactive" || observedCondition(t, p, "peak_gain").Value != nil || observedCondition(t, p, "trailing_drawdown").Status != "inactive" {
		t.Fatalf("no-position snapshot invents a signal: %+v", p)
	}
}

// TestObservation_PreservesBuyThenSellPhases 驗證同日先買再賣不會覆蓋成交前條件。
func TestObservation_PreservesBuyThenSellPhases(t *testing.T) {
	cfg := baseCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	cfg.InitialCash = 10000
	day := mustDate(t, "2026-10-13")
	series := map[string]*StockSeries{"AAA": seriesFrom(day.AddDate(0, 0, -60), constPrices(60, 100))}
	engine := NewEngine(cfg)
	engine.SeedPosition("AAA", day.AddDate(0, 0, -30), 10, 100)
	engine.SeedRiskState("AAA", RiskState{Peak: 120})
	var got []DecisionObservation
	engine.SetDecisionObserver(func(o DecisionObservation) { got = append(got, o) })
	if err := engine.ProcessOpenDecision(day, map[string]float64{"AAA": 90}, series, NoopExecutor{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("observations = %d", len(got))
	}
	o := got[0]
	if o.Buy.Status != "executed" || o.Sell.Status != "executed" || o.Buy.HeldSharesBefore != 10 || o.Buy.HeldSharesAfter <= 10 || o.Sell.HeldSharesBefore != o.Buy.HeldSharesAfter || o.Sell.HeldSharesAfter != 0 {
		t.Fatalf("phase holdings are not frozen correctly: %+v", o)
	}
	if o.Buy.CashAfter != o.Sell.CashBefore || o.Sell.CashAfter != engine.Cash() || o.Buy.CashBefore != 10000 {
		t.Fatalf("phase cash mismatch: %+v", o)
	}
	if c := observedCondition(t, o.Buy, "buy_cooldown"); c.Value != nil || c.Status != "passed" {
		t.Fatalf("post-buy cooldown leaked into pre-buy decision: %+v", c)
	}
	if c := observedCondition(t, o.Sell, "peak_gain"); c.Value == nil || math.Abs(*c.Value-(120.0/90-1)*100) > 1e-9 {
		t.Fatalf("sell phase failed to include newly bought lowest-cost lot: %+v", c)
	}
	if engine.PeakSinceHold("AAA") != 0 || o.Sell.Metrics[0].Value == nil || *o.Sell.Metrics[0].Value != 120 {
		t.Fatal("sell reset mutated frozen peak")
	}
}

// TestObservation_UsesPriorIndicatorsAndOverrides 驗證當日收盤不可見且各股票採自己的參數。
func TestObservation_UsesPriorIndicatorsAndOverrides(t *testing.T) {
	cfg := baseCfg("AAA", "BBB")
	cfg.DecisionPriceBasis = "open"
	band := .2
	cfg.StockOverrides = map[string]config.StockParams{"AAA": {RegimeMAWindow: iptr(2), BullBuyBand: &band, TrailReentryCooldownDays: iptr(42)}}
	day := mustDate(t, "2026-10-13")
	prices := append(constPrices(58, 100), 60, 70, 9999)
	ss := seriesFrom(day.AddDate(0, 0, -60), prices)
	series := map[string]*StockSeries{"AAA": ss, "BBB": ss}
	engine := NewEngine(cfg)
	engine.SeedLastTrailSell("AAA", day.AddDate(0, 0, -35))
	var got []DecisionObservation
	engine.SetDecisionObserver(func(o DecisionObservation) { got = append(got, o) })
	if err := engine.ProcessOpenDecision(day, map[string]float64{"AAA": 100, "BBB": 100}, series, NoopExecutor{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Regime != "bull" || got[1].Regime != "bear" || got[0].RegimeWindow != 2 || got[0].RegimeValue == nil || *got[0].RegimeValue != 70 || got[0].IndicatorDate != day.AddDate(0, 0, -1) {
		t.Fatalf("override/prior-data mismatch: %+v", got)
	}
	if c := observedCondition(t, got[0].Buy, "reentry_cooldown"); c.RemainingDays == nil || *c.RemainingDays != 7 {
		t.Fatalf("stock cooldown override missing: %+v", c)
	}
	if observedCondition(t, got[0].Buy, "entry_price").Status != "passed" || observedCondition(t, got[1].Buy, "entry_price").Status != "blocked" {
		t.Fatal("per-stock regime/band inputs were not used")
	}
}

// TestObservation_DoesNotChangeTrading 驗證觀察開關不影響整段歷史的資金、持倉及交易計數。
func TestObservation_DoesNotChangeTrading(t *testing.T) {
	cfg := baseCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	prices := append(linRamp(100, 50, 200), linRamp(80, 200, 80)...)
	ss := seriesFrom(mustDate(t, "2025-01-01"), prices)
	series := map[string]*StockSeries{"AAA": ss}
	plain, observed := NewEngine(cfg), NewEngine(cfg)
	count := 0
	observed.SetDecisionObserver(func(o DecisionObservation) { count++ })
	for _, day := range ss.Dates {
		if err := plain.ProcessDay(day, series, NoopExecutor{}); err != nil {
			t.Fatal(err)
		}
		if err := observed.ProcessDay(day, series, NoopExecutor{}); err != nil {
			t.Fatal(err)
		}
		if plain.Cash() != observed.Cash() || plain.Stats() != observed.Stats() || !reflect.DeepEqual(plain.positions, observed.positions) || plain.RiskState("AAA") != observed.RiskState("AAA") {
			t.Fatalf("observer changed trading on %s", day)
		}
	}
	if count != len(ss.Dates) {
		t.Fatalf("no-trade or unavailable days omitted: %d != %d", count, len(ss.Dates))
	}
	if observed.Clone().observer != nil {
		t.Fatal("candidate clone inherited a previous collector")
	}
}

// TestObservation_SuspendedAndInsufficientData 驗證停牌及暖身資料不足的空狀態。
func TestObservation_SuspendedAndInsufficientData(t *testing.T) {
	cfg := baseCfg("AAA", "BBB")
	day := mustDate(t, "2026-10-13")
	ss := seriesFrom(day.AddDate(0, 0, -1), []float64{100, 90})
	ss.Suspensions = [][2]time.Time{{day, day.AddDate(0, 0, 1)}}
	engine := NewEngine(cfg)
	var got []DecisionObservation
	engine.SetDecisionObserver(func(o DecisionObservation) { got = append(got, o) })
	if err := engine.ProcessDay(day, map[string]*StockSeries{"AAA": ss, "BBB": seriesFrom(day, []float64{90})}, NoopExecutor{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Status != "inactive" || got[1].Buy.Status != "unavailable" || observedCondition(t, got[1].Buy, "entry_price").Threshold != nil {
		t.Fatalf("empty-state mismatch: %+v", got)
	}
}
