// internal/repository/equity_repository.go 存取 EquityHistory 每日權益快照資料表。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// EquityHistoryRepository 存取 EquityHistory 表 (PK date),持久化上線引擎逐日的真實帳戶權益快照。
// 線上引擎以 RecordEquity 逐日 upsert;績效讀取以 ListEquityAsc 取回升冪全序列供折線圖。
// 數值的計算與捨入由服務層負責,本層僅做讀寫。
type EquityHistoryRepository struct {
	db *sql.DB
}

// NewEquityHistoryRepository 以傳入的連線池建立 EquityHistoryRepository。
func NewEquityHistoryRepository(db *sql.DB) *EquityHistoryRepository {
	return &EquityHistoryRepository{db: db}
}

// RecordEquity 以 upsert 寫入某交易日的權益快照;date 已存在時覆寫現金 / 持股市值 / 總權益。
// 同一天被 catch-up 與每日 loop 重覆處理時覆寫而非重複插入。
func (r *EquityHistoryRepository) RecordEquity(ctx context.Context, snap entity.EquitySnapshot) error {
	const query = "INSERT INTO EquityHistory (date, cash, holding_value, total_equity, cost_basis) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE cash = VALUES(cash), holding_value = VALUES(holding_value), total_equity = VALUES(total_equity), cost_basis = VALUES(cost_basis);"
	if _, err := r.db.ExecContext(ctx, query, snap.Date, snap.Cash, snap.HoldingValue, snap.TotalEquity, snap.CostBasis); err != nil {
		return fmt.Errorf("upsert equity history %q: %w", snap.Date, err)
	}
	return nil
}

// ListEquityAsc 以日期升冪回傳所有每日權益快照 (date 為 "YYYY-MM-DD" 字串,字典序即時序)。
func (r *EquityHistoryRepository) ListEquityAsc(ctx context.Context) ([]entity.EquitySnapshot, error) {
	const query = "SELECT date, cash, holding_value, total_equity, cost_basis FROM EquityHistory ORDER BY date ASC;"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query equity history: %w", err)
	}
	defer rows.Close()

	// 逐列掃描成 EquitySnapshot 並彙整成升冪切片。
	var out []entity.EquitySnapshot
	for rows.Next() {
		var s entity.EquitySnapshot
		if err := rows.Scan(&s.Date, &s.Cash, &s.HoldingValue, &s.TotalEquity, &s.CostBasis); err != nil {
			return nil, fmt.Errorf("scan equity history: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate equity history: %w", err)
	}
	return out, nil
}
