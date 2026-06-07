// internal/service/trading/indicators.go 提供策略使用的均線、前綴和與高點指標。
package trading

import "math"

// indicators.go 收錄策略用的技術指標純函式與 StockSeries 上的快取查詢方法。
// 純函式、只看過去資料、無未來洩漏;均線用 PrefixClose 做 O(1) 任意視窗查詢。

// BuildPrefixClose 回傳 prefix sum (len = len(close)+1, prefix[0]=0)。
func BuildPrefixClose(close []float64) []float64 {
	// 多配置一格作為哨兵,使任意 [l,r] 區間和皆可用 p[r+1]-p[l] 算出。
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
		// 累加當日收盤;若已有 window 筆以上則減去最舊一筆,保持滑動視窗。
		sum += p
		if i >= window {
			sum -= prices[i-window]
		}
		// 資料已足 window 筆:寫入平均值;否則標為 NaN 代表資料不足。
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
	// 防禦:視窗為 0 或負時退回預設 20 日。
	if window <= 0 {
		window = 20
	}
	// PrefixClose 長度不符代表尚未初始化,無法計算。
	if len(s.PrefixClose) != len(s.ClosePrices)+1 {
		return math.NaN()
	}
	// 索引越界或資料不足 window 筆時回傳 NaN。
	if i < window-1 || i >= len(s.ClosePrices) {
		return math.NaN()
	}
	// 利用前綴和在 O(1) 內計算 [i-window+1, i] 區間均值。
	return (s.PrefixClose[i+1] - s.PrefixClose[i+1-window]) / float64(window)
}

// peakAt 回傳「近 lookback 日 (含當日) 的最高收盤」;供 peak 深度基準 (距高點回撤)。整條陣列首次計算後快取。
func (s *StockSeries) peakAt(i, lookback int) float64 {
	// 防禦:lookback 為 0 或負時退回預設約一交易年 (252 日)。
	if lookback <= 0 {
		lookback = 252
	}
	// 索引越界直接回 NaN,避免陣列存取越界。
	if i < 0 || i >= len(s.ClosePrices) {
		return math.NaN()
	}
	// 延遲初始化快取 map。
	if s.peakCache == nil {
		s.peakCache = make(map[int][]float64)
	}
	// 同一 lookback 只計算一次,後續查詢直接從快取取值。
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
		// 計算滑動視窗起點;序列開頭不足 window 筆時從第 0 筆開始。
		lo := i - window + 1
		if lo < 0 {
			lo = 0
		}
		// 線性掃描視窗內所有收盤取最大值。
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
