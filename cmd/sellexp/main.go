package main

// cmd/sellexp 在「策略 C」(高利用率 regime 配方) 之上,廣泛探索**賣出端**的 regime 切換,
// 重點是「賣點好壞如何影響回撤 (MaxDD)」。
//
// 策略 C 基底 (疊在 config.yaml 之上):
//   BuyDepthBasis=peak, RegimeMethod=ma_pos(200), BullBuyBand=0.25, BullCooldownDays=5
// 掃描賣出旋鈕:
//   TrailStopBear (熊市移動停利,保護式全出) × TrailStopBull (牛市移動停利,通常關或鬆)
//   × SellThresholdBear (熊市提早鎖利) × SellThresholdBull (牛市讓贏家多跑)
// TrailMinGain 固定 0.20 (僅保護已獲利部位,不停損逢低買進)。

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

type scen struct {
	trailBear, trailBull float64
	thrBear, thrBull     float64
}

type res struct {
	s                                   scen
	cagr, mdd, calmar, exp, skill, xirr float64
	pareto                              bool
}

// applyC 把「策略 C」高利用率 regime 配方疊到 config 副本上。
func applyC(c *config.Config) {
	c.BuyDepthBasis = "peak"
	c.RegimeMethod = "ma_pos"
	c.RegimeMAWindow = 200
	c.BullBuyBand = 0.25
	c.BullCooldownDays = 5
	c.TrailMinGain = 0.20
}

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "baseline 設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	reportPath := flag.String("report", "docs/optimization/sell-results.md", "報告輸出")
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

	var rep strings.Builder
	logf := func(f string, a ...interface{}) {
		s := fmt.Sprintf(f, a...)
		fmt.Println(s)
		rep.WriteString(s + "\n")
	}

	eval := func(s scen) (res, bool) {
		c := cloneConfig(base)
		applyC(c)
		c.TrailStopBear = s.trailBear
		c.TrailStopBull = s.trailBull
		c.SellThresholdBear = s.thrBear
		c.SellThresholdBull = s.thrBull
		_, agg, err := kernals.EvaluateWalkForward(c, series, wfp)
		if err != nil {
			return res{}, false
		}
		return res{s: s, cagr: agg.MedStratCAGR, mdd: agg.MedStratMDD, calmar: agg.MedStratCalmar,
			exp: agg.MedStratAvgExp, skill: agg.BlendSkillRate, xirr: agg.MedStratXIRR}, true
	}

	trailBears := []float64{0, 0.06, 0.10, 0.15, 0.25}
	trailBulls := []float64{0, 0.25}
	thrBears := []float64{0, 0.3, 0.5, 0.7}
	thrBulls := []float64{0, 1.5}

	var rows []res
	for _, tb := range trailBears {
		for _, tu := range trailBulls {
			for _, hb := range thrBears {
				for _, hu := range thrBulls {
					if r, ok := eval(scen{trailBear: tb, trailBull: tu, thrBear: hb, thrBull: hu}); ok {
						rows = append(rows, r)
					}
				}
			}
		}
	}
	markPareto(rows)

	// 參考:C baseline (賣出端不動) 與 B&H。
	cbase, _ := eval(scen{})
	var bhCAGR, bhMDD float64
	{
		_, agg, _ := kernals.EvaluateWalkForward(cloneConfig(base), series, wfp)
		bhCAGR, bhMDD = agg.MedBHCAGR, agg.MedBHMDD
	}

	logf("# 賣出端 regime 切換實驗 (在策略 C 之上;聚焦回撤)")
	logf("")
	logf("- 基底=策略C:basis=peak, regime=ma_pos(200), bull_band=0.25, bull_cd=5, trail_min_gain=0.20")
	logf("- 掃描:trail_bear × trail_bull × sell_thr_bear × sell_thr_bull;共 %d 種", len(rows))
	logf("- **C baseline (賣出端不動)**: CAGR %s | MaxDD %s | Calmar %s | 曝險 %s | 真擇時 %s",
		pct(cbase.cagr), pct(cbase.mdd), ratio2(cbase.calmar), pct(cbase.exp), pct(cbase.skill))
	logf("- 目前 config.yaml(低利用率): CAGR +11.9%% | MaxDD -15.3%% | Calmar 1.10 | 曝險 39.8%%")
	logf("- Buy&Hold: CAGR %s | MaxDD %s", pct(bhCAGR), pct(bhMDD))
	logf("")

	// 依 |MaxDD| 升冪 (回撤最小在前)。
	sort.Slice(rows, func(i, j int) bool { return math.Abs(rows[i].mdd) < math.Abs(rows[j].mdd) })
	logf("## 全情境 (依回撤由小到大;* = Pareto 報酬↑/回撤↓)")
	logf("```")
	logf("%s", table(rows))
	logf("```")
	logf("")

	logf("## 具名最佳")
	logf("")
	minDD := pick(rows, func(r res) bool { return r.cagr >= 0.15 }, func(a, b res) bool { return math.Abs(a.mdd) < math.Abs(b.mdd) })
	maxCalmar := pick(rows, func(r res) bool { return true }, func(a, b res) bool { return a.calmar > b.calmar })
	balanced := pick(rows, func(r res) bool { return math.Abs(r.mdd) <= 0.20 }, func(a, b res) bool { return a.cagr > b.cagr })
	printPick(logf, "最低回撤 (CAGR≥15% 中 |MaxDD| 最小)", minDD, cbase, bhCAGR)
	printPick(logf, "最佳 CP 值 (Calmar 最高)", maxCalmar, cbase, bhCAGR)
	printPick(logf, "控回撤衝報酬 (|MaxDD|≤20% 中 CAGR 最高)", balanced, cbase, bhCAGR)

	if err := os.WriteFile(*reportPath, []byte(rep.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 寫報告失敗:", err)
	} else {
		fmt.Fprintln(os.Stderr, "報告已寫入:", *reportPath)
	}
}

func markPareto(rows []res) {
	for i := range rows {
		dom := false
		for j := range rows {
			if i == j {
				continue
			}
			a, b := rows[j], rows[i]
			ge := a.cagr >= b.cagr-1e-9 && math.Abs(a.mdd) <= math.Abs(b.mdd)+1e-9
			gt := a.cagr > b.cagr+1e-9 || math.Abs(a.mdd) < math.Abs(b.mdd)-1e-9
			if ge && gt {
				dom = true
				break
			}
		}
		rows[i].pareto = !dom
	}
}

func pick(rows []res, ok func(res) bool, better func(a, b res) bool) *res {
	var best *res
	for i := range rows {
		r := rows[i]
		if !ok(r) || math.IsNaN(r.calmar) || math.IsInf(r.calmar, 0) {
			continue
		}
		if best == nil || better(r, *best) {
			rr := r
			best = &rr
		}
	}
	return best
}

func scenStr(s scen) string {
	return fmt.Sprintf("trail_bear=%.2f trail_bull=%.2f thr_bear=%s thr_bull=%s",
		s.trailBear, s.trailBull, thrStr(s.thrBear), thrStr(s.thrBull))
}

func thrStr(v float64) string {
	if v <= 0 {
		return "1.0*"
	}
	return fmt.Sprintf("%.1f", v)
}

func table(rows []res) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "P\tCAGR\tMaxDD\tCalmar\t曝險\t真擇時\tXIRR\t賣出情境")
	for _, r := range rows {
		p := " "
		if r.pareto {
			p = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p,
			pct(r.cagr), pct(r.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill), pct(r.xirr), scenStr(r.s))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func printPick(logf func(string, ...interface{}), title string, r *res, cbase res, bhCAGR float64) {
	if r == nil {
		logf("### %s\n（無符合條件）\n", title)
		return
	}
	logf("### %s", title)
	logf("- CAGR **%s**（C %s / B&H %s）| MaxDD **%s**（C %s）| Calmar **%s** | 曝險 %s | 真擇時 %s",
		pct(r.cagr), pct(cbase.cagr), pct(bhCAGR), pct(r.mdd), pct(cbase.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill))
	logf("- 賣出設定:`%s`", scenStr(r.s))
	logf("")
}

func pct(x float64) string {
	if math.IsNaN(x) {
		return "n/a"
	}
	if math.IsInf(x, 0) {
		return "inf"
	}
	return fmt.Sprintf("%+.1f%%", x*100)
}

func ratio2(x float64) string {
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

func cloneConfig(c *config.Config) *config.Config {
	cp := *c
	cp.TrackStocks = append([]string(nil), c.TrackStocks...)
	cp.BaselineBuyTiers = append([]config.BaselineBuyTier(nil), c.BaselineBuyTiers...)
	return &cp
}
