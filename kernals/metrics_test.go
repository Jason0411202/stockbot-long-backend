package kernals

import (
	"math"
	"testing"
	"time"
)

// 所有期望值由獨立 Python 解算器 (Newton + bracket 交叉驗證) 預先算出,故為精確值而非估算。

const tol = 1e-9

func approx(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s = %.15g, want %.15g (diff %.3g > tol %.3g)", name, got, want, math.Abs(got-want), tolerance)
	}
}

var flowBase = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// cf 以 flowBase 為 t=0,建立一筆位於第 days 天的現金流。
func cf(amount float64, days int) Cashflow {
	return Cashflow{Date: flowBase.AddDate(0, 0, days), Amount: amount}
}

func TestXIRR_CanonicalDoubling(t *testing.T) {
	got, ok := xirr([]Cashflow{cf(-100000, 0), cf(200000, 730)})
	if !ok {
		t.Fatalf("expected solvable XIRR")
	}
	approx(t, "XIRR doubling", got, 0.41421356237309515, 1e-7)
}

func TestXIRR_WithIntermediateSell(t *testing.T) {
	got, ok := xirr([]Cashflow{cf(-100000, 0), cf(50000, 365), cf(60000, 730)})
	if !ok {
		t.Fatalf("expected solvable XIRR")
	}
	approx(t, "XIRR w/ sell", got, 0.06394102980498532, 1e-7)
}

func TestXIRR_TotalLossZeroSells(t *testing.T) {
	got, ok := xirr([]Cashflow{cf(-100000, 0), cf(40000, 730)})
	if !ok {
		t.Fatalf("expected solvable XIRR")
	}
	approx(t, "XIRR loss", got, -0.3675444679663241, 1e-7)
}

func TestXIRR_NoSignChange_Undefined(t *testing.T) {
	_, ok := xirr([]Cashflow{cf(-100000, 0), cf(-5000, 90)})
	if ok {
		t.Fatalf("expected XIRR undefined (no positive flow), got ok=true")
	}
}

// 多重變號現金流 (賣出後再買進) -> NPV 多根 -> 資金加權報酬不唯一 -> 回傳 (NaN, false)。
// 沒有這道防呆,舊版會回傳貼著 -0.9999 邊界的人為根 (把現金流誤報成 ~-99%)。
// 經典雙根樣本 [-1,+5,-6] (t=0,1,2y) 的根為 r=1.0 與 r=2.0,兩者皆在掃描範圍內 -> 應判定不唯一。
func TestXIRR_MultiRoot_Ambiguous(t *testing.T) {
	flows := []Cashflow{cf(-100000, 0), cf(500000, 365), cf(-600000, 730)}
	if v, ok := xirr(flows); ok {
		t.Fatalf("expected multi-root flows to be ambiguous (NaN,false), got (%.6f, true)", v)
	}
}

// 鎖定「封閉資金池 CAGR == 資金加權 IRR」的恆等式:唯二外部現金流為期初 -E0 與期末 +EN 時,
// XIRR 解必等於 CAGR。任何未來引入手續費/外部金流而破壞此恆等式都會被這個測試抓到。
func TestXIRR_EqualsPoolCAGR(t *testing.T) {
	x, ok := xirr([]Cashflow{cf(-100000, 0), cf(200000, 730)})
	if !ok {
		t.Fatalf("expected solvable XIRR")
	}
	approx(t, "pool XIRR vs CAGR", x, cagr(100000, 200000, 2.0), 1e-6)
}

func TestCAGR(t *testing.T) {
	approx(t, "CAGR 2y", cagr(100000, 137000, 2.0), 0.17046999107196248, 1e-12)
	approx(t, "CAGR short up", cagr(100000, 110000, 30.0/365.0), 2.188680476905307, 1e-9)
	approx(t, "CAGR short loss", cagr(100000, 80000, 274.0/365.0), -0.2571441552424847, 1e-9)
}

func TestPeriodReturn(t *testing.T) {
	approx(t, "period", periodReturn(100000, 110000), 0.10, tol)
	if !math.IsNaN(periodReturn(0, 100)) {
		t.Fatalf("expected NaN for start<=0")
	}
}

func TestMaxDrawdown(t *testing.T) {
	approx(t, "mdd 10pct",
		maxDrawdown([]float64{100000, 100000, 105000, 98000, 110000, 99000, 99000, 120000}),
		-0.10, 1e-9)
	approx(t, "mdd 33pct",
		maxDrawdown([]float64{100000, 120000, 90000, 110000, 80000, 130000}),
		-1.0/3.0, 1e-9)
	approx(t, "mdd monotonic",
		maxDrawdown([]float64{100000, 101000, 103000, 105000}),
		0.0, 1e-12)
}

func TestSortino(t *testing.T) {
	rets := []float64{0.01, -0.02, 0.015, -0.01, 0.00, 0.02, -0.03}
	approx(t, "downsideDev", downsideDeviation(rets, 0), 0.01414213562373095, 1e-12)
	// 逐期 (未年化) Sortino = mean / downsideDev
	approx(t, "sortino per-period", sortino(rets, 0, 1), -0.15152288168283162, 1e-9)
	// 年化 (×252 / ×sqrt(252))
	approx(t, "sortino annualized", sortino(rets, 0, 252), -2.4053511772118195, 1e-6)
}

func TestCalmar(t *testing.T) {
	approx(t, "calmar std", calmar(0.12, -0.10), 1.2, 1e-12)
	approx(t, "calmar negative", calmar(-0.05, -0.20), -0.25, 1e-12)
	if v := calmar(0.08, 0.0); !math.IsInf(v, 1) {
		t.Fatalf("calmar(0.08, 0) = %v, want +Inf", v)
	}
	if v := calmar(-0.03, 0.0); !math.IsNaN(v) {
		t.Fatalf("calmar(-0.03, 0) = %v, want NaN", v)
	}
}

func TestMedianStdev(t *testing.T) {
	approx(t, "median odd", median([]float64{3, 1, 2}), 2, tol)
	approx(t, "median even", median([]float64{4, 1, 3, 2}), 2.5, tol)
	approx(t, "stdev", stdev([]float64{2, 4, 4, 4, 5, 5, 7, 9}), 2.138089935299395, 1e-9)
}
