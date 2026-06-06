package kernals

import (
	"main/config"
	"math"
	"time"
)

// benchmarks.go 提供「每月定期定額注資」問題設定下的對照組。所有對照組與策略共用同一份 series、
// 同一組 windowDates、同一筆期初資金、同一份注資時程 (apples-to-apples),且刻意「不」走
// DecideBuy / DecideSell —— benchmark 的目的就是隔離出策略那套 MA / 加碼級距 / 冷卻 / 賣出邏輯
// 到底有沒有加值。
//
// 對照組:
//   - B&H (bhImmediateArm)   : 資金一解鎖就立刻等權買滿、抱到底 (永遠盡量滿倉的對照,高報酬高風險)。
//   - Blend (blendMetrics)   : 與策略「實際平均持股佔比 w」相同的固定權重「市場 + 現金」混合,
//                              收同一份注資。這是防作弊的關鍵 —— 低持股策略光靠抱現金就能贏 Calmar,
//                              策略必須連這個「同曝險笨蛋」都在 (資金加權報酬 + Calmar) 雙雙勝出,才算真有擇時能力。
//
// 因為有持續外部注資,報酬一律用資金加權 (XIRR/MWR;外部現金流 = 期初+每月注入皆為負、期末清算為正,
// 恰一次變號故必唯一可解),回撤一律用 NAV 單位淨值 (navCurveFromEquity,扣除注資灌水的真實投資回撤)。

// armResult 為單一做法 (策略 / B&H) 在某視窗下的模擬輸出。
type armResult struct {
	curve        []float64  // 每日總權益 (現金 + 持股市值,含已注入資金)
	contribOnDay []float64  // 每日注資額 (與 curve / windowDates 對齊;無注資為 0)
	flows        []Cashflow // 外部現金流:期初 -initial、每月 -monthly、期末 +finalEquity (供資金加權 XIRR)
	avgExposure  float64    // 平均持股佔比 mean(holdings/equity),僅計 equity>0 的日子
	finalEquity  float64    // 期末總權益
	totalIn      float64    // 投入本金總額 = 期初 + Σ注資
	finalCash    float64    // 期末閒置現金 (「現金尾巴」;策略才有意義)
	buys         int
	sells        int
	skipped      int
	trailSells   int // 移動停利觸發的賣出次數 (策略才有意義)
	profitSells  int // 獲利了結觸發的賣出次數 (策略才有意義)
}

// tradableAt 回傳在 day 當天「已上市可交易」的追蹤股票 (closeAsOf ok)。
// B&H 等權只能分配給當天已存在的股票,不得替「之後才上市」的股票預留現金 (那是未來資訊洩漏)。
func tradableAt(cfg *config.Config, series map[string]*stockSeries, day time.Time) []string {
	out := make([]string, 0, len(cfg.TrackStocks))
	for _, id := range cfg.TrackStocks {
		s, ok := series[id]
		if !ok {
			continue
		}
		if _, ok := s.closeAsOf(day); ok {
			out = append(out, id)
		}
	}
	return out
}

// holdingValue 以 as-of 收盤價結算固定持股在 day 的市值。
func holdingValue(series map[string]*stockSeries, positions map[string]int, day time.Time) float64 {
	total := 0.0
	for id, sh := range positions {
		if sh == 0 {
			continue
		}
		if px, ok := series[id].closeAsOf(day); ok {
			total += float64(sh) * px
		}
	}
	return total
}

// bhImmediateArm:期初把期初資金等權買滿,其後每個注資日把當前所有現金 (新注入 + 整股餘額)
// 立刻等權買滿,持有到底、永不賣 —— 即「資金一解鎖就立刻買」的 Buy & Hold。
func bhImmediateArm(cfg *config.Config, series map[string]*stockSeries, windowDates []time.Time, contribOnDay []float64) armResult {
	positions := make(map[string]int, len(cfg.TrackStocks))
	cash := cfg.InitialCash
	totalIn := cfg.InitialCash
	flows := []Cashflow{{Date: windowDates[0], Amount: -cfg.InitialCash}}

	buys := 0
	if deployAllCash(cfg, series, windowDates[0], positions, &cash) {
		buys++
	}

	curve := make([]float64, len(windowDates))
	expSum, expN := 0.0, 0
	for i, d := range windowDates {
		if contribOnDay[i] > 0 {
			cash += contribOnDay[i]
			totalIn += contribOnDay[i]
			flows = append(flows, Cashflow{Date: d, Amount: -contribOnDay[i]})
			if deployAllCash(cfg, series, d, positions, &cash) {
				buys++
			}
		}
		holdings := holdingValue(series, positions, d)
		equity := holdings + cash
		curve[i] = equity
		if equity > 0 {
			expSum += holdings / equity
			expN++
		}
	}

	end := windowDates[len(windowDates)-1]
	finalEq := holdingValue(series, positions, end) + cash
	flows = append(flows, Cashflow{Date: end, Amount: finalEq})
	return armResult{
		curve: curve, contribOnDay: contribOnDay, flows: flows,
		avgExposure: safeMean(expSum, expN), finalEquity: finalEq, totalIn: totalIn, buys: buys,
	}
}

// deployAllCash 把 *cash 等權買滿 day 當天可交易的追蹤股票 (整股,餘額留 *cash);永不借錢。
// 回傳是否至少買進 1 股。
func deployAllCash(cfg *config.Config, series map[string]*stockSeries, day time.Time, positions map[string]int, cash *float64) bool {
	trad := tradableAt(cfg, series, day)
	if len(trad) == 0 || *cash <= 0 {
		return false
	}
	per := *cash / float64(len(trad))
	bought := false
	for _, id := range trad {
		px, ok := series[id].closeAsOf(day)
		if !ok || px <= 0 {
			continue
		}
		sh := int(math.Floor(per / px))
		if sh <= 0 {
			continue
		}
		*cash -= float64(sh) * px
		positions[id] += sh
		bought = true
	}
	return bought
}

// blendMetrics 由 B&H 的 NAV (市場純投資淨值) 與權重 w (= 策略實際平均曝險) 建構「同曝險混合」並算指標。
// 混合 = 每日 w 在市場、(1-w) 在現金的定權重再平衡組合,收同一份注資。其曝險恆為 w,
// 用來把「可被抱現金灌水的 Calmar」轉成真擇時測試。
func blendMetrics(bhNav []float64, w float64, contribOnDay []float64, initial float64, dates []time.Time) SeriesMetrics {
	n := len(bhNav)
	if n == 0 {
		return SeriesMetrics{AvgExp: w}
	}
	blendNav := make([]float64, n)
	blendNav[0] = 1
	for i := 1; i < n; i++ {
		r := 0.0
		if bhNav[i-1] > 0 {
			r = bhNav[i]/bhNav[i-1] - 1
		}
		blendNav[i] = blendNav[i-1] * (1 + w*r)
	}
	flows, finalEq, totalIn := flowsFromNav(blendNav, contribOnDay, initial, dates)
	mwr, ok := xirr(flows)
	mdd := maxDrawdown(blendNav)
	cal := math.NaN()
	if ok {
		cal = calmar(mwr, mdd)
	}
	return SeriesMetrics{
		MWR: mwr, MWROK: ok, MaxDD: mdd, Calmar: cal,
		Sortino: sortino(dailyReturns(blendNav), 0, 252),
		AvgExp:  w, Multiple: safeDiv(finalEq, totalIn),
	}
}

// flowsFromNav 把「期初 initial + 注資時程 contribOnDay」投入一條 NAV 序列 (注資以前一日 NAV 換單位),
// 回傳外部現金流 (期初/每月為負、期末清算為正)、期末權益、投入本金總額。供合成對照組 (Blend) 算資金加權報酬。
func flowsFromNav(nav, contribOnDay []float64, initial float64, dates []time.Time) (flows []Cashflow, finalEquity, totalIn float64) {
	units := initial
	totalIn = initial
	flows = []Cashflow{{Date: dates[0], Amount: -initial}}
	for i := 1; i < len(nav); i++ {
		if contribOnDay[i] > 0 {
			if nav[i-1] > 0 {
				units += contribOnDay[i] / nav[i-1]
			}
			totalIn += contribOnDay[i]
			flows = append(flows, Cashflow{Date: dates[i], Amount: -contribOnDay[i]})
		}
	}
	finalEquity = units * nav[len(nav)-1]
	flows = append(flows, Cashflow{Date: dates[len(dates)-1], Amount: finalEquity})
	return flows, finalEquity, totalIn
}

func safeMean(sum float64, n int) float64 {
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
