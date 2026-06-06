package backtest

import (
	"math"
	"testing"
)

// misc_test.go 補齊小型純函式的邊界分支 (safeMean/safeDiv 的零除、cagr 的退化情形、
// dailyReturns、monthsBetween、sortInts)。regime ma_slope 的測試屬於 trading 層,留在該套件。

func TestSafeMeanDiv(t *testing.T) {
	if safeMean(0, 0) != 0 {
		t.Fatalf("safeMean(0,0) should be 0")
	}
	if safeMean(10, 2) != 5 {
		t.Fatalf("safeMean(10,2) = %g, want 5", safeMean(10, 2))
	}
	if safeDiv(10, 0) != 0 {
		t.Fatalf("safeDiv(_,0) should be 0")
	}
	if safeDiv(10, 2) != 5 {
		t.Fatalf("safeDiv(10,2) = %g, want 5", safeDiv(10, 2))
	}
}

func TestCAGR_DegenerateCases(t *testing.T) {
	if !math.IsNaN(cagr(0, 100, 1)) {
		t.Fatalf("cagr(start<=0) should be NaN")
	}
	if !math.IsNaN(cagr(100, 200, 0)) {
		t.Fatalf("cagr(years<=0) should be NaN")
	}
	if got := cagr(100, 0, 2); got != -1 {
		t.Fatalf("cagr(end<=0) = %g, want -1 (total loss)", got)
	}
}

func TestDailyReturns(t *testing.T) {
	// 少於兩點 → nil。
	if dailyReturns([]float64{100}) != nil {
		t.Fatalf("dailyReturns of single point should be nil")
	}
	// 含「前一日為 0」→ 該日報酬以 0 計 (避免除零)。
	got := dailyReturns([]float64{0, 50, 100})
	if len(got) != 2 || got[0] != 0 || math.Abs(got[1]-1.0) > 1e-12 {
		t.Fatalf("dailyReturns = %v, want [0, 1.0]", got)
	}
}

func TestMonthsBetween(t *testing.T) {
	if got := monthsBetween(mustDate(t, "2020-01-15"), mustDate(t, "2020-04-10")); got != 3 {
		t.Fatalf("monthsBetween = %d, want 3", got)
	}
	// b 早於 a → 夾到 0。
	if got := monthsBetween(mustDate(t, "2020-06-01"), mustDate(t, "2020-01-01")); got != 0 {
		t.Fatalf("monthsBetween(reversed) = %d, want 0", got)
	}
}

func TestSortInts(t *testing.T) {
	xs := []int{5, 1, 4, 2, 3}
	sortInts(xs)
	for i := 1; i < len(xs); i++ {
		if xs[i-1] > xs[i] {
			t.Fatalf("sortInts not ascending: %v", xs)
		}
	}
}
