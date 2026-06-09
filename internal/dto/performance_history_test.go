// internal/dto/performance_history_test.go 釘住 PerformanceHistoryPoint 的 JSON wire 契約
// (欄位名、實盤 nil→null、邊界數值編碼不失敗),對齊前端需求文件的 JSON 範例。
package dto

import (
	"encoding/json"
	"strings"
	"testing"
)

// fp 回傳指向 x 的 *float64 (供建構實盤欄位)。
func fp(x float64) *float64 { return &x }

// TestPerformanceHistoryPoint_WireContract_PreGoLive 驗證 go-live 前:回測欄位有值、實盤欄位全為 JSON null。
func TestPerformanceHistoryPoint_WireContract_PreGoLive(t *testing.T) {
	pre := PerformanceHistoryPoint{
		Date: "2019-05-02", Invested: 1000000,
		StratEquity: 1180000, BHEquity: 1240000,
		StratMultiple: 1.18, BHMultiple: 1.24,
		StratReturnRate: 18.0, BHReturnRate: 24.0,
		StratDrawdown: -8.3, BHDrawdown: -15.1, StratCAGR: fp(12.9),
		// 實盤欄位全部留 nil。
	}
	b, err := json.Marshal(pre)
	if err != nil {
		t.Fatalf("marshal pre go-live point failed (不得讓 JSON 編碼失敗): %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"date":"2019-05-02"`, `"invested":1000000`, `"strat_equity":1180000`, `"bh_equity":1240000`,
		`"strat_multiple":1.18`, `"bh_multiple":1.24`, `"strat_return_rate":18`, `"bh_return_rate":24`,
		`"strat_drawdown":-8.3`, `"bh_drawdown":-15.1`, `"strat_cagr":12.9`,
		`"cash":null`, `"holding_value":null`, `"total_equity":null`, `"holding_ratio":null`,
		`"cash_ratio":null`, `"total_pnl":null`, `"total_return_rate":null`, `"multiple":null`,
		`"realized_pnl":null`, `"unrealized_pnl":null`, `"cagr":null`, `"max_drawdown":null`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("pre go-live JSON 缺少 %s\n完整: %s", want, s)
		}
	}
}

// TestPerformanceHistoryPoint_WireContract_PostGoLive 驗證 go-live 後:實盤欄位填入並正確序列化。
func TestPerformanceHistoryPoint_WireContract_PostGoLive(t *testing.T) {
	post := PerformanceHistoryPoint{
		Date: "2026-05-11", Invested: 1000000,
		StratEquity: 2418000, BHEquity: 1905000,
		StratMultiple: 2.418, BHMultiple: 1.905,
		StratReturnRate: 141.8, BHReturnRate: 90.5,
		StratDrawdown: -18.4, BHDrawdown: -47.1, StratCAGR: fp(12.9),
		Cash: fp(372140), HoldingValue: fp(871360), TotalEquity: fp(1243500),
		HoldingRatio: fp(70.07), CashRatio: fp(29.93), TotalPnL: fp(243500),
		TotalReturnRate: fp(24.35), Multiple: fp(1.2435),
		RealizedPnL: fp(118240), UnrealizedPnL: fp(125260), CAGR: fp(24.35), MaxDrawdown: fp(-9.8),
	}
	b, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post go-live point failed: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"total_equity":1243500`, `"holding_value":871360`, `"cash":372140`,
		`"holding_ratio":70.07`, `"cash_ratio":29.93`, `"total_pnl":243500`,
		`"total_return_rate":24.35`, `"multiple":1.2435`,
		`"realized_pnl":118240`, `"unrealized_pnl":125260`, `"cagr":24.35`, `"max_drawdown":-9.8`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("post go-live JSON 缺少 %s\n完整: %s", want, s)
		}
	}
}
