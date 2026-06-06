package backtest

// isoos.go 提供「滾動式 walk-forward 樣本外驗證 (rolling / anchored walk-forward OOS)」:
// 錨定一段初始樣本內 (IS,前段) 後,其後「每一個視窗都只用它之前的資料判斷 → 視為樣本外 (OOS)」,
// 並把 OOS 依時間切成多個「折 (fold)」,檢查策略在「多個不同的未來子期間」是否都站得住,
// 而非只看單一一段。比固定單一 IS/OOS 切分有更多 OOS 樣本、更能照出過擬合。
//
// 反過擬合用法:一組參數若 OOS 彙整接近 IS、且每一折都不崩,才算真穩健;
// 若只有 IS 漂亮、OOS 彙整或某些折大幅退化,就是過擬合。
// 切分採時間順序 (前段訓練、後段測試),跨錨定日的視窗不計入任一邊,杜絕洩漏。

import (
	"fmt"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
	"time"
)

// OOSFold 為單一滾動折 (一段連續未來子期間) 的 OOS 結果。
type OOSFold struct {
	FirstStart time.Time
	LastEnd    time.Time
	N          int
	Calmar     float64 // 中位 Strat Calmar
	CalmarWin  float64 // Calmar 勝率
	GatesPass  int     // 通過的關卡數 (0~5)
}

// RollingOOSReport 為一次滾動 walk-forward OOS 驗證的結果。
type RollingOOSReport struct {
	ISMonths   int
	FoldMonths int
	Anchor     time.Time       // 初始 IS 與 OOS 的分界日
	NIS, NOOS  int             // IS / OOS 視窗數
	IS, OOS    AggregateReport // IS = 錨定前段;OOS = 錨定後「全部」視窗彙整
	Folds      []OOSFold       // OOS 依 FoldMonths 切成的滾動折
}

// EvaluateRollingOOS 以「初始 IS 錨定 isMonths 個月、其後滾動 OOS」評估。
//
//	IS  = 視窗終點 <= 錨定日 (僅供對照的訓練段)。
//	OOS = 視窗起點 >= 錨定日 (held-out 未來;每窗都只用其之前資料 → 樣本外),整體彙整 + 依 foldMonths 分折。
//
// isMonths<=0 預設 36;foldMonths<=0 預設 12。
func EvaluateRollingOOS(cfg *config.Config, series map[string]*trading.StockSeries, p WalkForwardParams, isMonths, foldMonths int) (RollingOOSReport, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return RollingOOSReport{}, fmt.Errorf("評估目前僅支援 Scaling_Strategy=Baseline")
	}
	if p.WindowMonths <= 0 {
		p.WindowMonths = 24
	}
	if p.StepMonths <= 0 {
		p.StepMonths = 3
	}
	if p.MinTradeDays <= 0 {
		p.MinTradeDays = 200
	}
	if isMonths <= 0 {
		isMonths = 36
	}
	if foldMonths <= 0 {
		foldMonths = 12
	}

	allDates := trading.CollectDateUnion(series)
	if len(allDates) == 0 {
		return RollingOOSReport{}, fmt.Errorf("無任何日期可供評估")
	}
	csStart, ok := commonSupportStart(cfg, series)
	if !ok {
		return RollingOOSReport{}, fmt.Errorf("無共同有效資料期")
	}
	anchor := csStart.AddDate(0, isMonths, 0)

	windows := generateWindows(cfg, series, allDates, p)
	var isW [][2]time.Time
	var oosW [][2]time.Time
	for _, w := range windows {
		switch {
		case !w[1].After(anchor): // 視窗終點 <= 錨定日 → IS
			isW = append(isW, w)
		case !w[0].Before(anchor): // 視窗起點 >= 錨定日 → OOS
			oosW = append(oosW, w)
		}
	}

	isReports, err := evalWindowSet(cfg, series, allDates, isW)
	if err != nil {
		return RollingOOSReport{}, err
	}
	oosReports, err := evalWindowSet(cfg, series, allDates, oosW)
	if err != nil {
		return RollingOOSReport{}, err
	}

	rep := RollingOOSReport{
		ISMonths: isMonths, FoldMonths: foldMonths, Anchor: anchor,
		NIS: len(isReports), NOOS: len(oosReports),
		IS: aggregate(isReports), OOS: aggregate(oosReports),
	}

	// 依「視窗起點落在錨定日後第幾個 foldMonths 段」分折。
	groups := map[int][]WindowReport{}
	var order []int
	for _, r := range oosReports {
		k := monthsBetween(anchor, r.Start) / foldMonths
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	sortInts(order)
	for _, k := range order {
		g := groups[k]
		a := aggregate(g)
		gates := 0
		for _, ok := range []bool{a.G1RetParticipation, a.G2RiskReduction, a.G3CalmarVsBH, a.G4Skill, a.G5Robustness} {
			if ok {
				gates++
			}
		}
		rep.Folds = append(rep.Folds, OOSFold{
			FirstStart: g[0].Start, LastEnd: g[len(g)-1].End, N: len(g),
			Calmar: a.MedStratCalmar, CalmarWin: a.CalmarWinRate, GatesPass: gates,
		})
	}
	return rep, nil
}

func evalWindowSet(cfg *config.Config, series map[string]*trading.StockSeries, allDates []time.Time, ws [][2]time.Time) ([]WindowReport, error) {
	out := make([]WindowReport, 0, len(ws))
	for _, w := range ws {
		rep, err := evaluateWindow(cfg, series, allDates, w[0], w[1])
		if err != nil {
			return nil, fmt.Errorf("OOS 視窗 %s: %w", w[0].Format("2006-01-02"), err)
		}
		out = append(out, rep)
	}
	return out, nil
}

// monthsBetween 回傳 a→b 的整數月數差 (b>=a;以日曆月計)。
func monthsBetween(a, b time.Time) int {
	m := (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
	if m < 0 {
		m = 0
	}
	return m
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
