package trading

import (
	"testing"
	"time"
)

// engine_test.go 為 Engine 的整合測試 (金字塔中層):驗證「決策 → 套用 → 狀態變動」這條鏈
// (現金夾取、持倉增減、峰值追蹤、打破冷卻計數、狀態還原、as-of 估值)。
// per-stock 隔離測試因依賴 backtest 視窗核心,移至 backtest 套件。

func TestEngine_BuysInBullNeverGoesNegative(t *testing.T) {
	// Arrange — 現金很少但 BullBuyFrac 誇張 (想買遠超現金),測現金夾取與 skipped。
	cfg := baseCfg("TEST")
	cfg.InitialCash = 1000
	cfg.BullBuyFrac = 10 // 想買 1000% 現金 → 必被夾取
	series := map[string]*StockSeries{"TEST": risingSeries(mustDate(t, "2020-01-01"), 120)}
	engine := NewEngine(cfg)

	// Act
	for _, d := range series["TEST"].Dates {
		if err := engine.ProcessDay(d, series, NoopExecutor{}); err != nil {
			t.Fatalf("ProcessDay(%s): %v", d.Format("2006-01-02"), err)
		}
	}

	// Assert — 至少買進一次、現金永不為負、現金耗盡後出現 skipped。
	stats := engine.Stats()
	if stats.TotalBuys == 0 {
		t.Fatalf("expected at least one buy in a bull series")
	}
	if engine.Cash() < 0 {
		t.Fatalf("cash went negative: %.2f", engine.Cash())
	}
	if stats.SkippedBuys == 0 {
		t.Fatalf("expected some skipped buys once cash is exhausted")
	}
}

func TestEngine_ProcessDay_SkipsUntradeableInputs(t *testing.T) {
	// Arrange — 未追蹤的股票 / 非交易日 / 非正價格都不可成交。
	cfg := baseCfg("TEST")
	s := seriesFrom(mustDate(t, "2020-01-01"), append(constPrices(30, 100), 0)) // 最後一天價格 0
	series := map[string]*StockSeries{"TEST": s}
	engine := NewEngine(cfg)

	// Act — 非交易日 (序列沒有的日期)。
	if err := engine.ProcessDay(mustDate(t, "1999-01-01"), series, NoopExecutor{}); err != nil {
		t.Fatalf("non-trading day should be no-op, got %v", err)
	}
	// Act — 價格為 0 的交易日。
	zeroDay := s.Dates[len(s.Dates)-1]
	if err := engine.ProcessDay(zeroDay, series, NoopExecutor{}); err != nil {
		t.Fatalf("zero-price day should be no-op, got %v", err)
	}

	// Assert — 完全沒有成交。
	if st := engine.Stats(); st.TotalBuys != 0 || st.TotalSells != 0 {
		t.Fatalf("expected no trades on untradeable inputs, got %+v", st)
	}
}

func TestEngine_TrailStopExitsInBearAfterPeak(t *testing.T) {
	// Arrange — 先大漲 (建倉 + 武裝峰值),再崩跌轉空 (觸發移動停利全出)。
	cfg := baseCfg("TEST")
	cfg.RegimeMAWindow = 20
	prices := append(linRamp(80, 50, 200), linRamp(40, 200, 120)...) // 漲到 200 再崩到 120
	series := map[string]*StockSeries{"TEST": seriesFrom(mustDate(t, "2020-01-01"), prices)}
	engine := NewEngine(cfg)

	// Act
	for _, d := range series["TEST"].Dates {
		if err := engine.ProcessDay(d, series, NoopExecutor{}); err != nil {
			t.Fatalf("ProcessDay: %v", err)
		}
	}

	// Assert — 崩跌段應觸發至少一次移動停利出場。
	if st := engine.Stats(); st.TrailSells == 0 {
		t.Fatalf("expected a trailing-stop exit after the crash, got %+v", st)
	}
}

func TestEngine_AddCashOnlyPositive(t *testing.T) {
	// Arrange
	engine := NewEngine(baseCfg("TEST")) // 起始現金 = InitialCash
	start := engine.Cash()

	// Act
	engine.AddCash(2500)
	engine.AddCash(-100) // 應為 no-op
	engine.AddCash(0)    // 應為 no-op

	// Assert
	if got := engine.Cash() - start; got != 2500 {
		t.Fatalf("AddCash net = %.2f, want 2500 (negatives/zero ignored)", got)
	}
}

func TestEngine_SeedRestoresState(t *testing.T) {
	// Arrange — 模擬上線重啟:從 DB 還原現金 / 持倉 / 最後買入日。
	cfg := baseCfg("TEST")
	series := map[string]*StockSeries{"TEST": seriesFrom(mustDate(t, "2020-01-01"), constPrices(10, 50))}
	engine := NewEngine(cfg)

	// Act
	engine.SeedCash(777)
	engine.SeedPosition("TEST", mustDate(t, "2020-01-03"), 100, 40)
	engine.SeedPosition("TEST", mustDate(t, "2020-01-03"), 0, 40) // 非正股數應被忽略
	engine.SeedLastBuy("TEST", mustDate(t, "2020-01-03"))

	// Assert — 現金被覆蓋;持倉以 as-of 收盤 50 估值 = 100×50。
	if engine.Cash() != 777 {
		t.Fatalf("SeedCash failed: cash=%.2f", engine.Cash())
	}
	if v := engine.HoldingValueAsOf(series, mustDate(t, "2020-01-05")); v != 100*50 {
		t.Fatalf("holding value = %.2f, want 5000", v)
	}
}

func TestEngine_HoldingValueAsOf_PreListingIsZero(t *testing.T) {
	// Arrange — 持倉估值在「該股尚未上市」的日期應貢獻 0 (無未來資訊)。
	cfg := baseCfg("TEST")
	series := map[string]*StockSeries{"TEST": seriesFrom(mustDate(t, "2020-06-01"), constPrices(10, 50))}
	engine := NewEngine(cfg)
	engine.SeedPosition("TEST", mustDate(t, "2020-06-01"), 10, 50)

	// Act + Assert — 上市前一日。
	if v := engine.HoldingValueAsOf(series, mustDate(t, "2020-01-01")); v != 0 {
		t.Fatalf("pre-listing holding value = %.2f, want 0", v)
	}
}

func TestEngine_BreaksInWindow_RollingCount(t *testing.T) {
	// Arrange — 直接填入歷次打破冷卻日期 (同套件可存取未匯出欄位)。
	cfg := baseCfg("TEST")
	cfg.CooldownBreakWindowDays = 365
	engine := NewEngine(cfg)
	today := mustDate(t, "2024-12-31")
	engine.breakDates["TEST"] = []time.Time{
		today.AddDate(-2, 0, 0), // 2 年前 → 視窗外
		today.AddDate(0, -1, 0), // 1 月前 → 視窗內
		today.AddDate(0, 0, -5), // 5 天前 → 視窗內
	}

	// Act
	n := engine.breaksInWindow(cfg, "TEST", today)

	// Assert — 只算近 365 日內的兩次。
	if n != 2 {
		t.Fatalf("breaksInWindow = %d, want 2", n)
	}
}

func TestRegimeBull_MaSlope(t *testing.T) {
	// Arrange — 上升序列;ma_slope:當前 MA > lb 日前 MA → bull。
	up := seriesFrom(mustDate(t, "2020-01-01"), linRamp(160, 50, 200))
	cfg := decideCfg()
	cfg.RegimeMethod = "ma_slope"
	cfg.RegimeMAWindow = 20
	cfg.RegimeLookback = 60

	// Act + Assert
	if !regimeBull(cfg, up, 159) {
		t.Fatalf("rising series should be bull under ma_slope")
	}
	// 回看越界 (idx-lb<0 → prev MA NaN) → false。
	if regimeBull(cfg, up, 5) {
		t.Fatalf("insufficient lookback should be bear (false)")
	}
}
