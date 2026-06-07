// internal/dto/performance_test.go 驗證 JSONFloat 對 NaN/Inf 的安全序列化。
package dto

import (
	"encoding/json"
	"math"
	"testing"
)

// TestJSONFloat_MarshalsNaNInfAsNull 驗證 NaN / ±Inf 編成 null、一般值照常輸出。
func TestJSONFloat_MarshalsNaNInfAsNull(t *testing.T) {
	cases := []struct {
		name string
		in   JSONFloat
		want string
	}{
		{"一般值", JSONFloat(1.25), "1.25"},
		{"零", JSONFloat(0), "0"},
		{"負值", JSONFloat(-0.3), "-0.3"},
		{"NaN", JSONFloat(math.NaN()), "null"},
		{"正無限", JSONFloat(math.Inf(1)), "null"},
		{"負無限", JSONFloat(math.Inf(-1)), "null"},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.in)
		if err != nil {
			t.Fatalf("%s: Marshal error: %v", c.name, err)
		}
		if string(b) != c.want {
			t.Fatalf("%s: Marshal = %s, want %s", c.name, b, c.want)
		}
	}
}

// TestPerformanceSummary_MarshalsWithNaNMetrics 驗證內含 NaN 指標的完整摘要可成功序列化 (不回錯)。
func TestPerformanceSummary_MarshalsWithNaNMetrics(t *testing.T) {
	s := PerformanceSummary{
		InitialCash:   100000,
		TotalInvested: 112500,
		Backtest: &BacktestPerformance{
			Strategy: ArmMetrics{Calmar: JSONFloat(math.Inf(1)), Sortino: JSONFloat(math.NaN())},
			WalkForward: WalkForwardScore{
				MedStratCalmar: JSONFloat(math.NaN()),
			},
		},
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatalf("Marshal PerformanceSummary with NaN/Inf metrics failed: %v", err)
	}
}
