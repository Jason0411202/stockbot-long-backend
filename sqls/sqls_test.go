package sqls

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/app_context"
	"github.com/Jason0411202/stockbot-long-backend/config"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/go-sql-driver/mysql" // 註冊 "mysql" driver,供 ConnectToMariadb 的 sql.Open 測試
	"github.com/sirupsen/logrus"
)

func sqlNoRows() error { return sql.ErrNoRows }

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// sqls_test.go 以 go-sqlmock (DB) + httptest (TWSE) 驗證資料存取層,完全不依賴真實 MariaDB / 網路。
// 因每個函式都會先 ConnectToMariadb(ping) + ConnectToDatabase("USE ..."),測試以「非嚴格順序 + 預掛足量 USE」
// 降低樣板,聚焦在「查詢/掃描/寫入」這層的正確性。

// mockCtx 回傳掛上 sqlmock 的 AppContext;ping 預設成功 (sqlmock 不監控 ping)。
func mockCtx(t *testing.T) (*app_context.AppContext, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	log := logrus.New()
	log.SetOutput(io.Discard)
	return &app_context.AppContext{
		Db:  db,
		Log: log,
		Cfg: &config.Config{TrackStocks: []string{"00631L"}},
	}, mock
}

// useN 預掛 n 個 "USE StockLongData" 期望 (各函式內部會重複呼叫 ConnectToDatabase)。
func useN(mock sqlmock.Sqlmock, n int) {
	for i := 0; i < n; i++ {
		mock.ExpectExec("USE StockLongData").WillReturnResult(sqlmock.NewResult(0, 0))
	}
}

func TestConnectToMariadb_NilDbNoDSN(t *testing.T) {
	// Arrange — Db 為 nil 且未設 DB_DSN → 應回錯。
	t.Setenv("DB_DSN", "")
	appCtx := &app_context.AppContext{Log: logrus.New()}
	appCtx.Log.SetOutput(io.Discard)

	// Act + Assert
	if err := ConnectToMariadb(appCtx); err == nil {
		t.Fatalf("expected error when Db nil and DB_DSN unset")
	}
}

func TestConnectToMariadb_ReusesHealthyConn(t *testing.T) {
	// Arrange — Db 已可用 (ping ok) → 直接重用,不需 DSN。
	appCtx, _ := mockCtx(t)

	// Act + Assert
	if err := ConnectToMariadb(appCtx); err != nil {
		t.Fatalf("healthy conn should reuse, got %v", err)
	}
}

func TestConnectToMariadb_OpensWithDSN(t *testing.T) {
	// Arrange — Db 為 nil 但有 DSN → sql.Open (lazy) 應成功並掛上 appCtx.Db。
	t.Setenv("DB_DSN", "user:pass@tcp(127.0.0.1:3306)/StockLongData")
	log := logrus.New()
	log.SetOutput(io.Discard)
	appCtx := &app_context.AppContext{Log: log, Cfg: &config.Config{TrackStocks: []string{"X"}}}

	// Act + Assert — sql.Open 不會真的連線,故應回 nil 並設定 Db。
	if err := ConnectToMariadb(appCtx); err != nil {
		t.Fatalf("ConnectToMariadb with DSN: %v", err)
	}
	if appCtx.Db == nil {
		t.Fatalf("expected appCtx.Db to be set after open")
	}
	_ = appCtx.Db.Close()
}

// TestDBFunctions_ConnectErrorPropagates 以「無 Db、無 DSN」的壞 context 一次覆蓋多個函式的
// ConnectToMariadb 失敗保護分支 (各函式的第一道 guard)。
func TestDBFunctions_ConnectErrorPropagates(t *testing.T) {
	t.Setenv("DB_DSN", "")
	broken := func() *app_context.AppContext {
		log := logrus.New()
		log.SetOutput(io.Discard)
		return &app_context.AppContext{Cfg: &config.Config{TrackStocks: []string{"X"}}, Log: log}
	}

	if _, _, err := LoadCash(broken()); err == nil {
		t.Fatalf("LoadCash should propagate connect error")
	}
	if err := SaveCash(broken(), 1); err == nil {
		t.Fatalf("SaveCash should propagate connect error")
	}
	if _, _, err := LoadLastBuyDate(broken(), "X"); err == nil {
		t.Fatalf("LoadLastBuyDate should propagate connect error")
	}
	if _, err := LoadAllUnrealizedLots(broken()); err == nil {
		t.Fatalf("LoadAllUnrealizedLots should propagate connect error")
	}
	if _, err := GetTodayStockPrice(broken(), "X", "2024-01-01", "close_price"); err == nil {
		t.Fatalf("GetTodayStockPrice should propagate connect error")
	}
	if _, err := GetStockName(broken(), "X"); err == nil {
		t.Fatalf("GetStockName should propagate connect error")
	}
	if _, err := GetStockHistoryData(broken(), "X"); err == nil {
		t.Fatalf("GetStockHistoryData should propagate connect error")
	}
	if _, err := GetAllUnrealizedGainsLosses(broken()); err == nil {
		t.Fatalf("GetAllUnrealizedGainsLosses should propagate connect error")
	}
	if _, err := GetAllRealizedGainsLosses(broken()); err == nil {
		t.Fatalf("GetAllRealizedGainsLosses should propagate connect error")
	}
	if _, err := GetLowestUnrealizedGainsLossesRecord(broken(), "X", "2024-01-01"); err == nil {
		t.Fatalf("GetLowestUnrealizedGainsLossesRecord should propagate connect error")
	}
	if err := DeleteLowestUnrealizedGainsLossesRecord(broken(), "X", "2024-01-01"); err == nil {
		t.Fatalf("Delete should propagate connect error")
	}
	if err := UpdateLowestUnrealizedGainsLossesRecord(broken(), "X", 1, 1, "2024-01-01"); err == nil {
		t.Fatalf("Update should propagate connect error")
	}
	if err := InsertToRealizedGainsLosses(broken(), "b", "s", "X", "n", 1, 2, 3, 4, 5, 6, 7); err == nil {
		t.Fatalf("InsertToRealizedGainsLosses should propagate connect error")
	}
	if _, err := getCompletedBackfillMonths(broken(), "X"); err == nil {
		t.Fatalf("getCompletedBackfillMonths should propagate connect error")
	}
	if err := markBackfillMonthComplete(broken(), "X", "2024-01"); err == nil {
		t.Fatalf("markBackfillMonthComplete should propagate connect error")
	}
	if got := LowerPointDays(broken(), "X", "2024-01-01"); got != -1 {
		t.Fatalf("LowerPointDays connect error should return -1, got %d", got)
	}
	if got := UpperPointDays(broken(), "X", "2024-01-01"); got != -1 {
		t.Fatalf("UpperPointDays connect error should return -1, got %d", got)
	}
}

func TestConnectToDatabase(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	mock.ExpectExec("USE StockLongData").WillReturnResult(sqlmock.NewResult(0, 0))

	// Act + Assert — USE 成功。
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		t.Fatalf("USE should succeed, got %v", err)
	}
}

func TestSaveAndLoadCash(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 4)
	mock.ExpectExec("INSERT INTO BotState").
		WithArgs("current_cash", "12345.5").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT state_value FROM BotState").
		WithArgs("current_cash").
		WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("12345.5"))

	// Act
	if err := SaveCash(appCtx, 12345.5); err != nil {
		t.Fatalf("SaveCash: %v", err)
	}
	cash, ok, err := LoadCash(appCtx)

	// Assert
	if err != nil || !ok || cash != 12345.5 {
		t.Fatalf("LoadCash = (%.2f,%v,%v), want (12345.5,true,nil)", cash, ok, err)
	}
}

func TestLoadCash_NoRow(t *testing.T) {
	// Arrange — BotState 無紀錄 → ok=false (呼叫端 fallback 到 InitialCash)。
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT state_value FROM BotState").
		WithArgs("current_cash").
		WillReturnError(sqlNoRows())

	// Act
	_, ok, err := LoadCash(appCtx)

	// Assert
	if ok || err != nil {
		t.Fatalf("missing BotState → (ok=%v,err=%v), want (false,nil)", ok, err)
	}
}

func TestSaveAndLoadWatermark(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 4)
	mock.ExpectExec("INSERT INTO BotState").
		WithArgs("last_processed_date", "2024-03-15").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT state_value FROM BotState").
		WithArgs("last_processed_date").
		WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("2024-03-15"))

	// Act
	if err := SaveWatermark(appCtx, mustDate(t, "2024-03-15")); err != nil {
		t.Fatalf("SaveWatermark: %v", err)
	}
	wm, err := LoadWatermark(appCtx)

	// Assert
	if err != nil || wm.Format("2006-01-02") != "2024-03-15" {
		t.Fatalf("LoadWatermark = (%v,%v), want 2024-03-15", wm, err)
	}
}

func TestLoadWatermark_ZeroWhenEmpty(t *testing.T) {
	// Arrange — 首次啟動,無紀錄 → zero time。
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT state_value FROM BotState").
		WithArgs("last_processed_date").
		WillReturnError(sqlNoRows())

	// Act
	wm, err := LoadWatermark(appCtx)

	// Assert
	if err != nil || !wm.IsZero() {
		t.Fatalf("empty watermark = (%v,%v), want (zero,nil)", wm, err)
	}
}

func TestLoadLastBuyDate(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT MAX").
		WithArgs("00631L", "00631L").
		WillReturnRows(sqlmock.NewRows([]string{"d"}).AddRow("2024-05-20"))

	// Act
	d, ok, err := LoadLastBuyDate(appCtx, "00631L")

	// Assert
	if err != nil || !ok || d.Format("2006-01-02") != "2024-05-20" {
		t.Fatalf("LoadLastBuyDate = (%v,%v,%v), want 2024-05-20/true", d, ok, err)
	}
}

func TestLoadLastBuyDate_NullNoHistory(t *testing.T) {
	// Arrange — 從未買過 → MAX 回 NULL。
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT MAX").
		WithArgs("X", "X").
		WillReturnRows(sqlmock.NewRows([]string{"d"}).AddRow(nil))

	// Act
	_, ok, err := LoadLastBuyDate(appCtx, "X")

	// Assert
	if ok || err != nil {
		t.Fatalf("null last buy → (ok=%v,err=%v), want (false,nil)", ok, err)
	}
}

func TestLoadAllUnrealizedLots(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	rows := sqlmock.NewRows([]string{"transaction_date", "stock_id", "transaction_price", "shares"}).
		AddRow("2024-01-02", "00631L", 50.5, 100).
		AddRow("2024-02-02", "00830", 30.0, 200)
	mock.ExpectQuery("SELECT transaction_date, stock_id, transaction_price, shares FROM UnrealizedGainsLosses").
		WillReturnRows(rows)

	// Act
	lots, err := LoadAllUnrealizedLots(appCtx)

	// Assert
	if err != nil || len(lots) != 2 || lots[0].StockID != "00631L" || lots[0].Shares != 100 {
		t.Fatalf("LoadAllUnrealizedLots = %+v (err %v), want 2 lots", lots, err)
	}
}

func TestGetTodayStockPrice(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(123.45))

	// Act
	px, err := GetTodayStockPrice(appCtx, "00631L", "2024-06-06", "close_price")

	// Assert
	if err != nil || px != 123.45 {
		t.Fatalf("GetTodayStockPrice = (%.2f,%v), want 123.45", px, err)
	}
}

func TestGetStockName(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT stock_name FROM StockHistory").
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"stock_name"}).AddRow("元大台灣50正2"))

	// Act
	name, err := GetStockName(appCtx, "00631L")

	// Assert
	if err != nil || name != "元大台灣50正2" {
		t.Fatalf("GetStockName = (%q,%v)", name, err)
	}
}

func TestLowerAndUpperPointDays(t *testing.T) {
	// Arrange — 收盤序列 (新→舊):今日 100;往回第 2 天才出現更低 (95) → lower=2;更高 (110) 在第 1 天 → upper=1。
	appCtx, mock := mockCtx(t)
	useN(mock, 4)
	prices := sqlmock.NewRows([]string{"close_price"}).AddRow(100.0).AddRow(110.0).AddRow(95.0)
	prices2 := sqlmock.NewRows([]string{"close_price"}).AddRow(100.0).AddRow(110.0).AddRow(95.0)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").WithArgs("00631L", "2024-06-06").WillReturnRows(prices)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").WithArgs("00631L", "2024-06-06").WillReturnRows(prices2)

	// Act
	lower := LowerPointDays(appCtx, "00631L", "2024-06-06")
	upper := UpperPointDays(appCtx, "00631L", "2024-06-06")

	// Assert
	if lower != 2 {
		t.Fatalf("LowerPointDays = %d, want 2", lower)
	}
	if upper != 1 {
		t.Fatalf("UpperPointDays = %d, want 1", upper)
	}
}

func TestSQLBuyStock(t *testing.T) {
	// Arrange — 取今日收盤 50 → 寫入 UnrealizedGainsLosses。
	appCtx, mock := mockCtx(t)
	useN(mock, 6)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(50.0))
	mock.ExpectExec("INSERT INTO UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(1, 1))

	// Act
	err := SQLBuyStock(appCtx, "00631L", "2024-06-06", 10)

	// Assert
	if err != nil {
		t.Fatalf("SQLBuyStock: %v", err)
	}
}

func TestSQLBuyStock_RejectsNonPositiveShares(t *testing.T) {
	// Arrange
	appCtx, _ := mockCtx(t)
	// Act + Assert — 0 股應立即回錯,不打 DB。
	if err := SQLBuyStock(appCtx, "00631L", "2024-06-06", 0); err == nil {
		t.Fatalf("expected error for shares<=0")
	}
}

func TestSQLSellStock_WholeLot(t *testing.T) {
	// Arrange — 賣 100 股;最低成本 lot 正好 100 股 → 整筆賣出 (Delete + Insert realized)。
	appCtx, mock := mockCtx(t)
	useN(mock, 10)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(80.0))
	lot := sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
		AddRow("2024-01-02", "00631L", "元大台灣50正2", 50.0, 5000.0, 100)
	mock.ExpectQuery("SELECT transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares FROM UnrealizedGainsLosses").
		WithArgs("00631L", "2024-06-06").WillReturnRows(lot)
	mock.ExpectExec("DELETE FROM UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO RealizedGainsLosses").WillReturnResult(sqlmock.NewResult(1, 1))

	// Act
	err := SQLSellStock(appCtx, "00631L", "2024-06-06", 100)

	// Assert
	if err != nil {
		t.Fatalf("SQLSellStock whole lot: %v", err)
	}
}

func TestSQLSellStock_NoInventoryIsNoop(t *testing.T) {
	// Arrange — 找不到持倉 → 視為 no-op (不報錯)。
	appCtx, mock := mockCtx(t)
	useN(mock, 6)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(80.0))
	mock.ExpectQuery("FROM UnrealizedGainsLosses").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}))

	// Act
	err := SQLSellStock(appCtx, "00631L", "2024-06-06", 50)

	// Assert
	if err != nil {
		t.Fatalf("selling with no inventory should be no-op, got %v", err)
	}
}

func TestSQLSellStock_ZeroTargetNoop(t *testing.T) {
	appCtx, _ := mockCtx(t)
	if err := SQLSellStock(appCtx, "00631L", "2024-06-06", 0); err != nil {
		t.Fatalf("targetShares<=0 should be no-op, got %v", err)
	}
}

func TestGetAllUnrealizedGainsLosses(t *testing.T) {
	// Arrange — 一筆持倉,並查當前價算未實現損益。
	appCtx, mock := mockCtx(t)
	useN(mock, 4)
	mock.ExpectQuery("FROM UnrealizedGainsLosses ORDER BY transaction_date DESC").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
			AddRow("2024-01-02", "00631L", "元大台灣50正2", 50.0, 5000.0, 100))
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(60.0))

	// Act
	out, err := GetAllUnrealizedGainsLosses(appCtx)

	// Assert — now_value = 60×100 = 6000;profit = 1000。
	if err != nil || len(out) != 1 || out[0]["now_value"].(float64) != 6000 {
		t.Fatalf("GetAllUnrealizedGainsLosses = %+v (err %v)", out, err)
	}
}

func TestGetAllRealizedGainsLosses(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("FROM RealizedGainsLosses ORDER BY sell_date DESC").
		WillReturnRows(sqlmock.NewRows([]string{"buy_date", "sell_date", "stock_id", "stock_name", "purchase_price", "sell_price", "investment_cost", "revenue", "profit_loss", "profit_rate", "shares"}).
			AddRow("2024-01-02", "2024-03-02", "00631L", "元大台灣50正2", 50.0, 80.0, 5000.0, 8000.0, 3000.0, 60.0, 100))

	// Act
	out, err := GetAllRealizedGainsLosses(appCtx)

	// Assert
	if err != nil || len(out) != 1 || out[0]["stock_id"] != "00631L" {
		t.Fatalf("GetAllRealizedGainsLosses = %+v (err %v)", out, err)
	}
}

func TestGetStockHistoryData(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT date, close_price FROM StockHistory").
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}).AddRow("2024-01-02", 50.0).AddRow("2024-01-03", 51.0))

	// Act
	out, err := GetStockHistoryData(appCtx, "00631L")

	// Assert
	if err != nil || len(out) != 2 || out[1]["price"].(float64) != 51.0 {
		t.Fatalf("GetStockHistoryData = %+v (err %v)", out, err)
	}
}

func TestGetStockStatisticData(t *testing.T) {
	// Arrange — 單檔:name + today price + upper/lower point days。
	appCtx, mock := mockCtx(t)
	useN(mock, 12)
	mock.ExpectQuery("SELECT stock_name FROM StockHistory").
		WillReturnRows(sqlmock.NewRows([]string{"stock_name"}).AddRow("元大台灣50正2"))
	mock.ExpectQuery("SELECT close_price FROM StockHistory.*LIMIT 1").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(100.0))
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(100.0).AddRow(110.0))
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(100.0).AddRow(110.0).AddRow(95.0))

	// Act
	out, err := GetStockStatisticData(appCtx)

	// Assert
	if err != nil || len(out) != 1 || out[0]["stock_id"] != "00631L" {
		t.Fatalf("GetStockStatisticData = %+v (err %v)", out, err)
	}
}

func TestGetCompletedBackfillMonths(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("SELECT month FROM BackfillStatus").
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"month"}).AddRow("2024-01").AddRow("2024-02"))

	// Act
	got, err := getCompletedBackfillMonths(appCtx, "00631L")

	// Assert
	if err != nil || !got["2024-01"] || !got["2024-02"] || got["2024-03"] {
		t.Fatalf("getCompletedBackfillMonths = %v (err %v)", got, err)
	}
}

func TestMarkBackfillMonthComplete(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectExec("INSERT IGNORE INTO BackfillStatus").
		WithArgs("00631L", "2024-01").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Act + Assert
	if err := markBackfillMonthComplete(appCtx, "00631L", "2024-01"); err != nil {
		t.Fatalf("markBackfillMonthComplete: %v", err)
	}
}

func TestMonthlyBackfillDates(t *testing.T) {
	// Act — 從 20240315 回推 2 個月。
	got := monthlyBackfillDates("20240315", 2)

	// Assert — 第一筆為 currentDate,其後各月 1 號。
	if len(got) != 3 || got[0] != "20240315" || got[1] != "20240201" || got[2] != "20240101" {
		t.Fatalf("monthlyBackfillDates = %v", got)
	}
}

func TestMonthlyBackfillDates_BadInput(t *testing.T) {
	// Act + Assert — 無法解析的日期 → 僅回 currentDate 本身。
	got := monthlyBackfillDates("not-a-date", 3)
	if len(got) != 1 || got[0] != "not-a-date" {
		t.Fatalf("bad date should yield single element, got %v", got)
	}
}

func TestDateToYearMonth(t *testing.T) {
	// Act
	ym, err := dateToYearMonth("20240315")
	// Assert
	if err != nil || ym != "2024-03" {
		t.Fatalf("dateToYearMonth = (%q,%v), want 2024-03", ym, err)
	}
	if _, err := dateToYearMonth("bad"); err == nil {
		t.Fatalf("expected error for bad date")
	}
}

// --- TWSE API (httptest) ---

const twseSampleJSON = `{"stat":"OK","title":"113年01月 00631L 元大台灣50正2 日成交資訊","data":[["113/01/02","1,000","50,000","50.00","51.00","49.00","50.50","+0.50","100"],["113/01/03","2,000","60,000","50.50","52.00","50.00","51.50","+1.00","120"]]}`

func TestTWSEapi_ParsesAndReverses(t *testing.T) {
	// Arrange — 用 httptest 取代真實 TWSE 端點。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, twseSampleJSON)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()

	appCtx, _ := mockCtx(t)

	// Act
	data, name, err := TWSEapi("20240101", "00631L", appCtx)

	// Assert — 解析逗號去除、由新到舊反轉 (最後一筆變第一筆)、stockName 取 title 第三欄。
	if err != nil {
		t.Fatalf("TWSEapi: %v", err)
	}
	if name != "元大台灣50正2" {
		t.Fatalf("stockName = %q", name)
	}
	if len(data) != 2 || data[0][0] != "113/01/03" || data[0][1] != "2000" {
		t.Fatalf("data not reversed/cleaned: %+v", data)
	}
}

func TestTWSEapi_NoDataKey(t *testing.T) {
	// Arrange — 回傳缺 data key。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"stat":"很抱歉，沒有符合條件的資料!"}`)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()
	appCtx, _ := mockCtx(t)

	// Act + Assert
	if _, _, err := TWSEapi("20240101", "00631L", appCtx); err == nil {
		t.Fatalf("expected error when data key missing")
	}
}

func TestTWSEapi_HTTPError(t *testing.T) {
	// Arrange — 伺服器回 500。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()
	appCtx, _ := mockCtx(t)

	// Act + Assert
	if _, _, err := TWSEapi("20240101", "00631L", appCtx); err == nil {
		t.Fatalf("expected error on HTTP 500")
	}
}

func TestSQLSellStock_PartialLot(t *testing.T) {
	// Arrange — 賣 30 股;最低成本 lot 有 100 股 → 部分賣出 (Update 剩 70 + Insert realized)。
	appCtx, mock := mockCtx(t)
	useN(mock, 10)
	mock.ExpectQuery("SELECT close_price FROM StockHistory").
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(80.0))
	lot := sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
		AddRow("2024-01-02", "00631L", "元大台灣50正2", 50.0, 5000.0, 100)
	mock.ExpectQuery("FROM UnrealizedGainsLosses").WithArgs("00631L", "2024-06-06").WillReturnRows(lot)
	mock.ExpectExec("UPDATE UnrealizedGainsLosses").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO RealizedGainsLosses").WillReturnResult(sqlmock.NewResult(1, 1))

	// Act + Assert
	if err := SQLSellStock(appCtx, "00631L", "2024-06-06", 30); err != nil {
		t.Fatalf("SQLSellStock partial: %v", err)
	}
}

func TestUpdateAndDeleteLowestRecord(t *testing.T) {
	// Arrange
	appCtx, mock := mockCtx(t)
	useN(mock, 4)
	mock.ExpectExec("UPDATE UnrealizedGainsLosses").
		WithArgs(3500.0, 70, "00631L", "2024-01-02").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM UnrealizedGainsLosses").
		WithArgs("00631L", "2024-01-02").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Act + Assert
	if err := UpdateLowestUnrealizedGainsLossesRecord(appCtx, "00631L", 3500.0, 70, "2024-01-02"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := DeleteLowestUnrealizedGainsLossesRecord(appCtx, "00631L", "2024-01-02"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestGetLowestRecord_NotFound(t *testing.T) {
	// Arrange — 無持倉列 → 回錯。
	appCtx, mock := mockCtx(t)
	useN(mock, 2)
	mock.ExpectQuery("FROM UnrealizedGainsLosses").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}))

	// Act + Assert
	if _, err := GetLowestUnrealizedGainsLossesRecord(appCtx, "00631L", "2024-06-06"); err == nil {
		t.Fatalf("expected error when no unrealized record")
	}
}

func TestConnectToDatabase_Error(t *testing.T) {
	// Arrange — USE 失敗。
	appCtx, mock := mockCtx(t)
	mock.ExpectExec("USE Nope").WillReturnError(sqlNoRows())

	// Act + Assert
	if err := ConnectToDatabase(appCtx, "Nope"); err == nil {
		t.Fatalf("expected USE error")
	}
}

// UpdataDatebase 走 TWSE 失敗即 break 的快速路徑 (httptest 回 500),驗證它不致命地返回 nil。
func TestUpdataDatebase_FetchFailsGracefully(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()

	appCtx, mock := mockCtx(t)
	appCtx.Cfg.MaxBackMonths = 0 // 僅 currentDate 一次 → 失敗即 break,不 sleep
	useN(mock, 4)

	// Act + Assert — fetch 失敗會被 break 吞掉,函式仍回 nil。
	if err := UpdataDatebase(appCtx); err != nil {
		t.Fatalf("UpdataDatebase should swallow fetch errors, got %v", err)
	}
}

// updateDatabaseWithMonths 同樣走快速失敗路徑 (先查 BackfillStatus,再因 TWSE 500 而 break)。
func TestUpdateDatabaseWithMonths_FetchFailsGracefully(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()

	appCtx, mock := mockCtx(t)
	useN(mock, 6)
	mock.ExpectQuery("SELECT month FROM BackfillStatus").
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"month"})) // 無已完成月份 → 不跳過,直接打 API

	// Act + Assert
	if err := updateDatabaseWithMonths(appCtx, 0); err != nil {
		t.Fatalf("updateDatabaseWithMonths should swallow fetch errors, got %v", err)
	}
}

func TestInitDatabase_MissingSQLFile(t *testing.T) {
	// Arrange — 測試 cwd 下不存在 ./sqls/SQLcommend.sql → ReadFile 失敗路徑。
	appCtx, _ := mockCtx(t)

	// Act + Assert
	if err := InitDatabase(appCtx); err == nil {
		t.Fatalf("expected error reading missing SQLcommend.sql")
	}
}

func TestInitDatabase_SuccessPath(t *testing.T) {
	// Arrange — 在 cwd(sqls/) 下備好 ./sqls/SQLcommend.sql,讓 ReadFile 成功;
	// TWSE 以 httptest 回 500 → 補資料的 fetch 失敗即 break (不致命),InitDatabase 仍完成建表流程。
	if err := os.MkdirAll("sqls", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll("sqls") })
	if err := os.WriteFile("sqls/SQLcommend.sql", []byte("CREATE TABLE IF NOT EXISTS Demo (id INT);"), 0o600); err != nil {
		t.Fatalf("write sql: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()

	appCtx, mock := mockCtx(t)
	appCtx.Cfg.InitDBBackMonths = 1 // > MaxBackMonths(0) → 走 updateDatabaseWithMonths 分支
	appCtx.Cfg.MaxBackMonths = 0
	useN(mock, 8)
	mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0)) // 建表腳本
	mock.ExpectQuery("SELECT month FROM BackfillStatus").WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"month"})) // 無已完成月份

	// Act + Assert
	if err := InitDatabase(appCtx); err != nil {
		t.Fatalf("InitDatabase success path: %v", err)
	}
}

func TestFetchAndInsertMonth(t *testing.T) {
	// Arrange — TWSE 回兩列;非當月 → 整月成功後標記 BackfillStatus。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, twseSampleJSON)
	}))
	defer srv.Close()
	restore := twseBaseURL
	twseBaseURL = srv.URL
	defer func() { twseBaseURL = restore }()

	appCtx, mock := mockCtx(t)
	useN(mock, 4)
	mock.ExpectExec("INSERT IGNORE INTO StockHistory").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT IGNORE INTO StockHistory").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT IGNORE INTO BackfillStatus").WillReturnResult(sqlmock.NewResult(1, 1))

	// Act
	err := fetchAndInsertMonth(appCtx, "00631L", "20240101", "2024-01", "2099-12")

	// Assert
	if err != nil {
		t.Fatalf("fetchAndInsertMonth: %v", err)
	}
}
