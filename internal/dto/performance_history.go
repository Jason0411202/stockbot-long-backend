// internal/dto/performance_history.go 定義統一日期時間軸的策略績效歷史 API 回應 DTO
// (回測 + 實盤兩組指標對齊同一條日期軸,供前端折線圖)。
package dto

// PerformanceHistoryPoint 是 GET /api/get_performance_history 的單一日期點:
// 同一交易日同時帶「回測」(全期皆有值) 與「實盤」(go-live 後才有,之前為 null) 兩組指標,共用同一 invested。
//
// 單位約定:
//   - 金額 (equity / cash / pnl / invested / cost) = 元
//   - 倍數 (*_multiple / multiple) = 倍 (如 13.21)
//   - 比率 (*_return_rate / *_cagr / ratio / drawdown / max_drawdown) = 百分比 (如 +44.4 或 -24.7)
//
// 實盤欄位以指標型 (*float64) 承載:go-live 前該日無實盤資料 → nil → JSON null;
// 回測比率在極早期 (年化基期過短) 不可靠時 strat_cagr 亦為 null。
type PerformanceHistoryPoint struct {
	Date     string  `json:"date"`     // 交易日 (YYYY-MM-DD)
	Invested float64 `json:"invested"` // 投入本金到當日 (期初 + 截至當日累計注資;lump-sum 為常數)

	// ── 回測 (全期皆有值,自共同上市日起) ──
	StratEquity     float64  `json:"strat_equity"`      // 策略當日總權益
	BHEquity        float64  `json:"bh_equity"`         // Buy & Hold 當日總權益
	StratMultiple   float64  `json:"strat_multiple"`    // 策略本金倍數 = strat_equity / invested
	BHMultiple      float64  `json:"bh_multiple"`       // B&H 本金倍數
	StratReturnRate float64  `json:"strat_return_rate"` // 策略累計報酬率 (%)
	BHReturnRate    float64  `json:"bh_return_rate"`    // B&H 累計報酬率 (%)
	StratDrawdown   float64  `json:"strat_drawdown"`    // 策略當日距歷史高點回撤 (%,<=0)
	BHDrawdown      float64  `json:"bh_drawdown"`       // B&H 當日距歷史高點回撤 (%,<=0)
	StratCAGR       *float64 `json:"strat_cagr"`        // 策略年化報酬 (%);基期過短時為 null

	// ── 實盤 (go-live 後才有,之前皆為 null) ──
	Cash            *float64 `json:"cash"`              // 當日閒置現金 (預備現金)
	HoldingValue    *float64 `json:"holding_value"`     // 當日持股市值
	TotalEquity     *float64 `json:"total_equity"`      // 當日總權益 = 現金 + 持股市值
	HoldingRatio    *float64 `json:"holding_ratio"`     // 持股佔總資產比例 (%)
	CashRatio       *float64 `json:"cash_ratio"`        // 預備現金佔總資產比例 (%)
	TotalPnL        *float64 `json:"total_pnl"`         // 總損益 = 總權益 − 投入本金
	TotalReturnRate *float64 `json:"total_return_rate"` // 總報酬率 (%)
	Multiple        *float64 `json:"multiple"`          // 實盤本金倍數 = total_equity / invested
	RealizedPnL     *float64 `json:"realized_pnl"`      // 累計已實現損益
	UnrealizedPnL   *float64 `json:"unrealized_pnl"`    // 當日未實現損益 = 持股市值 − 成本基礎
	CAGR            *float64 `json:"cagr"`              // 實盤年化報酬 (%);上線基期過短時為 null
	MaxDrawdown     *float64 `json:"max_drawdown"`      // 實盤當日距上線以來高點回撤 (%,<=0)
}
