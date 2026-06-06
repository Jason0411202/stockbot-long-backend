package kernals

import (
	"database/sql"
	"io"
	"testing"
	"time"

	"main/app_context"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sirupsen/logrus"
)

// kernals_online_test.go 以 sqlmock 覆蓋上線 / DB-bound 路徑 (狀態還原、catch-up、DB executor、
// DB 版回測與評估)。無限迴圈 (runDailyLoop) 與需 TWSE 網路的 orchestrator 不在此範圍。

func onlineCtx(t *testing.T) (*app_context.AppContext, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < 12; i++ {
		mock.ExpectExec("USE StockLongData").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	log := logrus.New()
	log.SetOutput(io.Discard)
	cfg := baseCfg("AAA")
	return &app_context.AppContext{Db: db, Log: log, Cfg: cfg}, mock
}

func TestLoadStockSeries(t *testing.T) {
	// Arrange — StockHistory 回三日收盤。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").
		WithArgs("AAA").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}).
			AddRow("2024-01-02", 50.0).AddRow("2024-01-03", 51.0).AddRow("2024-01-04", 52.0))

	// Act
	series, err := loadStockSeries(appCtx)

	// Assert
	if err != nil {
		t.Fatalf("loadStockSeries: %v", err)
	}
	s := series["AAA"]
	if s == nil || len(s.dates) != 3 || s.closePrices[2] != 52.0 {
		t.Fatalf("series misbuilt: %+v", s)
	}
}

func TestSeedEngineFromDB(t *testing.T) {
	// Arrange — 還原現金 / 一筆持倉 / 最後買入日。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT state_value FROM BotState").WithArgs("current_cash").
		WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("54321"))
	mock.ExpectQuery("FROM UnrealizedGainsLosses").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "transaction_price", "shares"}).
			AddRow("2024-01-02", "AAA", 50.0, 100))
	mock.ExpectQuery("SELECT MAX").WithArgs("AAA", "AAA").
		WillReturnRows(sqlmock.NewRows([]string{"d"}).AddRow("2024-01-02"))

	engine := NewEngine(appCtx.Cfg)

	// Act
	if err := seedEngineFromDB(appCtx, engine); err != nil {
		t.Fatalf("seedEngineFromDB: %v", err)
	}

	// Assert — 現金被還原;持倉以 2024-01-10 收盤估值 (用 series 不需要,這裡直接看現金 + lastBuy)。
	if engine.Cash() != 54321 {
		t.Fatalf("cash not seeded: %.2f", engine.Cash())
	}
	if lb, ok := engine.lastBuy["AAA"]; !ok || lb.Format("2006-01-02") != "2024-01-02" {
		t.Fatalf("lastBuy not seeded: %v %v", lb, ok)
	}
	if len(engine.positions["AAA"]) != 1 {
		t.Fatalf("position not seeded: %+v", engine.positions["AAA"])
	}
}

func TestRunCatchUp_FlatSeriesNoTrades(t *testing.T) {
	// Arrange — watermark 為空 (首次啟動) → 從 common issuance catch-up;平盤序列 → 零成交。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT state_value FROM BotState").WithArgs("last_processed_date").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO BotState").WillReturnResult(sqlmock.NewResult(1, 1)) // SaveWatermark
	mock.ExpectExec("INSERT INTO BotState").WillReturnResult(sqlmock.NewResult(1, 1)) // SaveCash

	engine := NewEngine(appCtx.Cfg)
	series := map[string]*stockSeries{"AAA": seriesFrom(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), constPrices(60, 100))}

	// Act + Assert
	if err := runCatchUp(appCtx, engine, series); err != nil {
		t.Fatalf("runCatchUp: %v", err)
	}
	if st := engine.Stats(); st.TotalBuys != 0 {
		t.Fatalf("flat catch-up should not trade, got %+v", st)
	}
}

func TestRunBacktest_FromDB(t *testing.T) {
	// Arrange — DB 版回測:loadStockSeries (sqlmock) + 純函式回測。
	appCtx, mock := onlineCtx(t)
	rows := sqlmock.NewRows([]string{"date", "close_price"})
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		rows.AddRow(base.AddDate(0, 0, i).Format("2006-01-02"), 100.0)
	}
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").WithArgs("AAA").WillReturnRows(rows)

	// Act
	res, err := RunBacktest(appCtx)

	// Assert — 平盤 → 零成交、期末總額 = 期初現金。
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}
	if res.TotalBuys != 0 || res.FinalTotal != appCtx.Cfg.InitialCash {
		t.Fatalf("flat backtest unexpected: %+v", res)
	}
}

func TestRunWalkForward_FromDB(t *testing.T) {
	// Arrange
	appCtx, mock := onlineCtx(t)
	rows := sqlmock.NewRows([]string{"date", "close_price"})
	base := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 800; i++ {
		rows.AddRow(base.AddDate(0, 0, i).Format("2006-01-02"), 100.0)
	}
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").WithArgs("AAA").WillReturnRows(rows)

	// Act
	_, agg, err := RunWalkForward(appCtx, WalkForwardParams{WindowMonths: 12, StepMonths: 6, MinTradeDays: 100})

	// Assert
	if err != nil {
		t.Fatalf("RunWalkForward: %v", err)
	}
	if agg.NWindows == 0 {
		t.Fatalf("expected >=1 window")
	}
}

func TestDBExecutor_OnBuyAndSell_Silent(t *testing.T) {
	// Arrange — silent (catch-up) executor:寫 DB、不發 Discord。
	appCtx, mock := onlineCtx(t)
	day := time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC)
	// OnBuyApplied → SQLBuyStock: 取今日收盤 + INSERT UnrealizedGainsLosses。
	mock.ExpectQuery("SELECT close_price FROM StockHistory").WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(50.0))
	mock.ExpectExec("INSERT INTO UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(1, 1))
	// OnSellApplied → SQLSellStock: 取今日收盤 + 找最低成本 lot + 整筆賣 (Delete + Insert realized)。
	mock.ExpectQuery("SELECT close_price FROM StockHistory").WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(80.0))
	mock.ExpectQuery("FROM UnrealizedGainsLosses").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
			AddRow("2024-01-02", "AAA", "n", 50.0, 5000.0, 100))
	mock.ExpectExec("DELETE FROM UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO RealizedGainsLosses").WillReturnResult(sqlmock.NewResult(1, 1))

	exec := &dbExecutor{appCtx: appCtx, notify: false}

	// Act + Assert
	if err := exec.OnBuyApplied("AAA", day, 10, 50.0, 1000); err != nil {
		t.Fatalf("OnBuyApplied: %v", err)
	}
	if err := exec.OnSellApplied("AAA", day, 100, 80.0, 9000); err != nil {
		t.Fatalf("OnSellApplied: %v", err)
	}
}

func TestRunBacktest_RejectsNonBaseline(t *testing.T) {
	// Arrange — guard 在 loadStockSeries 前就回錯,不碰 DB。
	appCtx, _ := onlineCtx(t)
	appCtx.Cfg.ScalingStrategy = "Other"

	// Act + Assert
	if _, err := RunBacktest(appCtx); err == nil {
		t.Fatalf("RunBacktest should reject non-Baseline")
	}
}

func TestRunBacktest_EmptySeriesErrors(t *testing.T) {
	// Arrange — StockHistory 無資料 → series 為空 → 回錯。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").WithArgs("AAA").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}))

	// Act + Assert
	if _, err := RunBacktest(appCtx); err == nil {
		t.Fatalf("RunBacktest should error on empty series")
	}
}

func TestRunWalkForward_EmptySeriesErrors(t *testing.T) {
	// Arrange
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").WithArgs("AAA").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}))

	// Act + Assert
	if _, _, err := RunWalkForward(appCtx, WalkForwardParams{}); err == nil {
		t.Fatalf("RunWalkForward should error on empty series")
	}
}

func TestRunOnlineMode_RejectsNonBaseline(t *testing.T) {
	// Arrange — 非 Baseline 策略,guard 在任何 DB / 網路前就回錯。
	appCtx, _ := onlineCtx(t)
	appCtx.Cfg.ScalingStrategy = "Other"

	// Act + Assert
	if err := runOnlineMode(appCtx); err == nil {
		t.Fatalf("runOnlineMode should reject non-Baseline strategy")
	}
}

func TestSeedEngineFromDB_NoCashFallbackAndDatetimeLot(t *testing.T) {
	// Arrange — 無現金紀錄 (沿用 cfg.InitialCash);lot 日期為 datetime 格式;無最後買入日。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT state_value FROM BotState").WithArgs("current_cash").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("FROM UnrealizedGainsLosses").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "transaction_price", "shares"}).
			AddRow("2024-01-02 00:00:00", "AAA", 50.0, 100))
	mock.ExpectQuery("SELECT MAX").WithArgs("AAA", "AAA").
		WillReturnRows(sqlmock.NewRows([]string{"d"}).AddRow(nil))

	engine := NewEngine(appCtx.Cfg)

	// Act
	if err := seedEngineFromDB(appCtx, engine); err != nil {
		t.Fatalf("seedEngineFromDB: %v", err)
	}

	// Assert — 現金維持 cfg.InitialCash (無 BotState 紀錄);持倉仍還原 (datetime 解析成功)。
	if engine.Cash() != appCtx.Cfg.InitialCash {
		t.Fatalf("cash should fall back to InitialCash, got %.2f", engine.Cash())
	}
	if len(engine.positions["AAA"]) != 1 {
		t.Fatalf("datetime-format lot not seeded: %+v", engine.positions["AAA"])
	}
}

func TestRunCatchUp_ResumesFromWatermark(t *testing.T) {
	// Arrange — watermark 已設 → 只 catch-up watermark 之後的日期。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT state_value FROM BotState").WithArgs("last_processed_date").
		WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("2024-01-30"))
	mock.ExpectExec("INSERT INTO BotState").WillReturnResult(sqlmock.NewResult(1, 1)) // SaveWatermark
	mock.ExpectExec("INSERT INTO BotState").WillReturnResult(sqlmock.NewResult(1, 1)) // SaveCash

	engine := NewEngine(appCtx.Cfg)
	series := map[string]*stockSeries{"AAA": seriesFrom(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), constPrices(60, 100))}

	// Act + Assert — watermark 之後仍有日期,平盤無成交。
	if err := runCatchUp(appCtx, engine, series); err != nil {
		t.Fatalf("runCatchUp resume: %v", err)
	}
}

func TestDBExecutor_OnBuyApplied_NotifyNilSession(t *testing.T) {
	// Arrange — notify=true 但 Dg 為 nil:Discord 發送失敗只被記錄,不影響回傳 (買入仍視為成功)。
	appCtx, mock := onlineCtx(t)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(50.0))
	mock.ExpectExec("INSERT INTO UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(1, 1))

	exec := &dbExecutor{appCtx: appCtx, notify: true}

	// Act + Assert
	if err := exec.OnBuyApplied("AAA", time.Date(2024, 6, 6, 0, 0, 0, 0, time.UTC), 10, 50.0, 1000); err != nil {
		t.Fatalf("OnBuyApplied with nil discord should still succeed, got %v", err)
	}
}
