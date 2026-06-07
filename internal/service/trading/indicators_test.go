// internal/service/trading/indicators_test.go 驗證均線、前綴和、高點等技術指標函式。
package trading

import (
	"math"
	"testing"
)

// indicators_test.go 為技術指標純函式的單元測試 (金字塔最底層):
// BuildPrefixClose / maAt / peakAt / rollingMax / RollingMA。皆只看過去資料、無未來洩漏。

// TestBuildPrefixClose 驗證 BuildPrefixClose 回傳長度 n+1 的前綴和陣列,prefix[0]=0 且各項累加正確。
func TestBuildPrefixClose(t *testing.T) {
	// Arrange
	close := []float64{10, 20, 30}

	// Act
	p := BuildPrefixClose(close)

	// Assert — 長度 = n+1、prefix[0]=0、prefix[i] = 前 i 項和。
	if len(p) != len(close)+1 {
		t.Fatalf("prefix len = %d, want %d", len(p), len(close)+1)
	}
	want := []float64{0, 10, 30, 60}
	for i, w := range want {
		if p[i] != w {
			t.Fatalf("prefix[%d] = %g, want %g", i, p[i], w)
		}
	}
}

// TestMaAt 驗證 maAt 以前綴和 O(1) 查詢任意視窗均線,資料不足或索引越界時回傳 NaN。
func TestMaAt(t *testing.T) {
	// Arrange — PrefixClose 必須建立,maAt 才能 O(1) 查任意視窗均線。
	s := seriesFrom(mustDate(t, "2020-01-01"), []float64{10, 12, 14, 16, 18})

	cases := []struct {
		name   string
		i      int
		window int
		want   float64
		isNaN  bool
	}{
		{"window 3 at idx2", 2, 3, (10 + 12 + 14) / 3.0, false},
		{"window 3 at idx4", 4, 3, (14 + 16 + 18) / 3.0, false},
		{"insufficient data NaN", 1, 3, 0, true},
		{"window<=0 falls back to 20 → NaN here", 4, 0, 0, true},
		{"index out of range NaN", 99, 3, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			got := s.maAt(c.i, c.window)

			// Assert
			if c.isNaN {
				if !math.IsNaN(got) {
					t.Fatalf("maAt(%d,%d) = %g, want NaN", c.i, c.window, got)
				}
				return
			}
			if math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("maAt(%d,%d) = %g, want %g", c.i, c.window, got, c.want)
			}
		})
	}
}

// TestMaAt_NoPrefixClose_NaN 驗證缺少 PrefixClose 時 maAt 安全回傳 NaN 而不 panic。
func TestMaAt_NoPrefixClose_NaN(t *testing.T) {
	// Arrange — 缺 PrefixClose (長度不符) 時 maAt 必須安全回 NaN,不可 panic。
	s := &StockSeries{ClosePrices: []float64{1, 2, 3}}

	// Act + Assert
	if got := s.maAt(2, 2); !math.IsNaN(got) {
		t.Fatalf("maAt without PrefixClose = %g, want NaN", got)
	}
}

// TestRollingMA 驗證 RollingMA 前 window-1 格為 NaN 暖機期,其後各點為正確的視窗平均值。
func TestRollingMA(t *testing.T) {
	// Arrange
	prices := []float64{1, 2, 3, 4, 5}

	// Act — window 3。
	out := RollingMA(prices, 3)

	// Assert — 前 2 格資料不足為 NaN,其後為視窗均值。
	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatalf("RollingMA warm-up should be NaN, got %v", out[:2])
	}
	wants := map[int]float64{2: 2, 3: 3, 4: 4}
	for i, w := range wants {
		if math.Abs(out[i]-w) > 1e-9 {
			t.Fatalf("RollingMA[%d] = %g, want %g", i, out[i], w)
		}
	}
}

// TestRollingMax 驗證 rollingMax 回傳近 window 日 (含當日) 的滾動最高價序列。
func TestRollingMax(t *testing.T) {
	// Arrange
	prices := []float64{3, 1, 4, 1, 5, 9, 2}

	// Act — 近 3 日 (含當日) 最高。
	out := rollingMax(prices, 3)

	// Assert
	want := []float64{3, 3, 4, 4, 5, 9, 9}
	for i, w := range want {
		if out[i] != w {
			t.Fatalf("rollingMax[%d] = %g, want %g", i, out[i], w)
		}
	}
}

// TestPeakAt_CachesAndQueries 驗證 peakAt 回傳指定回看窗口內的最高價,重複查詢命中快取且索引越界回傳 NaN。
func TestPeakAt_CachesAndQueries(t *testing.T) {
	// Arrange
	s := seriesFrom(mustDate(t, "2020-01-01"), []float64{5, 7, 6, 9, 8})

	// Act — 近 2 日最高;第二次查命中快取 (同值)。
	first := s.peakAt(3, 2)  // max(6,9)=9
	second := s.peakAt(3, 2) // 走快取

	// Assert
	if first != 9 || second != 9 {
		t.Fatalf("peakAt(3,2) = %g/%g, want 9/9", first, second)
	}
	if math.IsNaN(s.peakAt(2, 252)) {
		t.Fatalf("peakAt with lookback>len should clamp, not NaN")
	}
	// 越界索引回 NaN。
	if !math.IsNaN(s.peakAt(-1, 2)) {
		t.Fatalf("peakAt(-1) should be NaN")
	}
}
