// internal/service/backtest/walkforward.go 實作全期評估、walk-forward 視窗與彙整關卡。
package backtest

import (
	"fmt"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
	"math"
	"sort"
	"time"
)

// walkforward.go 把「單一長區間回測」升級成「多視窗 walk-forward 評估」:對每個視窗同時跑策略與兩個對照組
// (B&H、同曝險 Blend),最後用一張 scorecard 回答:
//
//   「在所有進場時點下,策略能否守住 B&H 七成以上的(資金加權)報酬、用顯著更小的(真實 NAV)回撤、
//     且 Calmar 穩定贏 B&H —— 而且這份優勢來自擇時,不是單純抱現金?」
//
// 問題設定 (problem setting):期初 cfg.InitialCash;cfg.MonthlyContribution>0 時每月第一個交易日再注入該額
// (定版為 0 = lump-sum,僅期初一次性本金、不再外部注資)。
// 方法論:
//   - 報酬一律用資金加權 (XIRR/MWR);外部現金流 = 期初+每月注入(負)、期末清算(正),恰一次變號故必唯一可解。
//     MonthlyContribution=0 時退化為「期初 -E0、期末 +EN」,XIRR 恆等於封閉資金池 CAGR (與舊版一致)。
//   - 回撤用 NAV 單位淨值 (navCurveFromEquity);注資不灌水,量到的是真實投資回撤。
//   - 只在「2 檔共同有效資料期」內產生視窗、視窗起點皆已過 MA 暖身 (同 universe、無暖身空轉、無未來資訊)。
//   - Calmar 會被「抱現金」灌水,故除了比 B&H,還必須贏「同曝險 Blend」才算真擇時。avg_exposure 一律同表呈現。

// WalkForwardParams 為評估參數;0 值會套用合理預設。注資額由 cfg.MonthlyContribution 決定。
type WalkForwardParams struct {
	WindowMonths int // 每個視窗長度 (日曆月),預設 24
	StepMonths   int // 視窗起點間隔,預設 3
	MinTradeDays int // 視窗最少交易日,低於此略過,預設 200
}

// SeriesMetrics 為某一條權益曲線在某視窗的績效指標 (注資設定下)。
type SeriesMetrics struct {
	MWR      float64 // 資金加權年化報酬 (XIRR on 外部現金流);未定義為 NaN
	MWROK    bool    // MWR 是否唯一可解 (注資設定下恆為 true)
	MaxDD    float64 // NAV 單位淨值最大回撤,<= 0 (扣除注資灌水的真實投資回撤)
	Calmar   float64 // MWR / |MaxDD|,可能為 +Inf/NaN
	Sortino  float64 // 年化 Sortino (用 NAV 日報酬)
	AvgExp   float64 // 平均持股佔比 (資金利用率)
	Multiple float64 // 期末權益 / 投入本金 (直觀倍數;受注資時程影響,僅輔助參考)
}

// WindowReport 為單一視窗的完整結果。
type WindowReport struct {
	Start, End time.Time
	Universe   int     // 視窗起點可交易的追蹤股票數
	TradeDays  int     // 視窗內交易日數
	Years      float64 // Actual/365 年數
	TotalIn    float64 // 本視窗投入本金 (期初 + Σ注資)

	Strat SeriesMetrics
	BH    SeriesMetrics // 立刻買滿的 Buy & Hold
	Blend SeriesMetrics // 同曝險混合 (constant weight = Strat.AvgExp)

	Buys, Sells, Skipped    int
	BHBuys                  int     // B&H 等權買滿的次數 (期初 + 各注資日,參考)
	TrailSells, ProfitSells int     // 策略賣出原因拆解 (移動停利 / 獲利了結)
	StratFinalCash          float64 // 策略期末閒置現金 (現金尾巴)

	// 每日權益曲線 (與三者等長,逐日對齊;供前端折線圖 / 視覺化,評估指標不使用)。
	Dates      []time.Time // 視窗內每個交易日
	StratCurve []float64   // 策略每日總權益 (現金 + 持股市值)
	BHCurve    []float64   // B&H 每日總權益

	// per-window 判定
	CalmarBeatsBH    bool    // Strat.Calmar > BH.Calmar (排除 Inf/NaN 不可比)
	BeatsBlendBoth   bool    // Strat 在 Calmar 與 MWR 雙雙贏 Blend (真擇時)
	RetParticipation float64 // Strat.MWR / BH.MWR
}

// 四道核心關卡的門檻 (可依風險偏好調整;集中於此方便校準)。
const (
	gateRetParticipation = 0.75 // G1:中位 Strat MWR >= 75% 中位 BH MWR
	gateRiskReduction    = 0.60 // G2:中位 |Strat MDD| <= 60% 中位 |BH MDD|
	gateCalmarWinRate    = 0.70 // G3:>=70% 視窗 Strat Calmar 贏 BH
	gateSkillRate        = 0.50 // G4:>=50% 視窗 Strat 在 Calmar+MWR 雙贏 Blend
)

// AggregateReport 為跨所有視窗的彙整與 scorecard。
type AggregateReport struct {
	NWindows int

	MedStratMWR, MedBHMWR       float64
	MedStratMDD, MedBHMDD       float64 // 中位最大回撤 (<=0)
	MedStratCalmar, MedBHCalmar float64 // 只取有限值的中位
	MedBlendMWR                 float64 // 同曝險 Blend 中位 MWR (參考)
	MedStratAvgExp              float64
	MedRetParticipation         float64

	CalmarWinRate  float64 // 可比視窗中 Strat Calmar 贏 BH 的比例
	BlendSkillRate float64 // Strat 在 Calmar+MWR 雙贏 Blend 的視窗比例

	DispersionStratMWR float64 // Strat MWR 樣本標準差 (越小越穩定)
	WorstStratMWR      float64
	WorstStratMDD      float64 // 最差 (最負) Strat 回撤
	WorstBHMDD         float64

	G1RetParticipation bool
	G2RiskReduction    bool
	G3CalmarVsBH       bool
	G4Skill            bool
	G5Robustness       bool // 最差視窗 Strat 回撤幅度 <= 最差視窗 BH 回撤幅度
	OverallPass        bool // G1 && G2 && G3 && G4 (核心主張 + 真擇時)
}

// walkForwardOnSeries 為不依賴 DB 的核心,方便單元測試。
func walkForwardOnSeries(cfg *config.Config, series map[string]*trading.StockSeries, p WalkForwardParams) ([]WindowReport, AggregateReport, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return nil, AggregateReport{}, fmt.Errorf("評估目前僅支援 Scaling_Strategy=Baseline")
	}
	// 套用各評估參數的合理預設值。
	if p.WindowMonths <= 0 {
		p.WindowMonths = 24
	}
	if p.StepMonths <= 0 {
		p.StepMonths = 3
	}
	if p.MinTradeDays <= 0 {
		p.MinTradeDays = 200
	}

	// 取得全序列日期並產生滾動視窗列表。
	allDates := trading.CollectDateUnion(series)
	if len(allDates) == 0 {
		return nil, AggregateReport{}, fmt.Errorf("無任何日期可供評估")
	}
	windows := generateWindows(cfg, series, allDates, p)
	if len(windows) == 0 {
		return nil, AggregateReport{}, fmt.Errorf(
			"共同有效資料期不足以產生任何 %d 個月視窗 (step=%d, minTradeDays=%d)",
			p.WindowMonths, p.StepMonths, p.MinTradeDays)
	}

	// 逐視窗評估並收集報告,最後彙整所有視窗的 scorecard。
	reports := make([]WindowReport, 0, len(windows))
	for _, w := range windows {
		rep, err := evaluateWindow(cfg, series, allDates, w[0], w[1])
		if err != nil {
			return nil, AggregateReport{}, fmt.Errorf("視窗 %s: %w", w[0].Format("2006-01-02"), err)
		}
		reports = append(reports, rep)
	}
	return reports, aggregate(reports), nil
}

// EvaluateFullSpan 在「共同有效資料期 ~ 最後資料日」的單一連續區間跑一次注資情境評估,
// 回傳一份 WindowReport (策略 vs B&H vs Blend)。注資動態 (現金累積) 在長區間最明顯,故作為 headline。
func EvaluateFullSpan(cfg *config.Config, series map[string]*trading.StockSeries) (WindowReport, error) {
	allDates := trading.CollectDateUnion(series)
	if len(allDates) == 0 {
		return WindowReport{}, fmt.Errorf("無任何日期可供評估")
	}
	start := allDates[0]
	if cs, ok := commonSupportStart(cfg, series); ok {
		start = cs
	}
	return evaluateWindow(cfg, series, allDates, start, allDates[len(allDates)-1])
}

// CommonIssuanceStart 回傳「所有追蹤股票都已發行 (都有資料)」的最早日期 = 各股票第一筆資料日的最大值。
// 回測 / 上線都從此日起算,確保整段期間每檔追蹤股票都存在 (不在某檔尚未上市的空窗期做決策)。
func CommonIssuanceStart(cfg *config.Config, series map[string]*trading.StockSeries) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, id := range cfg.TrackStocks {
		s, ok := series[id]
		if !ok || len(s.Dates) == 0 {
			continue
		}
		if d := s.Dates[0]; !found || d.After(latest) {
			latest = d
			found = true
		}
	}
	return latest, found
}

// commonSupportStart 回傳「所有追蹤股票皆已具備有效 MA20」的最早日期。
// = 各追蹤股票第 20 個交易日 (Dates[19]) 的最大值。確保每個視窗起點都無 MA20 暖身空轉。
func commonSupportStart(cfg *config.Config, series map[string]*trading.StockSeries) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, id := range cfg.TrackStocks {
		s, ok := series[id]
		if !ok || len(s.Dates) < 20 {
			continue
		}
		d := s.Dates[19]
		if !found || d.After(latest) {
			latest = d
			found = true
		}
	}
	return latest, found
}

// generateWindows 以日曆月為步進,在 [commonSupportStart, 最後資料日] 內產生完整 windowMonths 視窗。
func generateWindows(cfg *config.Config, series map[string]*trading.StockSeries, allDates []time.Time, p WalkForwardParams) [][2]time.Time {
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

// ContributionDue 回傳「交易日 d 相對前一個已處理交易日 prev」該注入的金額:
// prev 為零值 (無前一日,如序列首日) 或 d 與 prev 落在同一日曆月 → 0;跨月 → monthly。monthly<=0 一律 0。
//
// 此函式是回測 (ContributionAmounts) 與線上 (TradingService catch-up / 每日 loop) 共用的注資排程單一事實來源,
// 確保兩條路徑對同一段交易日序列產生「逐日完全相同」的注資時點,使線上忠實反映回測的每月定額注資情境。
func ContributionDue(prev, d time.Time, monthly float64) float64 {
	if monthly <= 0 || prev.IsZero() {
		return 0
	}
	if prev.Year() == d.Year() && prev.Month() == d.Month() {
		return 0
	}
	return monthly
}

// ContributionAmounts 回傳與 windowDates 對齊的「每日注資額」:每個日曆月的第一個交易日 (視窗起始月除外)
// 注入 monthly,其餘為 0。monthly<=0 時全為 0 (退化回無注資)。逐日委派給 ContributionDue,與線上注資同一邏輯。
func ContributionAmounts(windowDates []time.Time, monthly float64) []float64 {
	out := make([]float64, len(windowDates))
	for i := 1; i < len(windowDates); i++ {
		out[i] = ContributionDue(windowDates[i-1], windowDates[i], monthly)
	}
	return out
}

// evaluateWindow 對單一 [start, end] 視窗同時跑策略與兩對照組 (含每月注資),算出所有指標。
func evaluateWindow(cfg *config.Config, series map[string]*trading.StockSeries, allDates []time.Time, start, end time.Time) (WindowReport, error) {
	// 以二元搜尋切出視窗日期範圍並驗證非空。
	lo := sort.Search(len(allDates), func(i int) bool { return !allDates[i].Before(start) })
	hi := sort.Search(len(allDates), func(i int) bool { return allDates[i].After(end) })
	if lo >= hi {
		return WindowReport{}, fmt.Errorf("視窗內無交易日")
	}
	windowDates := allDates[lo:hi]
	initial := cfg.InitialCash
	years := yearsBetween(windowDates[0], windowDates[len(windowDates)-1])
	tradable := tradableAt(cfg, series, windowDates[0])
	contribOnDay := ContributionAmounts(windowDates, cfg.MonthlyContribution)

	// --- 策略 ---
	stratArm, err := runStratArm(cfg, series, windowDates, contribOnDay)
	if err != nil {
		return WindowReport{}, err
	}
	strat := armMetrics(stratArm, initial)

	// --- Buy & Hold (立刻買滿) ---
	bhArm := bhImmediateArm(cfg, series, windowDates, contribOnDay)
	bh := armMetrics(bhArm, initial)

	// --- 同曝險 Blend (constant weight = 策略實際平均曝險) ---
	bhNav := navCurveFromEquity(bhArm.curve, bhArm.contribOnDay, initial)
	blend := blendMetrics(bhNav, stratArm.avgExposure, contribOnDay, initial, windowDates)

	// --- per-window 判定 ---
	cw, cok := calmarWin(strat.Calmar, bh.Calmar)
	calmarBeatsBH := cok && cw
	cwB, cokB := calmarWin(strat.Calmar, blend.Calmar)
	beatsBlend := cokB && cwB && strat.MWR > blend.MWR
	// 計算策略對 B&H 的報酬參與率;B&H MWR 為 0 時保留 NaN 避免除零。
	part := math.NaN()
	if bh.MWR != 0 {
		part = strat.MWR / bh.MWR
	}

	return WindowReport{
		Start: windowDates[0], End: windowDates[len(windowDates)-1],
		Universe: len(tradable), TradeDays: len(windowDates), Years: years, TotalIn: stratArm.totalIn,
		Strat: strat, BH: bh, Blend: blend,
		Buys: stratArm.buys, Sells: stratArm.sells, Skipped: stratArm.skipped, BHBuys: bhArm.buys,
		TrailSells: stratArm.trailSells, ProfitSells: stratArm.profitSells, StratFinalCash: stratArm.finalCash,
		Dates: windowDates, StratCurve: stratArm.curve, BHCurve: bhArm.curve,
		CalmarBeatsBH: calmarBeatsBH, BeatsBlendBoth: beatsBlend, RetParticipation: part,
	}, nil
}

// runStratArm 以掛了 recorder 的 fresh engine 跑單一視窗 (含每月注資),回傳完整 armResult。
func runStratArm(cfg *config.Config, series map[string]*trading.StockSeries, windowDates []time.Time, contribOnDay []float64) (armResult, error) {
	// 建立 fresh engine 並掛上 recorder 以收集每日權益曲線與平均曝險。
	engine := trading.NewEngine(cfg)
	var curve []float64
	expSum, expN := 0.0, 0
	engine.SetRecorder(&trading.DayRecorder{
		OnEquity: func(_ time.Time, equity, _, holdings float64) {
			curve = append(curve, equity)
			if equity > 0 {
				expSum += holdings / equity
				expN++
			}
		},
	})

	// 逐日注資並驅動引擎,記錄每筆注資為負向現金流。
	flows := []Cashflow{{Date: windowDates[0], Amount: -cfg.InitialCash}}
	totalIn := cfg.InitialCash
	for i, d := range windowDates {
		if contribOnDay[i] > 0 {
			engine.AddCash(contribOnDay[i]) // 注資先入袋,當日即可動用
			flows = append(flows, Cashflow{Date: d, Amount: -contribOnDay[i]})
			totalIn += contribOnDay[i]
		}
		if err := engine.ProcessDay(d, series, trading.NoopExecutor{}); err != nil {
			return armResult{}, err
		}
	}

	// 以 as-of 估值結算期末持股市值,追加期末正向現金流並組裝結果。
	end := windowDates[len(windowDates)-1]
	finalCash := engine.Cash()
	finalEq := finalCash + engine.HoldingValueAsOf(series, end)
	flows = append(flows, Cashflow{Date: end, Amount: finalEq})
	stats := engine.Stats()
	return armResult{
		curve: curve, contribOnDay: contribOnDay, flows: flows,
		avgExposure: safeMean(expSum, expN), finalEquity: finalEq, totalIn: totalIn, finalCash: finalCash,
		buys: stats.TotalBuys, sells: stats.TotalSells, skipped: stats.SkippedBuys,
		trailSells: stats.TrailSells, profitSells: stats.ProfitSells,
	}, nil
}

// armMetrics 由 armResult 算出績效指標:資金加權報酬 (MWR) + NAV 回撤 + Calmar + 曝險。
func armMetrics(a armResult, initial float64) SeriesMetrics {
	nav := navCurveFromEquity(a.curve, a.contribOnDay, initial)
	mwr, ok := xirr(a.flows)
	mdd := maxDrawdown(nav)
	cal := math.NaN()
	if ok {
		cal = calmar(mwr, mdd)
	}
	return SeriesMetrics{
		MWR: mwr, MWROK: ok, MaxDD: mdd, Calmar: cal,
		Sortino: sortino(dailyReturns(nav), 0, 252),
		AvgExp:  a.avgExposure, Multiple: safeDiv(a.finalEquity, a.totalIn),
	}
}

// calmarWin 比較兩個 Calmar。NaN 或雙方皆 +Inf -> 不可比 (comparable=false)。
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

	// 初始化各指標的收集切片與計數器。
	stratMWR := make([]float64, 0, n)
	bhMWR := make([]float64, 0, n)
	blendMWR := make([]float64, 0, n)
	stratMDDmag := make([]float64, 0, n)
	bhMDDmag := make([]float64, 0, n)
	stratCalmarFinite := make([]float64, 0, n)
	bhCalmarFinite := make([]float64, 0, n)
	stratAvgExp := make([]float64, 0, n)
	part := make([]float64, 0, n)

	calmarComparable, calmarWins := 0, 0
	blendSkill := 0
	worstStratMDD, worstBHMDD := 0.0, 0.0
	worstStratMWR := math.Inf(1)

	// 逐視窗收集指標值,同時統計 Calmar 可比視窗數、勝率、最差回撤與最差 MWR。
	for _, r := range reports {
		stratMWR = append(stratMWR, r.Strat.MWR)
		bhMWR = append(bhMWR, r.BH.MWR)
		blendMWR = append(blendMWR, r.Blend.MWR)
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
		if r.Strat.MWR < worstStratMWR {
			worstStratMWR = r.Strat.MWR
		}
	}

	// 計算各指標的中位數、離散度與勝率,填入彙整報告。
	a.MedStratMWR = median(stratMWR)
	a.MedBHMWR = median(bhMWR)
	a.MedBlendMWR = median(blendMWR)
	a.MedStratMDD = -median(stratMDDmag) // 以 <=0 呈現
	a.MedBHMDD = -median(bhMDDmag)
	a.MedStratCalmar = median(stratCalmarFinite)
	a.MedBHCalmar = median(bhCalmarFinite)
	a.MedStratAvgExp = median(stratAvgExp)
	a.MedRetParticipation = median(part)
	a.DispersionStratMWR = stdev(stratMWR)
	a.WorstStratMWR = worstStratMWR
	a.WorstStratMDD = worstStratMDD
	a.WorstBHMDD = worstBHMDD

	if calmarComparable > 0 {
		a.CalmarWinRate = float64(calmarWins) / float64(calmarComparable)
	}
	a.BlendSkillRate = float64(blendSkill) / float64(n)

	medStratMDDmag := median(stratMDDmag)
	medBHMDDmag := median(bhMDDmag)

	// G1「守住 B&H 七成報酬」只在 B&H 中位 MWR > 0 時才是參與率語意;
	// B&H <= 0 (空頭/走勢差標的) 時改用方向性比較:策略只要不輸 B&H 即過,避免 0.75×負值把門檻抬得比 B&H 還嚴。
	if a.MedBHMWR > 0 {
		a.G1RetParticipation = a.MedStratMWR >= gateRetParticipation*a.MedBHMWR
	} else {
		a.G1RetParticipation = a.MedStratMWR >= a.MedBHMWR
	}
	// 評定 G2~G5 各關卡並計算總體是否通過。
	a.G2RiskReduction = medStratMDDmag <= gateRiskReduction*medBHMDDmag
	a.G3CalmarVsBH = a.CalmarWinRate >= gateCalmarWinRate
	a.G4Skill = a.BlendSkillRate >= gateSkillRate
	a.G5Robustness = math.Abs(worstStratMDD) <= math.Abs(worstBHMDD)
	a.OverallPass = a.G1RetParticipation && a.G2RiskReduction && a.G3CalmarVsBH && a.G4Skill

	return a
}
