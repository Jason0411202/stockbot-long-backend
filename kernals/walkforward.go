package kernals

import (
	"fmt"
	"main/app_context"
	"main/config"
	"math"
	"sort"
	"time"
)

// walkforward.go 把「單一長區間回測」升級成「多視窗 walk-forward 評估」,並對每個視窗
// 同時跑策略與三個對照組,最後用一張 scorecard 回答核心問題:
//
//   「在所有進場時點下,策略能否守住 B&H 七成以上的報酬、用顯著更小的最大回撤,
//     且 Calmar 穩定贏 B&H —— 而且這份優勢是來自擇時,不是單純抱現金?」
//
// 重要的方法論防呆 (來自審查):
//   - 只在「2 檔共同有效資料期 (common-support)」內產生視窗,且視窗起點皆已過 MA20 暖身,
//     確保每個視窗都是同universe、無暖身空轉、無未來資訊。
//   - 用固定 24 個月視窗,讓所有視窗在同一基準上年化 (避免短視窗年化爆炸)。
//   - Calmar 會被「抱現金」灌水,故除了比 B&H,還必須贏「同曝險混合 (exposureMatchedBlend)」,
//     才算真擇時。avg_exposure 與 deployed-capital XIRR 一律同表呈現,杜絕誤讀。

// WalkForwardParams 為評估參數;0 值會套用合理預設。
type WalkForwardParams struct {
	WindowMonths int     // 每個視窗長度 (日曆月),預設 24
	StepMonths   int     // 視窗起點間隔,預設 3
	DCAEveryDays int     // naive DCA 投入頻率 (交易日),預設 21 (約一個月)
	DCAAmount    float64 // naive DCA 每次投入金額;<=0 表示「自動:把整池在視窗內大致投滿」
	MinTradeDays int     // 視窗最少交易日,低於此略過,預設 200
}

// SeriesMetrics 為某一條權益曲線在某視窗的績效指標。
type SeriesMetrics struct {
	PeriodRet float64 // 未年化區間報酬
	CAGR      float64 // 年化 (視窗 >= 1 年才有意義)
	MaxDD     float64 // 最大回撤,<= 0
	Calmar    float64 // CAGR / |MaxDD|,可能為 +Inf/NaN
	Sortino   float64 // 年化 Sortino
	AvgExp    float64 // 平均持股佔比
}

// WindowReport 為單一視窗的完整結果。
type WindowReport struct {
	Start, End time.Time
	Universe   int     // 視窗起點可交易的追蹤股票數
	TradeDays  int     // 視窗內交易日數
	Years      float64 // Actual/365 年數

	Strat SeriesMetrics
	BH    SeriesMetrics // lump-sum Buy & Hold
	Blend SeriesMetrics // 同曝險混合 (constant weight = Strat.AvgExp)
	DCA   SeriesMetrics // naive 定期定額

	StratXIRR   float64 // deployed-capital 資金加權年化報酬;未定義時為 NaN
	StratXIRROK bool
	BHXIRR      float64
	BHXIRROK    bool

	Buys, Sells, Skipped int

	// per-window 判定
	CalmarBeatsBH    bool    // Strat.Calmar > BH.Calmar (排除 Inf/NaN 不可比)
	BeatsBlendBoth   bool    // Strat 在 Calmar 與 CAGR 雙雙贏 Blend (真擇時)
	RetParticipation float64 // Strat.CAGR / BH.CAGR
}

// 五道關卡的門檻 (可依風險偏好調整;集中於此方便校準)。
const (
	gateRetParticipation = 0.75 // G1:中位 Strat CAGR >= 75% 中位 BH CAGR
	gateRiskReduction    = 0.60 // G2:中位 |Strat MDD| <= 60% 中位 |BH MDD|
	gateCalmarWinRate    = 0.70 // G3:>=70% 視窗 Strat Calmar 贏 BH
	gateSkillRate        = 0.50 // G4:>=50% 視窗 Strat 在 Calmar+CAGR 雙贏 Blend
)

// AggregateReport 為跨所有視窗的彙整與 scorecard。
type AggregateReport struct {
	NWindows int

	MedStratCAGR, MedBHCAGR     float64
	MedStratMDD, MedBHMDD       float64 // 中位最大回撤 (<=0)
	MedStratCalmar, MedBHCalmar float64 // 只取有限值的中位
	MedStratAvgExp              float64
	MedStratXIRR                float64 // deployed-capital,只取可唯一求解的視窗中位
	MedBHXIRR                   float64 // B&H deployed-capital,供 apples-to-apples 對照
	NStratXIRRSolvable          int     // 策略 XIRR 可唯一求解的視窗數 (其餘為多重根/無解,已排除)
	MedRetParticipation         float64

	CalmarWinRate  float64 // 可比視窗中 Strat Calmar 贏 BH 的比例
	BlendSkillRate float64 // Strat 在 Calmar+CAGR 雙贏 Blend 的視窗比例

	DispersionStratCAGR float64 // Strat CAGR 樣本標準差 (越小越穩定)
	WorstStratCAGR      float64
	WorstStratMDD       float64 // 最差 (最負) Strat 回撤
	WorstBHMDD          float64

	G1RetParticipation bool
	G2RiskReduction    bool
	G3CalmarVsBH       bool
	G4Skill            bool
	G5Robustness       bool // 最差視窗 Strat 回撤幅度 <= 最差視窗 BH 回撤幅度
	OverallPass        bool // G1 && G2 && G3 && G4 (核心主張 + 真擇時)
}

// RunWalkForward 載入 DB series 後執行 walk-forward 評估 (appCtx 版,供 cmd 呼叫)。
func RunWalkForward(appCtx *app_context.AppContext, p WalkForwardParams) ([]WindowReport, AggregateReport, error) {
	series, err := loadStockSeries(appCtx)
	if err != nil {
		return nil, AggregateReport{}, err
	}
	if len(series) == 0 {
		return nil, AggregateReport{}, fmt.Errorf("無任何股票歷史資料可供回測")
	}
	return walkForwardOnSeries(appCtx.Cfg, series, p)
}

// walkForwardOnSeries 為不依賴 DB 的核心,方便單元測試。
func walkForwardOnSeries(cfg *config.Config, series map[string]*stockSeries, p WalkForwardParams) ([]WindowReport, AggregateReport, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return nil, AggregateReport{}, fmt.Errorf("評估目前僅支援 Scaling_Strategy=Baseline")
	}
	if p.WindowMonths <= 0 {
		p.WindowMonths = 24
	}
	if p.StepMonths <= 0 {
		p.StepMonths = 3
	}
	if p.DCAEveryDays <= 0 {
		p.DCAEveryDays = 21
	}
	if p.MinTradeDays <= 0 {
		p.MinTradeDays = 200
	}

	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		return nil, AggregateReport{}, fmt.Errorf("無任何日期可供評估")
	}
	windows := generateWindows(cfg, series, allDates, p)
	if len(windows) == 0 {
		return nil, AggregateReport{}, fmt.Errorf(
			"共同有效資料期不足以產生任何 %d 個月視窗 (step=%d, minTradeDays=%d)",
			p.WindowMonths, p.StepMonths, p.MinTradeDays)
	}

	reports := make([]WindowReport, 0, len(windows))
	for _, w := range windows {
		rep, err := evaluateWindow(cfg, series, allDates, w[0], w[1], p)
		if err != nil {
			return nil, AggregateReport{}, fmt.Errorf("視窗 %s: %w", w[0].Format("2006-01-02"), err)
		}
		reports = append(reports, rep)
	}
	return reports, aggregate(reports), nil
}

// commonSupportStart 回傳「所有追蹤股票皆已具備有效 MA20」的最早日期。
// = 各追蹤股票第 20 個交易日 (dates[19]) 的最大值。確保每個視窗起點都無 MA20 暖身空轉,
// 也讓所有 headline 視窗都是完整 universe。資料不足 20 日的股票會被排除。
func commonSupportStart(cfg *config.Config, series map[string]*stockSeries) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, id := range cfg.TrackStocks {
		s, ok := series[id]
		if !ok || len(s.dates) < 20 {
			continue
		}
		d := s.dates[19]
		if !found || d.After(latest) {
			latest = d
			found = true
		}
	}
	return latest, found
}

// generateWindows 以日曆月為步進,在 [commonSupportStart, 最後資料日] 內產生完整 windowMonths 視窗。
// 回傳每個視窗的 [start, end] (皆對齊到實際交易日)。
func generateWindows(cfg *config.Config, series map[string]*stockSeries, allDates []time.Time, p WalkForwardParams) [][2]time.Time {
	csStart, ok := commonSupportStart(cfg, series)
	if !ok {
		return nil
	}
	lastDate := allDates[len(allDates)-1]
	var windows [][2]time.Time
	for anchor := csStart; !anchor.AddDate(0, p.WindowMonths, 0).After(lastDate); anchor = anchor.AddDate(0, p.StepMonths, 0) {
		winEndCal := anchor.AddDate(0, p.WindowMonths, 0)
		lo := sort.Search(len(allDates), func(i int) bool { return !allDates[i].Before(anchor) })
		hi := sort.Search(len(allDates), func(i int) bool { return allDates[i].After(winEndCal) })
		if lo >= hi || hi-lo < p.MinTradeDays {
			continue
		}
		windows = append(windows, [2]time.Time{allDates[lo], allDates[hi-1]})
	}
	return windows
}

// evaluateWindow 對單一 [start, end] 視窗同時跑策略與三對照組,算出所有指標。
func evaluateWindow(cfg *config.Config, series map[string]*stockSeries, allDates []time.Time, start, end time.Time, p WalkForwardParams) (WindowReport, error) {
	lo := sort.Search(len(allDates), func(i int) bool { return !allDates[i].Before(start) })
	hi := sort.Search(len(allDates), func(i int) bool { return allDates[i].After(end) })
	if lo >= hi {
		return WindowReport{}, fmt.Errorf("視窗內無交易日")
	}
	windowDates := allDates[lo:hi]
	initial := cfg.InitialCash
	years := yearsBetween(windowDates[0], windowDates[len(windowDates)-1])
	tradable := tradableAt(cfg, series, windowDates[0])

	// --- 策略 ---
	sCurve, sFlows, sExp, stats, err := runStrategyWindow(cfg, series, windowDates)
	if err != nil {
		return WindowReport{}, err
	}
	strat := curveMetrics(sCurve, initial, years)
	strat.AvgExp = sExp
	sx, sxok := xirr(sFlows)

	// --- Buy & Hold ---
	bh := lumpSumBenchmark(series, windowDates, tradable, initial)
	bhM := curveMetrics(bh.curve, initial, years)
	bhM.AvgExp = bh.avgExposure
	bx, bxok := xirr(bh.deployed)

	// --- 同曝險混合 (constant weight = 策略實際平均曝險) ---
	blendCurve := exposureMatchedBlend(bh.curve, sExp, initial)
	blendM := curveMetrics(blendCurve, initial, years)
	blendM.AvgExp = sExp

	// --- naive DCA ---
	everyK := p.DCAEveryDays
	if everyK < 1 {
		everyK = 1 // 自洽防呆 (上游 walkForwardOnSeries 已設預設,此處避免 evaluateWindow 被單獨呼叫時除零)
	}
	dcaAmt := p.DCAAmount
	if dcaAmt <= 0 {
		// 實際買入日為 i%everyK==0 (i=0,K,2K,...),次數 = (len-1)/everyK + 1;
		// 用真實次數當除數,使每次金額 × 次數恰為整池,避免最後一筆被現金夾取造成前重後輕。
		nBuys := (len(windowDates)-1)/everyK + 1
		dcaAmt = initial / float64(nBuys)
	}
	dca := naiveDCABenchmark(cfg, series, windowDates, initial, everyK, dcaAmt)
	dcaM := curveMetrics(dca.curve, initial, years)
	dcaM.AvgExp = dca.avgExposure

	// --- per-window 判定 ---
	cw, cok := calmarWin(strat.Calmar, bhM.Calmar)
	calmarBeatsBH := cok && cw
	cwB, cokB := calmarWin(strat.Calmar, blendM.Calmar)
	beatsBlend := cokB && cwB && strat.CAGR > blendM.CAGR
	part := math.NaN()
	if bhM.CAGR != 0 {
		part = strat.CAGR / bhM.CAGR
	}

	return WindowReport{
		Start: windowDates[0], End: windowDates[len(windowDates)-1],
		Universe: len(tradable), TradeDays: len(windowDates), Years: years,
		Strat: strat, BH: bhM, Blend: blendM, DCA: dcaM,
		StratXIRR: sx, StratXIRROK: sxok, BHXIRR: bx, BHXIRROK: bxok,
		Buys: stats.TotalBuys, Sells: stats.TotalSells, Skipped: stats.SkippedBuys,
		CalmarBeatsBH: calmarBeatsBH, BeatsBlendBoth: beatsBlend, RetParticipation: part,
	}, nil
}

// runStrategyWindow 以掛了 recorder 的 fresh engine 跑單一視窗,回傳每日權益曲線、
// deployed-capital 現金流 (含期末清算)、平均曝險、累計統計、期末持股市值。
func runStrategyWindow(cfg *config.Config, series map[string]*stockSeries, windowDates []time.Time) ([]float64, []Cashflow, float64, EngineStats, error) {
	engine := NewEngine(cfg)
	var curve []float64
	var flows []Cashflow
	expSum, expN := 0.0, 0
	engine.SetRecorder(&DayRecorder{
		OnCashflow: func(day time.Time, amount float64) {
			flows = append(flows, Cashflow{Date: day, Amount: amount})
		},
		OnEquity: func(day time.Time, equity, cash, holdings float64) {
			curve = append(curve, equity)
			if equity > 0 {
				expSum += holdings / equity
				expN++
			}
		},
	})
	if err := engine.ProcessDates(windowDates, series, noopExecutor{}); err != nil {
		return nil, nil, 0, EngineStats{}, err
	}
	end := windowDates[len(windowDates)-1]
	if fh := engine.HoldingValueAsOf(series, end); fh > 0 {
		flows = append(flows, Cashflow{Date: end, Amount: fh}) // 期末清算為一筆正現金流
	}
	return curve, flows, safeMean(expSum, expN), engine.Stats(), nil
}

// curveMetrics 由權益曲線算出一組績效指標。起始值固定用 initial (資金池起點 = 曲線首日值)。
func curveMetrics(curve []float64, initial, years float64) SeriesMetrics {
	end := initial
	if len(curve) > 0 {
		end = curve[len(curve)-1]
	}
	c := cagr(initial, end, years)
	mdd := maxDrawdown(curve)
	return SeriesMetrics{
		PeriodRet: periodReturn(initial, end),
		CAGR:      c,
		MaxDD:     mdd,
		Calmar:    calmar(c, mdd),
		Sortino:   sortino(dailyReturns(curve), 0, 252),
	}
}

// calmarWin 比較兩個 Calmar。NaN 或雙方皆 +Inf -> 不可比 (comparable=false)。
// Go 中 +Inf > finite 為 true、+Inf > +Inf 為 false,正符合所需語意。
func calmarWin(strat, bench float64) (win, comparable bool) {
	if math.IsNaN(strat) || math.IsNaN(bench) {
		return false, false
	}
	if math.IsInf(strat, 1) && math.IsInf(bench, 1) {
		return false, false
	}
	return strat > bench, true
}

// aggregate 彙整所有視窗並評定五道關卡。
func aggregate(reports []WindowReport) AggregateReport {
	n := len(reports)
	a := AggregateReport{NWindows: n}
	if n == 0 {
		return a
	}

	stratCAGR := make([]float64, 0, n)
	bhCAGR := make([]float64, 0, n)
	stratMDDmag := make([]float64, 0, n)
	bhMDDmag := make([]float64, 0, n)
	stratCalmarFinite := make([]float64, 0, n)
	bhCalmarFinite := make([]float64, 0, n)
	stratAvgExp := make([]float64, 0, n)
	part := make([]float64, 0, n)
	stratXIRR := make([]float64, 0, n)
	bhXIRR := make([]float64, 0, n)

	calmarComparable, calmarWins := 0, 0
	blendSkill := 0
	worstStratMDD, worstBHMDD := 0.0, 0.0
	worstStratCAGR := math.Inf(1)

	for _, r := range reports {
		stratCAGR = append(stratCAGR, r.Strat.CAGR)
		bhCAGR = append(bhCAGR, r.BH.CAGR)
		stratMDDmag = append(stratMDDmag, math.Abs(r.Strat.MaxDD))
		bhMDDmag = append(bhMDDmag, math.Abs(r.BH.MaxDD))
		stratAvgExp = append(stratAvgExp, r.Strat.AvgExp)
		if !math.IsNaN(r.RetParticipation) {
			part = append(part, r.RetParticipation)
		}
		if !math.IsInf(r.Strat.Calmar, 0) && !math.IsNaN(r.Strat.Calmar) {
			stratCalmarFinite = append(stratCalmarFinite, r.Strat.Calmar)
		}
		if !math.IsInf(r.BH.Calmar, 0) && !math.IsNaN(r.BH.Calmar) {
			bhCalmarFinite = append(bhCalmarFinite, r.BH.Calmar)
		}
		if r.StratXIRROK {
			stratXIRR = append(stratXIRR, r.StratXIRR)
		}
		if r.BHXIRROK {
			bhXIRR = append(bhXIRR, r.BHXIRR)
		}

		if _, ok := calmarWin(r.Strat.Calmar, r.BH.Calmar); ok {
			calmarComparable++
			if r.CalmarBeatsBH {
				calmarWins++
			}
		}
		if r.BeatsBlendBoth {
			blendSkill++
		}
		if r.Strat.MaxDD < worstStratMDD {
			worstStratMDD = r.Strat.MaxDD
		}
		if r.BH.MaxDD < worstBHMDD {
			worstBHMDD = r.BH.MaxDD
		}
		if r.Strat.CAGR < worstStratCAGR {
			worstStratCAGR = r.Strat.CAGR
		}
	}

	a.MedStratCAGR = median(stratCAGR)
	a.MedBHCAGR = median(bhCAGR)
	a.MedStratMDD = -median(stratMDDmag) // 以 <=0 呈現
	a.MedBHMDD = -median(bhMDDmag)
	a.MedStratCalmar = median(stratCalmarFinite)
	a.MedBHCalmar = median(bhCalmarFinite)
	a.MedStratAvgExp = median(stratAvgExp)
	a.MedRetParticipation = median(part)
	a.MedStratXIRR = median(stratXIRR)
	a.MedBHXIRR = median(bhXIRR)
	a.NStratXIRRSolvable = len(stratXIRR)
	a.DispersionStratCAGR = stdev(stratCAGR)
	a.WorstStratCAGR = worstStratCAGR
	a.WorstStratMDD = worstStratMDD
	a.WorstBHMDD = worstBHMDD

	if calmarComparable > 0 {
		a.CalmarWinRate = float64(calmarWins) / float64(calmarComparable)
	}
	a.BlendSkillRate = float64(blendSkill) / float64(n)

	medStratMDDmag := median(stratMDDmag)
	medBHMDDmag := median(bhMDDmag)

	// G1「守住 B&H 七成報酬」只在 B&H 中位 CAGR > 0 時才是參與率語意;
	// B&H <= 0 (空頭/走勢差標的) 時改用方向性比較:策略只要不輸 B&H (少賠或不賠) 即過,
	// 避免 0.75×負值把門檻抬到比 B&H 本身還嚴而誤判。
	if a.MedBHCAGR > 0 {
		a.G1RetParticipation = a.MedStratCAGR >= gateRetParticipation*a.MedBHCAGR
	} else {
		a.G1RetParticipation = a.MedStratCAGR >= a.MedBHCAGR
	}
	a.G2RiskReduction = medStratMDDmag <= gateRiskReduction*medBHMDDmag
	a.G3CalmarVsBH = a.CalmarWinRate >= gateCalmarWinRate
	a.G4Skill = a.BlendSkillRate >= gateSkillRate
	a.G5Robustness = math.Abs(worstStratMDD) <= math.Abs(worstBHMDD)
	a.OverallPass = a.G1RetParticipation && a.G2RiskReduction && a.G3CalmarVsBH && a.G4Skill

	return a
}
