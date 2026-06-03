package kernals

import (
	"main/config"
	"math"
	"time"
)

// benchmarks.go 提供回測評估的對照組。三者都與策略共用同一份 series、同一組 windowDates、
// 同一筆起始現金池 (apples-to-apples),且刻意「不」走 DecideBuy / DecideSell —— benchmark 的
// 目的就是隔離出「策略那套 MA20 / 加碼級距 / 冷卻邏輯」到底有沒有加值。
//
// 對照組:
//   - lumpSumBenchmark        : 期初一次買滿、抱到底 (高報酬高風險的參考)。
//   - naiveDCABenchmark       : 固定金額、固定頻率、永不賣 (中性 DCA;「我的聰明邏輯有用嗎」的控制組)。
//   - exposureMatchedBlend    : 與策略「實際平均持股佔比 w」相同的固定權重現金+B&H 混合。
//                               這是防作弊的關鍵 —— 低持股策略光靠抱現金就能贏 Calmar,
//                               策略必須連這個「同曝險笨蛋」都在 Calmar 與 CAGR 雙雙勝出,才算真有擇時能力。

// benchResult 為單一對照組在某視窗的輸出。
type benchResult struct {
	curve       []float64  // 與 windowDates 等長的每日總權益 (現金 + 持股市值)
	deployed    []Cashflow // 已投入資金的對外現金流 (買入為負、期末清算為正),供 deployed-capital XIRR
	avgExposure float64    // 平均持股佔比 mean(holdings/total),僅計 total>0 的日子
}

// tradableAt 回傳在 day 當天「已上市可交易」的追蹤股票 (closeAsOf ok)。
// B&H 等權只能分配給 window 起始日已存在的股票,不得替「之後才上市」的股票預留現金 (那是未來資訊洩漏)。
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

// lumpSumBenchmark：期初 (windowDates[0]) 把現金池等權買滿 tradable 股票 (整股、餘額留現金),抱到底。
func lumpSumBenchmark(series map[string]*stockSeries, windowDates []time.Time, tradable []string, initialCash float64) benchResult {
	start := windowDates[0]
	positions := make(map[string]int, len(tradable))
	spent := 0.0
	if len(tradable) > 0 {
		perStock := initialCash / float64(len(tradable))
		for _, id := range tradable {
			px, ok := series[id].closeAsOf(start)
			if !ok || px <= 0 {
				continue
			}
			sh := int(math.Floor(perStock / px))
			if sh <= 0 {
				continue
			}
			positions[id] = sh
			spent += float64(sh) * px
		}
	}
	leftover := initialCash - spent // 整股餘額留現金,屬於資金池權益的一部分

	curve := make([]float64, len(windowDates))
	expSum, expN := 0.0, 0
	for i, d := range windowDates {
		holdings := holdingValue(series, positions, d)
		total := holdings + leftover
		curve[i] = total
		if total > 0 {
			expSum += holdings / total
			expN++
		}
	}
	avgExp := safeMean(expSum, expN)

	var deployed []Cashflow
	if spent > 0 {
		end := windowDates[len(windowDates)-1]
		finalHoldings := holdingValue(series, positions, end)
		deployed = []Cashflow{{Date: start, Amount: -spent}, {Date: end, Amount: finalHoldings}}
	}
	return benchResult{curve: curve, deployed: deployed, avgExposure: avgExp}
}

// naiveDCABenchmark：每 everyK 個交易日 (含第 0 日) 投入 amountPerBuy,等權分散、整股、永不賣。
// clamp 到剩餘現金,故總投入不超過資金池。用來回答「拿掉擇時邏輯、純定期定額會怎樣」。
func naiveDCABenchmark(cfg *config.Config, series map[string]*stockSeries, windowDates []time.Time, initialCash float64, everyK int, amountPerBuy float64) benchResult {
	if everyK < 1 {
		everyK = 1
	}
	positions := make(map[string]int)
	cash := initialCash
	curve := make([]float64, len(windowDates))
	var deployed []Cashflow
	expSum, expN := 0.0, 0

	for i, d := range windowDates {
		if i%everyK == 0 && cash > 0 {
			trad := tradableAt(cfg, series, d)
			if len(trad) > 0 {
				per := amountPerBuy / float64(len(trad))
				for _, id := range trad {
					px, ok := series[id].closeAsOf(d)
					if !ok || px <= 0 {
						continue
					}
					afford := math.Min(per, cash)
					sh := int(math.Floor(afford / px))
					if sh <= 0 {
						continue
					}
					cost := float64(sh) * px
					cash -= cost
					positions[id] += sh
					deployed = append(deployed, Cashflow{Date: d, Amount: -cost})
				}
			}
		}
		holdings := holdingValue(series, positions, d)
		total := holdings + cash
		curve[i] = total
		if total > 0 {
			expSum += holdings / total
			expN++
		}
	}
	if len(deployed) > 0 {
		end := windowDates[len(windowDates)-1]
		if fh := holdingValue(series, positions, end); fh > 0 {
			deployed = append(deployed, Cashflow{Date: end, Amount: fh})
		}
	}
	return benchResult{curve: curve, deployed: deployed, avgExposure: safeMean(expSum, expN)}
}

// exposureMatchedBlend：固定權重 w 的「B&H 投組 + 0% 現金」每日再平衡混合。
// 由建構方式保證每日曝險恰為 w,故 avgExposure == w。報酬 = initialCash * Π(1 + w*bhDailyReturn)。
// 這是把「可被抱現金灌水的 Calmar」轉成「真擇時測試」的對照基準。
func exposureMatchedBlend(bhCurve []float64, w, initialCash float64) []float64 {
	curve := make([]float64, len(bhCurve))
	if len(bhCurve) == 0 {
		return curve
	}
	curve[0] = initialCash
	for i := 1; i < len(bhCurve); i++ {
		bhRet := 0.0
		if bhCurve[i-1] > 0 {
			bhRet = bhCurve[i]/bhCurve[i-1] - 1
		}
		curve[i] = curve[i-1] * (1 + w*bhRet)
	}
	return curve
}

func safeMean(sum float64, n int) float64 {
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
