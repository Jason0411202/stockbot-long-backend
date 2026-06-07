// internal/service/trading/splits_test.go 驗證 ApplySplitAdjust 對正向分割、反向分割及非分割行情的還原正確性。
package trading

import (
	"math"
	"testing"
)

// TestSplitAdjust_ForwardSplit 驗證正向股票分割時,分割前的收盤價與最高價皆按比例縮小以維持序列連續。
func TestSplitAdjust_ForwardSplit(t *testing.T) {
	// 1:4 正向分割發生在 idx1→idx2 (102 → 25.5,ratio 0.25)。
	closes := []float64{100, 102, 25.5, 26}
	highs := []float64{101, 103, 25.8, 26.5}
	ApplySplitAdjust(closes, highs)
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

// TestSplitAdjust_ReverseSplit 驗證反向股票分割時,分割前的收盤價按倍率放大以維持序列連續。
func TestSplitAdjust_ReverseSplit(t *testing.T) {
	// 1:N 反向分割 (idx1→idx2:11 → 44,ratio 4)。
	closes := []float64{10, 11, 44, 45}
	ApplySplitAdjust(closes)
	want := []float64{40, 44, 44, 45}
	for i, w := range want {
		if math.Abs(closes[i]-w) > 1e-9 {
			t.Fatalf("close[%d]=%.4f, want %.4f", i, closes[i], w)
		}
	}
}

// TestSplitAdjust_NoSplit_Unchanged 驗證正常行情與除息小跳空不被誤判為分割,價格序列維持不變。
func TestSplitAdjust_NoSplit_Unchanged(t *testing.T) {
	// 一般行情 + 除息小跳空 (~3%) 都不應被當成分割。
	closes := []float64{100, 105, 98, 95.2, 110}
	cp := append([]float64(nil), closes...)
	ApplySplitAdjust(closes)
	for i := range closes {
		if math.Abs(closes[i]-cp[i]) > 1e-12 {
			t.Fatalf("close[%d] 被改動 = %.4f, want 原值 %.4f (非分割不應調整)", i, closes[i], cp[i])
		}
	}
}
