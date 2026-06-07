// internal/repository/repository_test.go 以 sqlmock 驗證各 repository 的 SQL 與資料掃描邏輯。
package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// repository_test.go drives every repository method through go-sqlmock — no real
// MariaDB. It mirrors sqls_test.go: each query/exec expectation is matched with
// regexp.QuoteMeta of the exact SQL, results/args are asserted, and
// ExpectationsWereMet verifies nothing was missed or left pending.

// newMock returns a fresh *sql.DB backed by sqlmock plus the controller.
func newMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// assertMet fails the test if any expectation was unmet.
func assertMet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

var ctx = context.Background()

// --- StockHistoryRepository ---

// TestGetStockName 驗證 GetStockName 依 stock_id 查詢並回傳最新股票名稱。
func TestGetStockName(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1;")).
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"stock_name"}).AddRow("元大台灣50正2"))

	// Act
	name, err := repo.GetStockName(ctx, "00631L")

	// Assert
	if err != nil || name != "元大台灣50正2" {
		t.Fatalf("GetStockName = (%q, %v), want 元大台灣50正2", name, err)
	}
	assertMet(t, mock)
}

// TestGetStockName_NoRows 驗證查無資料時 GetStockName 回傳空字串且無錯誤。
func TestGetStockName_NoRows(t *testing.T) {
	// Arrange — no history → empty string, no error.
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1;")).
		WithArgs("ZZZ").
		WillReturnError(sql.ErrNoRows)

	// Act
	name, err := repo.GetStockName(ctx, "ZZZ")

	// Assert
	if err != nil || name != "" {
		t.Fatalf("GetStockName (no rows) = (%q, %v), want (\"\", nil)", name, err)
	}
	assertMet(t, mock)
}

// TestGetPriceAsOf 驗證 GetPriceAsOf 依日期回傳指定價格欄位的最新值。
func TestGetPriceAsOf(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT 1;")).
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(123.45))

	// Act
	px, err := repo.GetPriceAsOf(ctx, "00631L", "2024-06-06", "close_price")

	// Assert
	if err != nil || px != 123.45 {
		t.Fatalf("GetPriceAsOf = (%.2f, %v), want 123.45", px, err)
	}
	assertMet(t, mock)
}

// TestGetPriceAsOf_RejectsInvalidPriceType 驗證非白名單欄位名稱在發送查詢前即被拒絕。
func TestGetPriceAsOf_RejectsInvalidPriceType(t *testing.T) {
	// Arrange — a non-whitelisted column must be rejected before any query.
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	// Intentionally register NO expectations: a query here would be a failure.

	// Act
	px, err := repo.GetPriceAsOf(ctx, "00631L", "2024-06-06", "1=1; DROP TABLE StockHistory")

	// Assert
	if err == nil {
		t.Fatalf("expected error for invalid priceType, got px=%.2f", px)
	}
	assertMet(t, mock) // proves no query was issued
}

// TestGetClosePricesDescAsOf 驗證 GetClosePricesDescAsOf 以降冪回傳指定日期前的收盤價序列。
func TestGetClosePricesDescAsOf(t *testing.T) {
	// Arrange — newest-first close series.
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;")).
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"close_price"}).AddRow(100.0).AddRow(110.0).AddRow(95.0))

	// Act
	prices, err := repo.GetClosePricesDescAsOf(ctx, "00631L", "2024-06-06")

	// Assert
	if err != nil || len(prices) != 3 || prices[0] != 100.0 || prices[2] != 95.0 {
		t.Fatalf("GetClosePricesDescAsOf = (%v, %v), want [100 110 95]", prices, err)
	}
	assertMet(t, mock)
}

// TestGetCloseHistoryAsc 驗證 GetCloseHistoryAsc 以升冪回傳完整收盤歷史記錄。
func TestGetCloseHistoryAsc(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;")).
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}).AddRow("2024-01-02", 50.0).AddRow("2024-01-03", 51.0))

	// Act
	hist, err := repo.GetCloseHistoryAsc(ctx, "00631L")

	// Assert
	if err != nil || len(hist) != 2 || hist[1].Date != "2024-01-03" || hist[1].ClosePrice != 51.0 {
		t.Fatalf("GetCloseHistoryAsc = (%+v, %v)", hist, err)
	}
	assertMet(t, mock)
}

// TestLoadSeries 驗證 LoadSeries 對多個股票代號各發一次查詢並彙整成 map 回傳。
func TestLoadSeries(t *testing.T) {
	// Arrange — one query per stockID.
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	q := regexp.QuoteMeta("SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;")
	mock.ExpectQuery(q).WithArgs("AAA").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}).AddRow("2024-01-02", 10.0))
	mock.ExpectQuery(q).WithArgs("BBB").
		WillReturnRows(sqlmock.NewRows([]string{"date", "close_price"}).AddRow("2024-01-02", 20.0).AddRow("2024-01-03", 21.0))

	// Act
	series, err := repo.LoadSeries(ctx, []string{"AAA", "BBB"})

	// Assert
	if err != nil || len(series) != 2 || len(series["AAA"]) != 1 || len(series["BBB"]) != 2 || series["BBB"][1].ClosePrice != 21.0 {
		t.Fatalf("LoadSeries = (%+v, %v)", series, err)
	}
	assertMet(t, mock)
}

// TestInsertBarIgnore 驗證 InsertBarIgnore 以 INSERT IGNORE 寫入 K 線資料且不回傳錯誤。
func TestInsertBarIgnore(t *testing.T) {
	// Arrange — value/price_change/transactions intentionally omitted.
	db, mock := newMock(t)
	repo := NewStockHistoryRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("INSERT IGNORE INTO StockHistory (stock_id, stock_name, date, volume, open_price, high_price, low_price, close_price) VALUES (?, ?, ?, ?, ?, ?, ?, ?);")).
		WithArgs("00631L", "元大台灣50正2", "2024-01-02", 1000.0, 50.0, 51.0, 49.0, 50.5).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Act
	err := repo.InsertBarIgnore(ctx, "00631L", "元大台灣50正2", entity.Bar{
		Date: "2024-01-02", Open: 50.0, High: 51.0, Low: 49.0, Close: 50.5, Volume: 1000.0,
	})

	// Assert
	if err != nil {
		t.Fatalf("InsertBarIgnore: %v", err)
	}
	assertMet(t, mock)
}

// --- LedgerRepository ---

const unrealizedCols = "transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares"

// TestLoadAllUnrealized 驗證 LoadAllUnrealized 讀取 UnrealizedGainsLosses 全部記錄並正確掃描欄位。
func TestLoadAllUnrealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + unrealizedCols + " FROM UnrealizedGainsLosses;")).
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
			AddRow("2024-01-02", "00631L", "元大台灣50正2", 50.5, 5050.0, 100).
			AddRow("2024-02-02", "00830", "國泰美國費城半導體", 30.0, 6000.0, 200))

	// Act
	lots, err := repo.LoadAllUnrealized(ctx)

	// Assert
	if err != nil || len(lots) != 2 || lots[0].StockID != "00631L" || lots[0].Shares != 100 || lots[1].InvestmentCost != 6000.0 {
		t.Fatalf("LoadAllUnrealized = (%+v, %v)", lots, err)
	}
	assertMet(t, mock)
}

// TestGetLowestUnrealized_Found 驗證有符合條件的記錄時 GetLowestUnrealized 回傳最低買入成本的持倉。
func TestGetLowestUnrealized_Found(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+unrealizedCols+" FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ? ORDER BY transaction_price ASC, transaction_date ASC LIMIT 1;")).
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
			AddRow("2024-01-02", "00631L", "元大台灣50正2", 50.0, 5000.0, 100))

	// Act
	lot, ok, err := repo.GetLowestUnrealized(ctx, "00631L", "2024-06-06")

	// Assert
	if err != nil || !ok || lot.TransactionPrice != 50.0 || lot.Shares != 100 {
		t.Fatalf("GetLowestUnrealized = (%+v, %v, %v), want found", lot, ok, err)
	}
	assertMet(t, mock)
}

// TestGetLowestUnrealized_NotFound 驗證查無記錄時 GetLowestUnrealized 回傳 ok=false 且無錯誤。
func TestGetLowestUnrealized_NotFound(t *testing.T) {
	// Arrange — empty result set.
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+unrealizedCols+" FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ? ORDER BY transaction_price ASC, transaction_date ASC LIMIT 1;")).
		WithArgs("00631L", "2024-06-06").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}))

	// Act
	_, ok, err := repo.GetLowestUnrealized(ctx, "00631L", "2024-06-06")

	// Assert
	if err != nil || ok {
		t.Fatalf("GetLowestUnrealized (none) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	assertMet(t, mock)
}

// TestInsertUnrealized 驗證 InsertUnrealized 以正確參數寫入一筆未實現損益記錄。
func TestInsertUnrealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO UnrealizedGainsLosses (transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares) VALUES (?, ?, ?, ?, ?, ?);")).
		WithArgs("2024-06-06", "00631L", "元大台灣50正2", 50.0, 500.0, 10).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Act
	err := repo.InsertUnrealized(ctx, entity.UnrealizedGainsLoss{
		TransactionDate: "2024-06-06", StockID: "00631L", StockName: "元大台灣50正2",
		TransactionPrice: 50.0, InvestmentCost: 500.0, Shares: 10,
	})

	// Assert
	if err != nil {
		t.Fatalf("InsertUnrealized: %v", err)
	}
	assertMet(t, mock)
}

// TestDeleteUnrealized 驗證 DeleteUnrealized 以 stock_id 和日期刪除指定持倉記錄。
func TestDeleteUnrealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ?;")).
		WithArgs("00631L", "2024-01-02").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Act + Assert
	if err := repo.DeleteUnrealized(ctx, "00631L", "2024-01-02"); err != nil {
		t.Fatalf("DeleteUnrealized: %v", err)
	}
	assertMet(t, mock)
}

// TestUpdateUnrealized 驗證 UpdateUnrealized 以新成本與股數更新指定持倉記錄。
func TestUpdateUnrealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE UnrealizedGainsLosses SET investment_cost = ?, shares = ? WHERE stock_id = ? AND transaction_date = ?;")).
		WithArgs(3500.0, 70, "00631L", "2024-01-02").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Act + Assert
	if err := repo.UpdateUnrealized(ctx, "00631L", "2024-01-02", 3500.0, 70); err != nil {
		t.Fatalf("UpdateUnrealized: %v", err)
	}
	assertMet(t, mock)
}

// TestInsertRealized 驗證 InsertRealized 以完整欄位寫入一筆已實現損益記錄。
func TestInsertRealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO RealizedGainsLosses (buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);")).
		WithArgs("2024-01-02", "2024-03-02", "00631L", "元大台灣50正2", 50.0, 80.0, 5000.0, 8000.0, 3000.0, 60.0, 100).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Act
	err := repo.InsertRealized(ctx, entity.RealizedGainsLoss{
		BuyDate: "2024-01-02", SellDate: "2024-03-02", StockID: "00631L", StockName: "元大台灣50正2",
		PurchasePrice: 50.0, SellPrice: 80.0, InvestmentCost: 5000.0, Revenue: 8000.0,
		ProfitLoss: 3000.0, ProfitRate: 60.0, Shares: 100,
	})

	// Assert
	if err != nil {
		t.Fatalf("InsertRealized: %v", err)
	}
	assertMet(t, mock)
}

// TestListUnrealized 驗證 ListUnrealized 依日期降冪讀取最多 500 筆未實現損益記錄。
func TestListUnrealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + unrealizedCols + " FROM UnrealizedGainsLosses ORDER BY transaction_date DESC LIMIT 500;")).
		WillReturnRows(sqlmock.NewRows([]string{"transaction_date", "stock_id", "stock_name", "transaction_price", "investment_cost", "shares"}).
			AddRow("2024-01-02", "00631L", "元大台灣50正2", 50.0, 5000.0, 100))

	// Act
	out, err := repo.ListUnrealized(ctx)

	// Assert
	if err != nil || len(out) != 1 || out[0].StockID != "00631L" {
		t.Fatalf("ListUnrealized = (%+v, %v)", out, err)
	}
	assertMet(t, mock)
}

// TestListRealized 驗證 ListRealized 依賣出日期降冪讀取最多 500 筆已實現損益記錄。
func TestListRealized(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares FROM RealizedGainsLosses ORDER BY sell_date DESC LIMIT 500;")).
		WillReturnRows(sqlmock.NewRows([]string{"buy_date", "sell_date", "stock_id", "stock_name", "purchase_price", "sell_price", "investment_cost", "revenue", "profit_loss", "profit_rate", "shares"}).
			AddRow("2024-01-02", "2024-03-02", "00631L", "元大台灣50正2", 50.0, 80.0, 5000.0, 8000.0, 3000.0, 60.0, 100))

	// Act
	out, err := repo.ListRealized(ctx)

	// Assert
	if err != nil || len(out) != 1 || out[0].StockID != "00631L" || out[0].Revenue != 8000.0 {
		t.Fatalf("ListRealized = (%+v, %v)", out, err)
	}
	assertMet(t, mock)
}

// TestLastBuyDateRaw_Found 驗證有買入紀錄時 LastBuyDateRaw 回傳最後買入日期且 ok=true。
func TestLastBuyDateRaw_Found(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery("SELECT MAX").
		WithArgs("00631L", "00631L").
		WillReturnRows(sqlmock.NewRows([]string{"d"}).AddRow("2024-05-20"))

	// Act
	raw, ok, err := repo.LastBuyDateRaw(ctx, "00631L")

	// Assert
	if err != nil || !ok || raw != "2024-05-20" {
		t.Fatalf("LastBuyDateRaw = (%q, %v, %v), want 2024-05-20/true", raw, ok, err)
	}
	assertMet(t, mock)
}

// TestLastBuyDateRaw_Null 驗證從未買入時 LastBuyDateRaw 回傳空字串且 ok=false 無錯誤。
func TestLastBuyDateRaw_Null(t *testing.T) {
	// Arrange — never bought → MAX returns NULL.
	db, mock := newMock(t)
	repo := NewLedgerRepository(db)
	mock.ExpectQuery("SELECT MAX").
		WithArgs("ZZZ", "ZZZ").
		WillReturnRows(sqlmock.NewRows([]string{"d"}).AddRow(nil))

	// Act
	raw, ok, err := repo.LastBuyDateRaw(ctx, "ZZZ")

	// Assert
	if err != nil || ok || raw != "" {
		t.Fatalf("LastBuyDateRaw (null) = (%q, %v, %v), want (\"\", false, nil)", raw, ok, err)
	}
	assertMet(t, mock)
}

// --- BotStateRepository ---

// TestBotState_Set 驗證 Set 以 upsert 方式寫入機器人狀態鍵值。
func TestBotState_Set(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewBotStateRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO BotState (state_key, state_value) VALUES (?, ?) ON DUPLICATE KEY UPDATE state_value = VALUES(state_value);")).
		WithArgs("current_cash", "12345.5").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Act + Assert
	if err := repo.Set(ctx, "current_cash", "12345.5"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	assertMet(t, mock)
}

// TestBotState_Get 驗證 Get 依鍵名讀取機器人狀態並回傳值且 ok=true。
func TestBotState_Get(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewBotStateRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT state_value FROM BotState WHERE state_key = ?;")).
		WithArgs("current_cash").
		WillReturnRows(sqlmock.NewRows([]string{"state_value"}).AddRow("12345.5"))

	// Act
	v, ok, err := repo.Get(ctx, "current_cash")

	// Assert
	if err != nil || !ok || v != "12345.5" {
		t.Fatalf("Get = (%q, %v, %v), want 12345.5/true", v, ok, err)
	}
	assertMet(t, mock)
}

// TestBotState_Get_NoRow 驗證鍵不存在時 Get 回傳 ok=false 且無錯誤。
func TestBotState_Get_NoRow(t *testing.T) {
	// Arrange — missing key → ok=false, no error.
	db, mock := newMock(t)
	repo := NewBotStateRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT state_value FROM BotState WHERE state_key = ?;")).
		WithArgs("last_processed_date").
		WillReturnError(sql.ErrNoRows)

	// Act
	_, ok, err := repo.Get(ctx, "last_processed_date")

	// Assert
	if err != nil || ok {
		t.Fatalf("Get (no row) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	assertMet(t, mock)
}

// --- BackfillRepository ---

// TestCompletedMonths 驗證 CompletedMonths 回傳指定股票已完成回填的月份集合。
func TestCompletedMonths(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewBackfillRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT month FROM BackfillStatus WHERE stock_id = ?;")).
		WithArgs("00631L").
		WillReturnRows(sqlmock.NewRows([]string{"month"}).AddRow("2024-01").AddRow("2024-02"))

	// Act
	got, err := repo.CompletedMonths(ctx, "00631L")

	// Assert
	if err != nil || !got["2024-01"] || !got["2024-02"] || got["2024-03"] {
		t.Fatalf("CompletedMonths = (%v, %v)", got, err)
	}
	assertMet(t, mock)
}

// TestMarkComplete 驗證 MarkComplete 以 INSERT IGNORE 標記指定月份為已完成。
func TestMarkComplete(t *testing.T) {
	// Arrange
	db, mock := newMock(t)
	repo := NewBackfillRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("INSERT IGNORE INTO BackfillStatus (stock_id, month, completed_at) VALUES (?, ?, NOW());")).
		WithArgs("00631L", "2024-01").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Act + Assert
	if err := repo.MarkComplete(ctx, "00631L", "2024-01"); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}
	assertMet(t, mock)
}
