// internal/repository/ledger_repository.go 存取未實現與已實現損益 ledger。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// LedgerRepository 存取 UnrealizedGainsLosses 與 RealizedGainsLosses 兩張資料表。
// 本層不含商業邏輯:成本與股數由呼叫端計算後傳入,此處僅負責持久化。
type LedgerRepository struct {
	db *sql.DB
}

// NewLedgerRepository 以傳入的連線池建立 LedgerRepository。
func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// unrealizedColumns 集中列出未實現持倉查詢需要掃描的欄位。
const unrealizedColumns = "transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares"

// scanUnrealized 將目前 rows 游標掃描成單筆未實現持倉 entity。
func scanUnrealized(rows *sql.Rows) (entity.UnrealizedGainsLoss, error) {
	var e entity.UnrealizedGainsLoss
	err := rows.Scan(&e.TransactionDate, &e.StockID, &e.StockName, &e.TransactionPrice, &e.InvestmentCost, &e.Shares)
	return e, err
}

// LoadAllUnrealized 回傳 UnrealizedGainsLosses 中的所有持倉紀錄,供上線引擎啟動時還原記憶體狀態。
func (r *LedgerRepository) LoadAllUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error) {
	query := "SELECT " + unrealizedColumns + " FROM UnrealizedGainsLosses;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query unrealized lots: %w", err)
	}
	defer rows.Close()

	// 逐列掃描並收集所有未實現持倉。
	out := make([]entity.UnrealizedGainsLoss, 0)
	for rows.Next() {
		e, err := scanUnrealized(rows)
		if err != nil {
			return nil, fmt.Errorf("scan unrealized lot: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unrealized lots: %w", err)
	}
	return out, nil
}

// GetLowestUnrealized 回傳 stockID 在 asOf 日期以前成本最低的未實現持倉 (最低 transaction_price,相同時取最早日期)。
// 查無資料時 ok=false。
func (r *LedgerRepository) GetLowestUnrealized(ctx context.Context, stockID, asOf string) (entity.UnrealizedGainsLoss, bool, error) {
	query := "SELECT " + unrealizedColumns + " FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ? ORDER BY transaction_price ASC, transaction_date ASC LIMIT 1;"
	rows, err := r.db.QueryContext(ctx, query, stockID, asOf)
	if err != nil {
		return entity.UnrealizedGainsLoss{}, false, fmt.Errorf("query lowest unrealized lot for %s: %w", stockID, err)
	}
	defer rows.Close()

	// 無資料列時回傳 ok=false。
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return entity.UnrealizedGainsLoss{}, false, fmt.Errorf("query lowest unrealized lot for %s: %w", stockID, err)
		}
		return entity.UnrealizedGainsLoss{}, false, nil
	}
	e, err := scanUnrealized(rows)
	if err != nil {
		return entity.UnrealizedGainsLoss{}, false, fmt.Errorf("scan lowest unrealized lot for %s: %w", stockID, err)
	}
	return e, true, nil
}

// InsertUnrealized 寫入單筆未實現持倉。investment_cost 由呼叫端計算後傳入。
func (r *LedgerRepository) InsertUnrealized(ctx context.Context, e entity.UnrealizedGainsLoss) error {
	const query = "INSERT INTO UnrealizedGainsLosses (transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares) VALUES (?, ?, ?, ?, ?, ?);"
	if _, err := r.db.ExecContext(ctx, query, e.TransactionDate, e.StockID, e.StockName, e.TransactionPrice, e.InvestmentCost, e.Shares); err != nil {
		return fmt.Errorf("insert unrealized lot for %s on %s: %w", e.StockID, e.TransactionDate, err)
	}
	return nil
}

// DeleteUnrealized 刪除以 (stockID, transactionDate) 為鍵的未實現持倉紀錄。
func (r *LedgerRepository) DeleteUnrealized(ctx context.Context, stockID, transactionDate string) error {
	const query = "DELETE FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ?;"
	if _, err := r.db.ExecContext(ctx, query, stockID, transactionDate); err != nil {
		return fmt.Errorf("delete unrealized lot for %s on %s: %w", stockID, transactionDate, err)
	}
	return nil
}

// UpdateUnrealized 更新部分賣出後以 (stockID, transactionDate) 為鍵的持倉剩餘 investment_cost 與 shares。
func (r *LedgerRepository) UpdateUnrealized(ctx context.Context, stockID, transactionDate string, investmentCost float64, shares int) error {
	const query = "UPDATE UnrealizedGainsLosses SET investment_cost = ?, shares = ? WHERE stock_id = ? AND transaction_date = ?;"
	if _, err := r.db.ExecContext(ctx, query, investmentCost, shares, stockID, transactionDate); err != nil {
		return fmt.Errorf("update unrealized lot for %s on %s: %w", stockID, transactionDate, err)
	}
	return nil
}

// InsertRealized 寫入單筆已實現損益紀錄,所有數值由呼叫端計算後傳入。
func (r *LedgerRepository) InsertRealized(ctx context.Context, e entity.RealizedGainsLoss) error {
	const query = "INSERT INTO RealizedGainsLosses (buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
	if _, err := r.db.ExecContext(ctx, query, e.BuyDate, e.SellDate, e.StockID, e.StockName, e.PurchasePrice, e.SellPrice, e.InvestmentCost, e.Revenue, e.ProfitLoss, e.ProfitRate, e.Shares); err != nil {
		return fmt.Errorf("insert realized P&L for %s (%s->%s): %w", e.StockID, e.BuyDate, e.SellDate, err)
	}
	return nil
}

// ListUnrealized 回傳最新 500 筆未實現持倉,依 transaction_date 降冪排序,供 API 端使用。
func (r *LedgerRepository) ListUnrealized(ctx context.Context) ([]entity.UnrealizedGainsLoss, error) {
	query := "SELECT " + unrealizedColumns + " FROM UnrealizedGainsLosses ORDER BY transaction_date DESC LIMIT 500;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list unrealized lots: %w", err)
	}
	defer rows.Close()

	// 逐列掃描並收集查詢結果。
	out := make([]entity.UnrealizedGainsLoss, 0)
	for rows.Next() {
		e, err := scanUnrealized(rows)
		if err != nil {
			return nil, fmt.Errorf("scan unrealized lot: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unrealized lots: %w", err)
	}
	return out, nil
}

// ListRealized 回傳最新 500 筆已實現損益,依 sell_date 降冪排序。
func (r *LedgerRepository) ListRealized(ctx context.Context) ([]entity.RealizedGainsLoss, error) {
	const query = "SELECT buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares FROM RealizedGainsLosses ORDER BY sell_date DESC LIMIT 500;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list realized P&L: %w", err)
	}
	defer rows.Close()

	// 逐列掃描所有欄位並收集至切片。
	out := make([]entity.RealizedGainsLoss, 0)
	for rows.Next() {
		var e entity.RealizedGainsLoss
		if err := rows.Scan(&e.BuyDate, &e.SellDate, &e.StockID, &e.StockName, &e.PurchasePrice, &e.SellPrice, &e.InvestmentCost, &e.Revenue, &e.ProfitLoss, &e.ProfitRate, &e.Shares); err != nil {
			return nil, fmt.Errorf("scan realized P&L row: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realized P&L: %w", err)
	}
	return out, nil
}

// LastBuyDateRaw 以 UNION ALL 合併 UnrealizedGainsLosses.transaction_date 與 RealizedGainsLosses.buy_date,
// 回傳 stockID 最新一筆買入日的原始字串。查無資料或 NULL 時 ok=false,由呼叫端解析成 time.Time。
func (r *LedgerRepository) LastBuyDateRaw(ctx context.Context, stockID string) (string, bool, error) {
	const query = `
		SELECT MAX(d) FROM (
			SELECT MAX(transaction_date) AS d FROM UnrealizedGainsLosses WHERE stock_id = ?
			UNION ALL
			SELECT MAX(buy_date) AS d FROM RealizedGainsLosses WHERE stock_id = ?
		) t;`
	var dateStr sql.NullString
	err := r.db.QueryRowContext(ctx, query, stockID, stockID).Scan(&dateStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query last buy date for %s: %w", stockID, err)
	}
	// NULL 或空字串表示該股從未買入。
	if !dateStr.Valid || dateStr.String == "" {
		return "", false, nil
	}
	return dateStr.String, true, nil
}
