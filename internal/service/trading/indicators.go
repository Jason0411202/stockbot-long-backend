package trading

import "math"

// indicators.go 收錄策略用的技術指標純函式與 StockSeries 上的快取查詢方法。
// 純函式、只看過去資料、無未來洩漏;均線用 PrefixClose 做 O(1) 任意視窗查詢。

// BuildPrefixClose 回傳 prefix sum (len = len(close)+1, prefix[0]=0)。
func BuildPrefixClose(close []float64) []float64 {
	p := make([]float64, len(close)+1)
	for i, c := range close {
		p[i+1] = p[i] + c
	}
	return p
}

// RollingMA 回傳 window 日簡單移動平均;不足 window 日的位置為 NaN。
func RollingMA(prices []float64, window int) []float64 {
	out := make([]float64, len(prices))
	sum := 0.0
	for i, p := range prices {
		sum += p
		if i >= window {
			sum -= prices[i-window]
		}
		if i >= window-1 {
			out[i] = sum / float64(window)
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

// maAt 回傳「截至 index i 的 window 日均價」。資料不足 (i < window-1) 回傳 NaN。
// 需要 PrefixClose 已建立;否則退回 NaN (呼叫端在預設路徑不會走到這裡)。
func (s *StockSeries) maAt(i, window int) float64 {
	if window <= 0 {
		window = 20
	}
	if len(s.PrefixClose) != len(s.ClosePrices)+1 {
		return math.NaN()
	}
	if i < window-1 || i >= len(s.ClosePrices) {
		return math.NaN()
	}
	return (s.PrefixClose[i+1] - s.PrefixClose[i+1-window]) / float64(window)
}

// peakAt 回傳「近 lookback 日 (含當日) 的最高收盤」;供 peak 深度基準 (距高點回撤)。整條陣列首次計算後快取。
func (s *StockSeries) peakAt(i, lookback int) float64 {
	if lookback <= 0 {
		lookback = 252
	}
	if i < 0 || i >= len(s.ClosePrices) {
		return math.NaN()
	}
	if s.peakCache == nil {
		s.peakCache = make(map[int][]float64)
	}
	arr, ok := s.peakCache[lookback]
	if !ok {
		arr = rollingMax(s.ClosePrices, lookback)
		s.peakCache[lookback] = arr
	}
	return arr[i]
}

// rollingMax 回傳「近 window 日 (含當日) 最高值」陣列 (naive O(n·window),結果會被快取)。
func rollingMax(prices []float64, window int) []float64 {
	out := make([]float64, len(prices))
	for i := range prices {
		lo := i - window + 1
		if lo < 0 {
			lo = 0
		}
		m := prices[lo]
		for j := lo + 1; j <= i; j++ {
			if prices[j] > m {
				m = prices[j]
			}
		}
		out[i] = m
	}
	return out
}
