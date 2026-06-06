package kernals

// splits.go 處理股票分割 (split):TWSE STOCK_DAY 提供「未還原」原始價,分割會在價格序列造成大幅隔日跳空
// (例如 00631L 於 2026-03 約 1:23 分割,443 → 19)。若不還原,200MA、+100% 獲利了結、8% 移動停利、
// regime 判定都會被這個假跳空汙染。此處在載入時自動偵測極端隔日比例並 back-adjust,使序列連續 (以最新價為基準)。
//
// 偵測門檻 0.5 / 2.0:單日漲跌 ±50% 以上對大盤型 / 2x 槓桿 ETF 實務上不會發生 (台股單日漲跌幅受限),
// 故能乾淨區分「分割」與「真實行情 / 除息 (僅 1–3% 小跳空)」—— 除息不會被誤判為分割。

const (
	splitDownRatio = 0.5 // 隔日收盤 < 前日 ×0.5 → 視為正向分割 (股數變多、價格變小)
	splitUpRatio   = 2.0 // 隔日收盤 > 前日 ×2.0 → 視為反向分割 (股數變少、價格變大)
)

// splitAdjustFactors 回傳與 closes (升冪) 等長的乘數:adjusted[i] = closes[i] × factor[i]。
// 由最新一筆 (factor=1) 往回累乘各次分割比例,使分割前的價格縮放到分割後尺度 → 序列連續。
func splitAdjustFactors(closes []float64) []float64 {
	n := len(closes)
	factors := make([]float64, n)
	if n == 0 {
		return factors
	}
	cum := 1.0
	factors[n-1] = 1.0
	for i := n - 1; i >= 1; i-- {
		if closes[i] > 0 && closes[i-1] > 0 {
			if r := closes[i] / closes[i-1]; r < splitDownRatio || r > splitUpRatio {
				cum *= r
			}
		}
		factors[i-1] = cum
	}
	return factors
}

// applySplitAdjust 就地把 closes (必填) 與其他同長度價格序列 (highs/lows…) 依分割因子 back-adjust。
// 須在「日期升冪」且「計算 MA / 前綴和之前」呼叫。
func applySplitAdjust(closes []float64, others ...[]float64) {
	f := splitAdjustFactors(closes)
	for i := range closes {
		closes[i] *= f[i]
	}
	for _, s := range others {
		if len(s) == len(f) {
			for i := range s {
				s[i] *= f[i]
			}
		}
	}
}
