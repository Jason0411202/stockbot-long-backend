package kernals

// isoos.go 提供「樣本內 / 樣本外 (In-Sample / Out-Of-Sample) 切分驗證」:把共同有效期依分割日切成
// 前段 (IS,調參用) 與後段 (OOS,驗證用),各自跑 walk-forward 彙整。
//
// 用途 (反過擬合的核心工具):一組參數若只在 IS 漂亮、到 OOS 大幅退化,就是過擬合;
// 若 OOS 指標接近 IS,才算真正穩健、可信。調參時應「只看 IS 挑參數、用 OOS 確認不退化」。
//
// 切分採「時間順序」(前段過去、後段未來,模擬真實上線):跨越分割日的視窗兩組都不計,杜絕資料洩漏。

import (
	"fmt"
	"main/config"
	"time"
)

// ISOOSReport 為一次 IS/OOS 切分驗證的結果。
type ISOOSReport struct {
	SplitDate time.Time
	SplitFrac float64
	NIS, NOOS int             // 各段視窗數
	ISStart   time.Time       // IS 第一個視窗起點
	OOSEnd    time.Time       // OOS 最後一個視窗終點
	IS, OOS   AggregateReport // 各段彙整 scorecard
}

// EvaluateISOOS 依 splitFrac (IS 佔共同有效期的比例;<=0 或 >=1 時預設 0.6) 切分並各自評估。
// IS = 視窗終點 <= 分割日;OOS = 視窗起點 >= 分割日;跨越分割日的視窗不計入任一段。
func EvaluateISOOS(cfg *config.Config, series map[string]*stockSeries, p WalkForwardParams, splitFrac float64) (ISOOSReport, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return ISOOSReport{}, fmt.Errorf("評估目前僅支援 Scaling_Strategy=Baseline")
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
	if splitFrac <= 0 || splitFrac >= 1 {
		splitFrac = 0.6
	}

	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		return ISOOSReport{}, fmt.Errorf("無任何日期可供評估")
	}
	csStart, ok := commonSupportStart(cfg, series)
	if !ok {
		return ISOOSReport{}, fmt.Errorf("無共同有效資料期")
	}
	lastDate := allDates[len(allDates)-1]
	splitDate := csStart.Add(time.Duration(float64(lastDate.Sub(csStart)) * splitFrac))

	windows := generateWindows(cfg, series, allDates, p)
	var isW, oosW [][2]time.Time
	for _, w := range windows {
		switch {
		case !w[1].After(splitDate): // 視窗終點 <= 分割日 → 全在前段
			isW = append(isW, w)
		case !w[0].Before(splitDate): // 視窗起點 >= 分割日 → 全在後段
			oosW = append(oosW, w)
		}
	}

	isReports, err := evalWindowSet(cfg, series, allDates, isW)
	if err != nil {
		return ISOOSReport{}, err
	}
	oosReports, err := evalWindowSet(cfg, series, allDates, oosW)
	if err != nil {
		return ISOOSReport{}, err
	}

	rep := ISOOSReport{
		SplitDate: splitDate, SplitFrac: splitFrac,
		NIS: len(isReports), NOOS: len(oosReports),
		IS: aggregate(isReports), OOS: aggregate(oosReports),
	}
	if len(isReports) > 0 {
		rep.ISStart = isReports[0].Start
	}
	if len(oosReports) > 0 {
		rep.OOSEnd = oosReports[len(oosReports)-1].End
	}
	return rep, nil
}

func evalWindowSet(cfg *config.Config, series map[string]*stockSeries, allDates []time.Time, ws [][2]time.Time) ([]WindowReport, error) {
	out := make([]WindowReport, 0, len(ws))
	for _, w := range ws {
		rep, err := evaluateWindow(cfg, series, allDates, w[0], w[1])
		if err != nil {
			return nil, fmt.Errorf("IS/OOS 視窗 %s: %w", w[0].Format("2006-01-02"), err)
		}
		out = append(out, rep)
	}
	return out, nil
}
