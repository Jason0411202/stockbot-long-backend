// internal/service/backtest/metrics.go 提供報酬、回撤、XIRR 與統計指標計算。
package backtest

import (
	"math"
	"time"
)

// metrics.go 收錄回測評估用的純函式績效指標。
//
// 設計原則 (來自方法論審查):
//   - 報酬同時保留「未年化區間報酬」與「年化值」;年化只在視窗 >= 1 年時才有意義
//     (年化會把短視窗的雜訊放大,例如 +10%/30天 年化成 +218%)。
//   - XIRR 用「掃描變號 + 二分法」而非 Newton-Raphson:對本策略會出現的現金流形狀
//     (買多次、極少賣、可能全損) 更穩健,絕不會發散到 r <= -1 而產生 NaN/Inf。
//   - Sortino 用 MAR=0、全體期數 N 為分母 (target semideviation)、日頻 * sqrt(252) 年化。
//   - Calmar 對 MaxDD==0 採用 piecewise 定義 (+Inf / NaN sentinel),保留 CAGR 正負號,
//     呼叫端 (scorecard) 需把 Inf/NaN 視窗排除於勝率分母外。
//
// 所有指標皆為純函式,不讀 DB / 不依賴 Engine 狀態,方便用已驗證的數值向量做表格測試。

// EquityPoint 為某一日期的帳戶總權益 (現金 + 持股市值) 快照。
type EquityPoint struct {
	Date   time.Time
	Equity float64
}

// Cashflow 為一筆對外現金流。慣例:買入/投入為負,賣出/期末清算為正。
type Cashflow struct {
	Date   time.Time
	Amount float64
}

// yearsBetween 以 Actual/365 計算兩日期間的年數 (calendar days,非交易日)。
func yearsBetween(start, end time.Time) float64 {
	return end.Sub(start).Hours() / 24.0 / 365.0
}

// periodReturn 為未年化的區間報酬 (end/start - 1)。start <= 0 回傳 NaN。
func periodReturn(start, end float64) float64 {
	if start <= 0 {
		return math.NaN()
	}
	return end/start - 1
}

// cagr 為年化複合成長率 (end/start)^(1/years) - 1。
// years <= 0 或 start <= 0 回傳 NaN;end <= 0 (全損) 回傳 -1。
// 注意:years < 1 時年化會非線性放大報酬與虧損,呼叫端需自行判斷是否採用 (見 walkforward 的 minYears 守門)。
func cagr(start, end, years float64) float64 {
	if start <= 0 || years <= 0 {
		return math.NaN()
	}
	if end <= 0 {
		return -1
	}
	return math.Pow(end/start, 1.0/years) - 1
}

// navCurveFromEquity 由「每日總權益」與「每日外部注資額」還原 NAV / 單位淨值序列 (contribution-neutral)。
//
// 有定期注資時,原始權益曲線會被注資「灌高」,直接量 maxDrawdown 會低估真實投資回撤。
// 解法 = 共同基金式單位記帳:注資視為「以當日(注資前)淨值買進新單位」,不改變每單位淨值 →
// NAV 序列只反映投資績效、與注資時程無關,其回撤才是真實的投資回撤。
//
//	nav[0] = equity[0] / units0          (units0 = initial,故注資前 nav[0]≈1)
//	每日:注資 c_i 以 nav[i-1] 換成新單位 units += c_i/nav[i-1];nav[i] = equity[i]/units
//
// 當所有注資為 0 時,units 恆為 initial、nav[i]=equity[i]/initial,maxDrawdown(nav)==maxDrawdown(equity)
// (回撤對等比例縮放不變),故行為與「無注資」舊版完全一致。
// contribOnDay[i] = 第 i 天「在當日交易前」注入的金額 (無注資為 0);長度需與 equity 對齊。
func navCurveFromEquity(equity, contribOnDay []float64, initial float64) []float64 {
	n := len(equity)
	nav := make([]float64, n)
	if n == 0 || initial <= 0 {
		return nav
	}
	units := initial
	nav[0] = equity[0] / units
	for i := 1; i < n; i++ {
		if i < len(contribOnDay) && contribOnDay[i] > 0 && nav[i-1] > 0 {
			units += contribOnDay[i] / nav[i-1]
		}
		if units > 0 {
			nav[i] = equity[i] / units
		}
	}
	return nav
}

// maxDrawdown 回傳權益曲線的最大回撤,以 <= 0 的比例表示 (例如 -0.25 = 自高點下跌 25%)。
// 單調上升或空曲線回傳 0。
func maxDrawdown(curve []float64) float64 {
	if len(curve) == 0 {
		return 0
	}
	peak := curve[0]
	worst := 0.0
	for _, v := range curve {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			dd := v/peak - 1
			if dd < worst {
				worst = dd
			}
		}
	}
	return worst
}

// calmar = cagr / |maxDD|,保留 cagr 正負號。
// maxDD == 0 (無回撤):cagr > 0 -> +Inf;cagr <= 0 -> NaN (無意義)。
// 呼叫端需把 Inf/NaN 視窗排除於 Calmar 勝率分母外,避免 Inf > finite 自動過關。
func calmar(cagrVal, maxDD float64) float64 {
	absDD := math.Abs(maxDD)
	if absDD < 1e-12 {
		if cagrVal > 0 {
			return math.Inf(1)
		}
		return math.NaN()
	}
	return cagrVal / absDD
}

// dailyReturns 由權益曲線算逐日簡單報酬,長度 = len(curve) - 1。
func dailyReturns(curve []float64) []float64 {
	if len(curve) < 2 {
		return nil
	}
	out := make([]float64, 0, len(curve)-1)
	for i := 1; i < len(curve); i++ {
		if curve[i-1] > 0 {
			out = append(out, curve[i]/curve[i-1]-1)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

// downsideDeviation 為目標半變異數的平方根 (target semideviation)。
// 分母用「全體期數 N」(非僅下行期數),只有低於 mar 的報酬計入平方和 —— 這是 Sortino 慣例,
// 也是與下行期數分母 (k) 差異最大的來源 (同一組樣本可差約 50%)。
func downsideDeviation(rets []float64, mar float64) float64 {
	if len(rets) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range rets {
		if d := r - mar; d < 0 {
			sum += d * d
		}
	}
	return math.Sqrt(sum / float64(len(rets)))
}

// sortino 為年化 Sortino 比率。rets 為逐日報酬,mar 為逐期最低可接受報酬 (本專案固定 0),
// periodsPerYear 通常為 252。年化:分子 (mean-mar)*periodsPerYear;分母 downsideDev*sqrt(periodsPerYear)。
// 下行偏差為 0:正超額 -> +Inf,否則 NaN。
func sortino(rets []float64, mar, periodsPerYear float64) float64 {
	if len(rets) == 0 {
		return math.NaN()
	}
	mean := 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	dd := downsideDeviation(rets, mar)
	if dd < 1e-15 {
		if mean-mar > 0 {
			return math.Inf(1)
		}
		return math.NaN()
	}
	return (mean - mar) * periodsPerYear / (dd * math.Sqrt(periodsPerYear))
}

// npv 為現金流在年利率 rate 下的淨現值;ref 為折現基準日 (通常最早一筆現金流)。
func npv(rate float64, flows []Cashflow, ref time.Time) float64 {
	sum := 0.0
	for _, f := range flows {
		years := f.Date.Sub(ref).Hours() / 24.0 / 365.0
		sum += f.Amount / math.Pow(1+rate, years)
	}
	return sum
}

// xirr 以資金加權方式解年化內部報酬率 (money-weighted return)。
//
// 解法:在 (-0.9999, 100] 上掃描 NPV 的「全部」變號區間。
//   - 恰好 1 個變號區間 -> 二分法收斂回傳唯一根 (true)。
//   - 0 個變號 (例如全損且無賣出) -> (NaN, false)。
//   - >= 2 個變號 -> 多重 IRR,資金加權報酬「不唯一」,回傳 (NaN, false) 視為 N/A。
//
// 刻意不用 Newton-Raphson:本策略可能「賣出後在冷卻期過後再買進」,造成現金流多次變號 ->
// NPV 有多個實根;Newton (或只取最小根) 可能挑到貼著 -0.9999 邊界的人為根 (例如把淨賺的
// 現金流誤報成 -99%)。對多根視窗直接標 N/A 並排除,是「不自欺」最誠實的處理 (deployed-XIRR
// 僅供參考、不進任何 gate,排除少數不唯一視窗不影響結論)。
//
// 前置條件:至少一筆正、一筆負現金流,否則 IRR 無定義,回傳 (NaN, false)。
func xirr(flows []Cashflow) (float64, bool) {
	if len(flows) < 2 {
		return math.NaN(), false
	}
	// 確認至少有一筆正向與一筆負向現金流,並找出最早日期作為折現基準。
	var hasPos, hasNeg bool
	ref := flows[0].Date
	for _, f := range flows {
		if f.Amount > 0 {
			hasPos = true
		}
		if f.Amount < 0 {
			hasNeg = true
		}
		if f.Date.Before(ref) {
			ref = f.Date
		}
	}
	if !hasPos || !hasNeg {
		return math.NaN(), false
	}

	// 以固定步距在 (-0.9999, 100] 掃描 NPV 的全部變號區間,偵測根的數量。
	const lo = -0.9999
	const hi = 100.0
	const steps = 4000
	prevR := lo
	prevV := npv(prevR, flows, ref)
	signChanges := 0
	var bLo, bHi float64
	for i := 1; i <= steps; i++ {
		r := lo + (hi-lo)*float64(i)/float64(steps)
		v := npv(r, flows, ref)
		if (prevV < 0) != (v < 0) {
			signChanges++
			if signChanges == 1 {
				bLo, bHi = prevR, r
			} else {
				// 多重根:資金加權報酬不唯一,不報單一值。
				return math.NaN(), false
			}
		}
		prevR, prevV = r, v
	}
	// 恰好一個變號區間時,以二分法收斂回傳唯一根。
	if signChanges == 1 {
		return bisectRate(bLo, bHi, flows, ref), true
	}
	return math.NaN(), false
}

// bisectRate 在已知變號的 [lo, hi] 上對 NPV 做二分法,回傳根 (年化報酬率)。
func bisectRate(lo, hi float64, flows []Cashflow, ref time.Time) float64 {
	// 逐步以中點取代同側端點,收斂到精度 1e-12 或最多 200 次迭代後回傳近似根。
	flo := npv(lo, flows, ref)
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		fmid := npv(mid, flows, ref)
		if fmid == 0 || (hi-lo) < 1e-12 {
			return mid
		}
		if (flo < 0) != (fmid < 0) {
			hi = mid
		} else {
			lo = mid
			flo = fmid
		}
	}
	return (lo + hi) / 2
}

// median 回傳切片的中位數 (會排序輸入的副本);空切片回傳 NaN。
func median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return math.NaN()
	}
	cp := make([]float64, n)
	copy(cp, xs)
	sortFloats(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// stdev 為樣本標準差 (n-1 分母);少於 2 筆回傳 0。
func stdev(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(n)
	ss := 0.0
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(n-1))
}

// sortFloats 為簡單的就地升冪排序 (避免為了 median/stdev 引入 sort 對 []float64 的額外包裝)。
func sortFloats(xs []float64) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
