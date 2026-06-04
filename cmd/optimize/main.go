package main

// cmd/optimize 是「自主參數掃描 / 優化」runner。
//
// 流程:
//   1. 從本機 CSV 快取 (data/*.csv) 載入歷史資料 — 完全不依賴 MariaDB / docker。
//   2. 以 config.yaml 為 baseline,跑一次 walk-forward 取得 scorecard。
//   3. 對「優化旋鈕登錄表」做 coordinate-ascent:逐一旋鈕做參數掃描 (sweep),
//      每個候選值都在當前 champion 上套用後跑 walk-forward;以 objective + 護欄判定勝負:
//        - 勝者 (顯著改善風險調整報酬且不犧牲報酬/真擇時) → 併入 champion (留)。
//        - 敗者 → 丟棄,champion 不變。
//   4. 多輪 (pass) 掃描以捕捉旋鈕間交互;最後輸出 champion 演進、最終 config 與 metrics。
//
// objective (可由 -objective 切換):
//   composite = clampedCalmar × clampedParticipation
//     - clampedCalmar      : 中位 Strat Calmar (NaN 判出局, +Inf 上限 10)
//     - clampedParticipation: 中位 (Strat CAGR / B&H CAGR),夾到 [0,1.5];NaN 視為 1
//   直覺:同時獎勵「風險調整後報酬」與「相對 B&H 的報酬參與率」,
//   低參與率 (放棄報酬、純抱現金) 會被懲罰 → 對齊「穩定、低風險、又有競爭力」。

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"main/config"
	"main/kernals"
)

// sweepRes 為單一候選值在 walk-forward 下的評估結果。
type sweepRes struct {
	val   float64
	agg   kernals.AggregateReport
	score float64
	ok    bool
}

// knob 為單一可掃描旋鈕。apply 把候選值寫進 config 副本;grid 為候選值集合。
type knob struct {
	name   string
	note   string
	apply  func(*config.Config, float64)
	grid   []float64
	fmtVal func(float64) string
}

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "baseline 設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗長度 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	passes := flag.Int("passes", 2, "coordinate-ascent 輪數")
	objective := flag.String("objective", "composite", "目標函式: composite | calmar | cagr")
	reportPath := flag.String("report", "docs/optimization/sweep-results.md", "結果報告輸出路徑")
	flag.Parse()

	base, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 config 失敗:", err)
		os.Exit(1)
	}
	series, err := kernals.LoadSeriesFromCSV(*dataDir, base.TrackStocks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 CSV 失敗:", err)
		os.Exit(1)
	}

	wfp := kernals.WalkForwardParams{WindowMonths: *window, StepMonths: *step, MinTradeDays: *minDays}
	scoreFn := makeScorer(*objective)

	var rep strings.Builder
	logf := func(format string, a ...interface{}) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		rep.WriteString(line + "\n")
	}

	eval := func(c *config.Config) (kernals.AggregateReport, bool) {
		_, agg, err := kernals.EvaluateWalkForward(c, series, wfp)
		if err != nil {
			return kernals.AggregateReport{}, false
		}
		return agg, true
	}

	logf("# 自主參數掃描結果 (walk-forward / 防 overfit)")
	logf("")
	logf("- 資料: %v  視窗 %d 月 / 步進 %d 月 / 最少 %d 交易日", base.TrackStocks, *window, *step, *minDays)
	logf("- 目標函式: %s", *objective)
	logf("- 選股留汰: 候選值在當前 champion 上套用後跑 walk-forward;勝者併入、敗者丟棄")
	logf("")

	champion := cloneConfig(base)
	baseAgg, ok := eval(champion)
	if !ok {
		fmt.Fprintln(os.Stderr, "baseline 評估失敗 (視窗不足?)")
		os.Exit(1)
	}
	logf("## Baseline scorecard")
	logf("```")
	logf("%s", formatAgg(baseAgg))
	logf("```")
	logf("baseline 目標分數 = %.4f", scoreFn(baseAgg))
	logf("")

	knobs := buildKnobs()
	champScore := scoreFn(baseAgg)
	champAgg := baseAgg
	type adoption struct {
		knob   string
		val    string
		before float64
		after  float64
	}
	var adopted []adoption

	for pass := 1; pass <= *passes; pass++ {
		logf("## Pass %d / %d", pass, *passes)
		logf("")
		changedThisPass := false
		for _, k := range knobs {
			// 對此旋鈕做 sweep:每個候選值套在 champion 上評估。
			results := make([]sweepRes, 0, len(k.grid))
			for _, v := range k.grid {
				c := cloneConfig(champion)
				k.apply(c, v)
				agg, ok := eval(c)
				s := math.Inf(-1)
				if ok {
					s = scoreFn(agg)
				}
				results = append(results, sweepRes{val: v, agg: agg, score: s, ok: ok})
			}
			// 找最佳候選。
			best := results[0]
			for _, r := range results[1:] {
				if r.ok && r.score > best.score {
					best = r
				}
			}
			// 印 sweep 小表。
			logf("### 旋鈕: %s — %s", k.name, k.note)
			logf("```")
			logf("%s", formatSweep(k, results, scoreFn, champScore))
			logf("```")

			// 判定: best 是否勝過 champion (改善 + 護欄)。
			if best.ok && isImprovement(best.score, champScore, best.agg, champAgg) {
				k.apply(champion, best.val)
				adopted = append(adopted, adoption{knob: k.name, val: k.fmtVal(best.val), before: champScore, after: best.score})
				logf("✅ 採納 %s=%s  (分數 %.4f → %.4f)", k.name, k.fmtVal(best.val), champScore, best.score)
				champScore = best.score
				champAgg = best.agg
				changedThisPass = true
			} else {
				logf("· 丟棄 %s (最佳候選 %s 分數 %.4f 未顯著勝過 champion %.4f 或違反護欄)",
					k.name, k.fmtVal(best.val), best.score, champScore)
			}
			logf("")
		}
		if !changedThisPass {
			logf("Pass %d 無任何旋鈕被採納,提早收斂。", pass)
			logf("")
			break
		}
	}

	// ── 最終彙整 ──
	logf("## 最終結果")
	logf("")
	if len(adopted) == 0 {
		logf("沒有任何旋鈕勝過 baseline — baseline 已是掃描範圍內的局部最佳。")
	} else {
		logf("採納的旋鈕 (champion 演進):")
		logf("")
		logf("| # | 旋鈕 | 採用值 | 分數 before→after |")
		logf("| --- | --- | --- | --- |")
		for i, a := range adopted {
			logf("| %d | %s | %s | %.4f → %.4f |", i+1, a.knob, a.val, a.before, a.after)
		}
	}
	logf("")
	logf("### Baseline vs 最終 champion scorecard")
	logf("```")
	logf("%s", formatCompare(baseAgg, champAgg))
	logf("```")
	logf("baseline 分數 %.4f → champion 分數 %.4f  (Δ %+.4f)", scoreFn(baseAgg), champScore, champScore-scoreFn(baseAgg))
	logf("")
	logf("### 最終 champion 的覆寫旋鈕 (相對 config.yaml)")
	logf("```yaml")
	logf("%s", diffConfig(base, champion))
	logf("```")

	if err := os.WriteFile(*reportPath, []byte(rep.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 寫報告失敗:", err)
	} else {
		fmt.Fprintln(os.Stderr, "報告已寫入:", *reportPath)
	}
}

// ───────────────────────── objective / 判定 ─────────────────────────

func makeScorer(kind string) func(kernals.AggregateReport) float64 {
	switch kind {
	case "calmar":
		return func(a kernals.AggregateReport) float64 { return clampCalmar(a.MedStratCalmar) }
	case "cagr":
		return func(a kernals.AggregateReport) float64 {
			if math.IsNaN(a.MedStratCAGR) {
				return math.Inf(-1)
			}
			return a.MedStratCAGR
		}
	case "gates":
		// 直接最大化「通過幾道 scorecard 關卡 (G1~G5)」,同關卡數內再用 composite 細排。
		// 這最貼近專案自身對『好策略』的定義 (穩定+低風險+有競爭力+真擇時)。
		return func(a kernals.AggregateReport) float64 {
			n := 0
			for _, g := range []bool{a.G1RetParticipation, a.G2RiskReduction, a.G3CalmarVsBH, a.G4Skill, a.G5Robustness} {
				if g {
					n++
				}
			}
			cal := clampCalmar(a.MedStratCalmar)
			if math.IsInf(cal, -1) {
				cal = 0
			}
			part := a.MedRetParticipation
			if math.IsNaN(part) || math.IsInf(part, 0) {
				part = 1.0
			}
			if part < 0 {
				part = 0
			} else if part > 1.5 {
				part = 1.5
			}
			return float64(n) + 0.05*cal*part // 整數關卡數 + 小數細排
		}
	default: // composite
		return func(a kernals.AggregateReport) float64 {
			cal := clampCalmar(a.MedStratCalmar)
			if math.IsInf(cal, -1) {
				return math.Inf(-1)
			}
			part := a.MedRetParticipation
			if math.IsNaN(part) || math.IsInf(part, 0) {
				part = 1.0
			}
			if part < 0 {
				part = 0
			}
			if part > 1.5 {
				part = 1.5
			}
			return cal * part
		}
	}
}

func clampCalmar(cal float64) float64 {
	if math.IsNaN(cal) {
		return math.Inf(-1) // 不可比 → 出局
	}
	if math.IsInf(cal, 1) {
		return 10 // 無回撤的 Calmar 上限,避免主導
	}
	return cal
}

// isImprovement 判定 challenger 是否勝過 champion:
//   - 目標分數顯著改善 (相對 +0.5% 且絕對 +1e-4)
//   - 護欄1: 中位 Strat CAGR 不比 champion 差超過 12% (相對) → 不大幅犧牲報酬
//   - 護欄2: 真擇時勝率 (BlendSkillRate) 不比 champion 低超過 0.08 → 不靠抱現金假贏
func isImprovement(chScore, champScore float64, ch, champ kernals.AggregateReport) bool {
	if math.IsInf(chScore, -1) || math.IsNaN(chScore) {
		return false
	}
	improved := chScore > champScore+1e-4 && chScore > champScore*(1+0.005)
	if champScore <= 0 { // champion 分數非正時改用絕對門檻
		improved = chScore > champScore+1e-4
	}
	if !improved {
		return false
	}
	// 護欄1: 報酬不大幅退步。
	champCAGR := champ.MedStratCAGR
	tol := 0.12 * math.Abs(champCAGR)
	if ch.MedStratCAGR < champCAGR-tol-1e-9 {
		return false
	}
	// 護欄2: 真擇時不崩。
	if ch.BlendSkillRate < champ.BlendSkillRate-0.08 {
		return false
	}
	return true
}

// ───────────────────────── 旋鈕登錄表 (wave 1) ─────────────────────────

func buildKnobs() []knob {
	intFmt := func(v float64) string { return fmt.Sprintf("%d", int(v)) }
	pctFmt := func(v float64) string { return fmt.Sprintf("%.2f", v) }
	return []knob{
		{
			name: "cooldown_days", note: "買後冷卻天數 (#3)",
			apply:  func(c *config.Config, v float64) { c.CooldownDays = int(v) },
			grid:   []float64{3, 5, 7, 10, 14, 21, 28, 35, 42, 56, 70, 90},
			fmtVal: intFmt,
		},
		{
			name: "sell_threshold", note: "獲利出場門檻 (#2/#50)",
			apply:  func(c *config.Config, v float64) { c.BaselineSellThreshold = v },
			grid:   []float64{0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 1.0, 1.2, 1.5, 2.0},
			fmtVal: pctFmt,
		},
		{
			name: "ma_window", note: "進場均線長度 (#4/#68)",
			apply:  func(c *config.Config, v float64) { c.MAWindow = int(v) },
			grid:   []float64{5, 10, 15, 20, 30, 40, 50, 60, 80, 120},
			fmtVal: intFmt,
		},
		{
			name: "bias_min", note: "乖離率最小進場深度 (#8/#17)",
			apply:  func(c *config.Config, v float64) { c.BuyBiasMin = v },
			grid:   []float64{0, 0.005, 0.01, 0.02, 0.03, 0.05, 0.08, 0.10},
			fmtVal: pctFmt,
		},
		{
			name: "long_ma_above", note: "長均線多頭硬濾網 (僅站上長均線才買, #5);0=off",
			apply: func(c *config.Config, v float64) {
				if v <= 0 {
					c.BuyLongMAWindow = 0
					c.BuyRequireAboveLongMA = false
					return
				}
				c.BuyLongMAWindow = int(v)
				c.BuyRequireAboveLongMA = true
			},
			grid:   []float64{0, 60, 100, 120, 150, 200, 240},
			fmtVal: intFmt,
		},
		{
			name: "long_ma_slope_up", note: "長均線斜率向上才買 (#30/#80);0=off",
			apply: func(c *config.Config, v float64) {
				if v <= 0 {
					c.BuyRequireLongMASlopeUp = false
					return
				}
				if c.BuyLongMAWindow <= 0 {
					c.BuyLongMAWindow = int(v)
				}
				c.BuyRequireLongMASlopeUp = true
			},
			grid:   []float64{0, 60, 120, 200},
			fmtVal: intFmt,
		},
		{
			name: "buy_rsi_max", note: "RSI(14) 超賣進場 gate (#34);0=off",
			apply: func(c *config.Config, v float64) {
				if v <= 0 {
					c.BuyRSIPeriod = 0
					return
				}
				c.BuyRSIPeriod = 14
				c.BuyRSIMax = v
			},
			grid:   []float64{0, 25, 30, 35, 40, 45, 50},
			fmtVal: pctFmt,
		},
		{
			name: "buy_confirm_up", note: "今收>昨收止穩才買 (#33);0=off,1=on",
			apply: func(c *config.Config, v float64) { c.BuyConfirmUp = v >= 1 },
			grid:  []float64{0, 1},
			fmtVal: func(v float64) string {
				if v >= 1 {
					return "on"
				}
				return "off"
			},
		},
		{
			name: "sell_rsi_min", note: "RSI(14) 超買才出場 gate (#39/#94);0=off",
			apply: func(c *config.Config, v float64) {
				if v <= 0 {
					c.SellRSIPeriod = 0
					return
				}
				c.SellRSIPeriod = 14
				c.SellRSIMin = v
			},
			grid:   []float64{0, 50, 55, 60, 65, 70, 75},
			fmtVal: pctFmt,
		},
		{
			name: "multiplier", note: "買賣金額乘數 (#55)",
			apply:  func(c *config.Config, v float64) { c.BuyAndSellMultiplier = v },
			grid:   []float64{1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0},
			fmtVal: pctFmt,
		},
		{
			name: "sell_amount", note: "每次賣出目標金額 (#47/#48);與買入解耦",
			apply:  func(c *config.Config, v float64) { c.BaselineSellAmount = v },
			grid:   []float64{2000, 4000, 6000, 10000, 15000, 20000, 30000, 40000},
			fmtVal: intFmt,
		},
		{
			name: "buy_tier_ratio", note: "加碼曲線陡度:跌越深買越多倍 (#9/#79);1=平坦,>1=金字塔",
			apply: func(c *config.Config, v float64) {
				if c.BuyBaseAmount <= 0 {
					c.BuyBaseAmount = 500 // 沿用現行最淺 tier 金額當基底
				}
				c.BuyTierRatio = v
			},
			grid:   []float64{1.0, 1.25, 1.5, 1.75, 2.0, 2.5},
			fmtVal: pctFmt,
		},
		{
			name: "buy_base_amount", note: "每次買入基礎金額 (淺跌時, #9);需搭配幾何曲線",
			apply: func(c *config.Config, v float64) {
				if c.BuyTierRatio <= 0 {
					c.BuyTierRatio = 1.5 // 沿用近似現行曲線陡度
				}
				c.BuyBaseAmount = v
			},
			grid:   []float64{300, 500, 800, 1200, 2000, 3000},
			fmtVal: intFmt,
		},
		{
			name: "cash_floor", note: "保留現金比例不部署 dry powder (#22)",
			apply:  func(c *config.Config, v float64) { c.CashFloorFrac = v },
			grid:   []float64{0, 0.05, 0.10, 0.20, 0.30},
			fmtVal: pctFmt,
		},
	}
}

// ───────────────────────── 格式化 ─────────────────────────

func pct(x float64) string {
	if math.IsNaN(x) {
		return "n/a"
	}
	if math.IsInf(x, 0) {
		return "inf"
	}
	return fmt.Sprintf("%+.1f%%", x*100)
}

func ratio(x float64) string {
	if math.IsNaN(x) {
		return "n/a"
	}
	if math.IsInf(x, 1) {
		return "inf"
	}
	if math.IsInf(x, -1) {
		return "-inf"
	}
	return fmt.Sprintf("%.2f", x)
}

func formatAgg(a kernals.AggregateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "視窗數 %d\n", a.NWindows)
	fmt.Fprintf(&b, "中位 CAGR     Strat %s | B&H %s | 參與率 %s\n", pct(a.MedStratCAGR), pct(a.MedBHCAGR), ratio(a.MedRetParticipation))
	fmt.Fprintf(&b, "中位 MaxDD    Strat %s | B&H %s\n", pct(a.MedStratMDD), pct(a.MedBHMDD))
	fmt.Fprintf(&b, "中位 Calmar   Strat %s | B&H %s\n", ratio(a.MedStratCalmar), ratio(a.MedBHCalmar))
	fmt.Fprintf(&b, "中位 XIRR     Strat %s | B&H %s (可解 %d/%d)\n", pct(a.MedStratXIRR), pct(a.MedBHXIRR), a.NStratXIRRSolvable, a.NWindows)
	fmt.Fprintf(&b, "平均曝險      %s\n", pct(a.MedStratAvgExp))
	fmt.Fprintf(&b, "Calmar 勝率(vsB&H) %s | 真擇時勝率(vsBlend) %s\n", pct(a.CalmarWinRate), pct(a.BlendSkillRate))
	fmt.Fprintf(&b, "CAGR 離散 %s | 最差視窗 CAGR %s MaxDD %s\n", pct(a.DispersionStratCAGR), pct(a.WorstStratCAGR), pct(a.WorstStratMDD))
	fmt.Fprintf(&b, "Gates: G1=%v G2=%v G3=%v G4=%v G5=%v | OverallPass=%v", a.G1RetParticipation, a.G2RiskReduction, a.G3CalmarVsBH, a.G4Skill, a.G5Robustness, a.OverallPass)
	return b.String()
}

func formatSweep(k knob, results []sweepRes, scoreFn func(kernals.AggregateReport) float64, champScore float64) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "值\tCAGR\tMaxDD\tCalmar\t曝險\t真擇時\t分數")
	for _, r := range results {
		if !r.ok {
			fmt.Fprintf(w, "%s\t(評估失敗)\n", k.fmtVal(r.val))
			continue
		}
		mark := ""
		if r.score > champScore+1e-9 {
			mark = " *"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%.4f%s\n",
			k.fmtVal(r.val), pct(r.agg.MedStratCAGR), pct(r.agg.MedStratMDD),
			ratio(r.agg.MedStratCalmar), pct(r.agg.MedStratAvgExp), pct(r.agg.BlendSkillRate), r.score, mark)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func formatCompare(base, champ kernals.AggregateReport) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "指標\tBaseline\tChampion")
	fmt.Fprintf(w, "中位 CAGR\t%s\t%s\n", pct(base.MedStratCAGR), pct(champ.MedStratCAGR))
	fmt.Fprintf(w, "中位 MaxDD\t%s\t%s\n", pct(base.MedStratMDD), pct(champ.MedStratMDD))
	fmt.Fprintf(w, "中位 Calmar\t%s\t%s\n", ratio(base.MedStratCalmar), ratio(champ.MedStratCalmar))
	fmt.Fprintf(w, "中位 XIRR\t%s\t%s\n", pct(base.MedStratXIRR), pct(champ.MedStratXIRR))
	fmt.Fprintf(w, "參與率\t%s\t%s\n", ratio(base.MedRetParticipation), ratio(champ.MedRetParticipation))
	fmt.Fprintf(w, "平均曝險\t%s\t%s\n", pct(base.MedStratAvgExp), pct(champ.MedStratAvgExp))
	fmt.Fprintf(w, "Calmar勝率\t%s\t%s\n", pct(base.CalmarWinRate), pct(champ.CalmarWinRate))
	fmt.Fprintf(w, "真擇時勝率\t%s\t%s\n", pct(base.BlendSkillRate), pct(champ.BlendSkillRate))
	fmt.Fprintf(w, "CAGR離散\t%s\t%s\n", pct(base.DispersionStratCAGR), pct(champ.DispersionStratCAGR))
	fmt.Fprintf(w, "最差MaxDD\t%s\t%s\n", pct(base.WorstStratMDD), pct(champ.WorstStratMDD))
	fmt.Fprintf(w, "OverallPass\t%v\t%v\n", base.OverallPass, champ.OverallPass)
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// ───────────────────────── config 複製 / diff ─────────────────────────

func cloneConfig(c *config.Config) *config.Config {
	cp := *c
	cp.TrackStocks = append([]string(nil), c.TrackStocks...)
	cp.BaselineBuyTiers = append([]config.BaselineBuyTier(nil), c.BaselineBuyTiers...)
	return &cp
}

// diffConfig 印出 champion 相對 base 有變動的旋鈕 (yaml 風格)。
func diffConfig(base, champ *config.Config) string {
	var lines []string
	add := func(name string, b, c interface{}) {
		if fmt.Sprint(b) != fmt.Sprint(c) {
			lines = append(lines, fmt.Sprintf("%s: %v   # was %v", name, c, b))
		}
	}
	add("buy_and_sell_multiplier", base.BuyAndSellMultiplier, champ.BuyAndSellMultiplier)
	add("cooldown_days", base.CooldownDays, champ.CooldownDays)
	add("baseline_sell_threshold", base.BaselineSellThreshold, champ.BaselineSellThreshold)
	add("ma_window", base.MAWindow, champ.MAWindow)
	add("buy_long_ma_window", base.BuyLongMAWindow, champ.BuyLongMAWindow)
	add("buy_require_above_long_ma", base.BuyRequireAboveLongMA, champ.BuyRequireAboveLongMA)
	add("buy_require_long_ma_slope_up", base.BuyRequireLongMASlopeUp, champ.BuyRequireLongMASlopeUp)
	add("buy_bias_min", base.BuyBiasMin, champ.BuyBiasMin)
	add("buy_rsi_period", base.BuyRSIPeriod, champ.BuyRSIPeriod)
	add("buy_rsi_max", base.BuyRSIMax, champ.BuyRSIMax)
	add("buy_confirm_up", base.BuyConfirmUp, champ.BuyConfirmUp)
	add("sell_rsi_period", base.SellRSIPeriod, champ.SellRSIPeriod)
	add("sell_rsi_min", base.SellRSIMin, champ.SellRSIMin)
	add("cash_floor_frac", base.CashFloorFrac, champ.CashFloorFrac)
	add("baseline_sell_amount", base.BaselineSellAmount, champ.BaselineSellAmount)
	add("buy_base_amount", base.BuyBaseAmount, champ.BuyBaseAmount)
	add("buy_tier_ratio", base.BuyTierRatio, champ.BuyTierRatio)
	if len(lines) == 0 {
		return "(無變動)"
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
