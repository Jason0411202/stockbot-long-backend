package kernals

import (
	"math"
	"testing"
)

func TestSplitAdjust_ForwardSplit(t *testing.T) {
	// 1:4 正向分割發生在 idx1→idx2 (102 → 25.5,ratio 0.25)。
	closes := []float64{100, 102, 25.5, 26}
	highs := []float64{101, 103, 25.8, 26.5}
	applySplitAdjust(closes, highs)
	// 分割前 (idx 0,1) 應被縮小 ×0.25,分割後不變 → 序列連續。
	want := []float64{25, 25.5, 25.5, 26}
	for i, w := range want {
		if math.Abs(closes[i]-w) > 1e-9 {
			t.Fatalf("close[%d]=%.4f, want %.4f", i, closes[i], w)
		}
	}
	if math.Abs(highs[0]-25.25) > 1e-9 {
		t.Fatalf("high[0]=%.4f, want 25.25 (high 同步 back-adjust)", highs[0])
	}
}

func TestSplitAdjust_ReverseSplit(t *testing.T) {
	// 1:N 反向分割 (idx1→idx2:11 → 44,ratio 4)。
	closes := []float64{10, 11, 44, 45}
	applySplitAdjust(closes)
	want := []float64{40, 44, 44, 45}
	for i, w := range want {
		if math.Abs(closes[i]-w) > 1e-9 {
			t.Fatalf("close[%d]=%.4f, want %.4f", i, closes[i], w)
		}
	}
}

func TestSplitAdjust_NoSplit_Unchanged(t *testing.T) {
	// 一般行情 + 除息小跳空 (~3%) 都不應被當成分割。
	closes := []float64{100, 105, 98, 95.2, 110}
	cp := append([]float64(nil), closes...)
	applySplitAdjust(closes)
	for i := range closes {
		if math.Abs(closes[i]-cp[i]) > 1e-12 {
			t.Fatalf("close[%d] 被改動 = %.4f, want 原值 %.4f (非分割不應調整)", i, closes[i], cp[i])
		}
	}
}
