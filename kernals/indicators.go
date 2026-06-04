package kernals

import "math"

// indicators.go 收錄掃描優化用的技術指標純函式與 stockSeries 上的快取查詢方法。
// 設計原則:
//   - 純函式、只看過去資料、無未來洩漏 (rsi[i] 只用 close[0..i])。
//   - 重指標 (RSI) 以「整條陣列一次算好 + 快取」攤平成本,任意 period 第一次用時才算。
//   - 均線用 prefixClose 做 O(1) 任意視窗查詢,免為每個掃描值重算。

// buildPrefixClose 回傳 prefix sum (len = len(close)+1, prefix[0]=0)。
func buildPrefixClose(close []float64) []float64 {
	p := make([]float64, len(close)+1)
	for i, c := range close {
		p[i+1] = p[i] + c
	}
	return p
}

// maAt 回傳「截至 index i 的 window 日均價」。資料不足 (i < window-1) 回傳 NaN。
// 需要 prefixClose 已建立;否則退回 NaN (呼叫端在預設路徑不會走到這裡)。
func (s *stockSeries) maAt(i, window int) float64 {
	if window <= 0 {
		window = 20
	}
	if len(s.prefixClose) != len(s.closePrices)+1 {
		return math.NaN()
	}
	if i < window-1 || i >= len(s.closePrices) {
		return math.NaN()
	}
	return (s.prefixClose[i+1] - s.prefixClose[i+1-window]) / float64(window)
}

// peakAt 回傳「近 lookback 日 (含當日) 的最高收盤」;供 peak 深度基準 (距高點回撤)。整條陣列首次計算後快取。
func (s *stockSeries) peakAt(i, lookback int) float64 {
	if lookback <= 0 {
		lookback = 252
	}
	if i < 0 || i >= len(s.closePrices) {
		return math.NaN()
	}
	if s.peakCache == nil {
		s.peakCache = make(map[int][]float64)
	}
	arr, ok := s.peakCache[lookback]
	if !ok {
		arr = rollingMax(s.closePrices, lookback)
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

// rsiAt 回傳 Wilder RSI(period) 在 index i 的值;不足資料回傳 NaN。整條陣列首次計算後快取。
func (s *stockSeries) rsiAt(i, period int) float64 {
	if period <= 0 {
		return math.NaN()
	}
	if i < 0 || i >= len(s.closePrices) {
		return math.NaN()
	}
	if s.rsiCache == nil {
		s.rsiCache = make(map[int][]float64)
	}
	arr, ok := s.rsiCache[period]
	if !ok {
		arr = wilderRSI(s.closePrices, period)
		s.rsiCache[period] = arr
	}
	return arr[i]
}

// wilderRSI 以 Wilder 平滑法計算整條 RSI 陣列 (rsi[i] 對應 close[i])。
// rsi[i] 在 i < period 時為 NaN (暖身不足)。avgLoss==0 時 RSI=100。
func wilderRSI(close []float64, period int) []float64 {
	n := len(close)
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	if n <= period || period <= 0 {
		return out
	}

	var gainSum, lossSum float64
	for i := 1; i <= period; i++ {
		ch := close[i] - close[i-1]
		if ch >= 0 {
			gainSum += ch
		} else {
			lossSum += -ch
		}
	}
	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	out[period] = rsiFrom(avgGain, avgLoss)

	for i := period + 1; i < n; i++ {
		ch := close[i] - close[i-1]
		gain, loss := 0.0, 0.0
		if ch >= 0 {
			gain = ch
		} else {
			loss = -ch
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		out[i] = rsiFrom(avgGain, avgLoss)
	}
	return out
}

func rsiFrom(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50 // 完全無波動,中性
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}
