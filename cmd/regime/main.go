package main

// cmd/regime 實驗:牛熊 regime 感知 + 更好的「加碼深度基準」是否能在「不犧牲風險」下
// 提高資金利用率 (尤其是多頭時別空抱現金)。
//
// 兩個被測概念:
//   1. 牛市別空手:bull regime 放寬進場 (今價<均線×(1+band) 才買,甚至站上均線一點也買) + 較短冷卻。
//   2. 熊市維持深跌加碼 (alpha 來源)。
//   3. 加碼金額「深度基準」:held_high(原始,相對自己成本) vs ma(乖離) vs peak(距高點回撤)。
//
// 部署量旋鈕沿用 config.yaml 現值,只變動 regime/basis/bull,以隔離 regime 感知本身的效果。

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
	basis  string
	regime string
	band   float64
	bullcd int
}

type res struct {
	s                                   scen
	cagr, mdd, calmar, exp, skill, xirr float64
	pareto                              bool
}

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "baseline 設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	reportPath := flag.String("report", "docs/optimization/regime-results.md", "報告輸出")
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
		c.BuyDepthBasis = s.basis
		c.RegimeMethod = s.regime
		c.RegimeMAWindow = 200
		c.BullBuyBand = s.band
		c.BullCooldownDays = s.bullcd
		_, agg, err := kernals.EvaluateWalkForward(c, series, wfp)
		if err != nil {
			return res{}, false
		}
		return res{s: s, cagr: agg.MedStratCAGR, mdd: agg.MedStratMDD, calmar: agg.MedStratCalmar,
			exp: agg.MedStratAvgExp, skill: agg.BlendSkillRate, xirr: agg.MedStratXIRR}, true
	}

	// 列舉情境:basis × regime × bull設定。regime=off 時只取 band=0/cd=0 (其餘無意義)。
	bases := []string{"held_high", "ma", "peak"}
	regimes := []string{"", "ma_pos", "ma_slope", "mom"}
	bands := []float64{0.05, 0.10, 0.25}
	bullcds := []int{0, 5}

	var rows []res
	for _, ba := range bases {
		for _, rg := range regimes {
			if rg == "" {
				if r, ok := eval(scen{basis: ba, regime: "", band: 0, bullcd: 0}); ok {
					rows = append(rows, r)
				}
				continue
			}
			for _, bd := range bands {
				for _, cd := range bullcds {
					if r, ok := eval(scen{basis: ba, regime: rg, band: bd, bullcd: cd}); ok {
						rows = append(rows, r)
					}
				}
			}
		}
	}

	markPareto(rows)

	// 參考。
	var bhCAGR, bhMDD float64
	{
		_, agg, _ := kernals.EvaluateWalkForward(cloneConfig(base), series, wfp)
		bhCAGR, bhMDD = agg.MedBHCAGR, agg.MedBHMDD
	}

	logf("# 牛熊 regime + 加碼深度基準 實驗")
	logf("")
	logf("- 部署量沿用 config.yaml 現值 (ma=20, ratio=2.0, base=500, mult=2, sell=10000, cd=14, sell_rsi=75)")
	logf("- 只變動:加碼深度基準 × 牛熊判定法 × 牛市進場放寬(band)/冷卻;共 %d 種情境", len(rows))
	logf("- 目前 config.yaml: CAGR +11.9%% | MaxDD -15.3%% | Calmar 1.10 | 曝險 39.8%% | 真擇時 57%%")
	logf("- Buy&Hold: CAGR %s | MaxDD %s", pct(bhCAGR), pct(bhMDD))
	logf("")

	// 依曝險 (資金利用率) 升冪排序,* = Pareto 前緣 (報酬↑/風險↓/利用率↑ 不被支配)。
	sort.Slice(rows, func(i, j int) bool { return rows[i].exp < rows[j].exp })
	logf("## 全情境 (依資金利用率升冪;* = Pareto 前緣)")
	logf("```")
	logf("%s", table(rows))
	logf("```")
	logf("")

	// Exp A:三種深度基準 (regime 關閉) 直接比較。
	logf("## 實驗A:加碼深度基準 (regime 關閉,其餘同現況)")
	logf("```")
	logf("%s", basisTable(rows))
	logf("```")
	logf("")

	// 具名建議。
	logf("## 具名最佳")
	logf("")
	maxCalmar := pick(rows, func(r res) bool { return true }, func(a, b res) bool { return a.calmar > b.calmar })
	highUtilGoodCP := pick(rows, func(r res) bool { return r.exp >= 0.55 && r.calmar >= 1.0 && math.Abs(r.mdd) <= 0.25 },
		func(a, b res) bool { return a.exp > b.exp })
	maxRetCtrl := pick(rows, func(r res) bool { return math.Abs(r.mdd) <= 0.22 }, func(a, b res) bool { return a.cagr > b.cagr })
	printPick(logf, "最高 Calmar (CP值)", maxCalmar, bhCAGR)
	printPick(logf, "高利用率仍保 CP≥1.0 (曝險≥55% 且 Calmar≥1.0 且 |MDD|≤25%, 取曝險最高)", highUtilGoodCP, bhCAGR)
	printPick(logf, "控風險衝報酬 (|MDD|≤22% 取 CAGR 最高)", maxRetCtrl, bhCAGR)

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
			ge := a.cagr >= b.cagr-1e-9 && math.Abs(a.mdd) <= math.Abs(b.mdd)+1e-9 && a.exp >= b.exp-1e-9
			gt := a.cagr > b.cagr+1e-9 || math.Abs(a.mdd) < math.Abs(b.mdd)-1e-9 || a.exp > b.exp+1e-9
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
	rg := s.regime
	if rg == "" {
		rg = "off"
	}
	return fmt.Sprintf("basis=%-9s regime=%-8s band=%.2f bullcd=%d", s.basis, rg, s.band, s.bullcd)
}

func table(rows []res) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "P\tCAGR\tMaxDD\tCalmar\t曝險\t真擇時\tXIRR\t情境")
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

func basisTable(rows []res) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "深度基準\tCAGR\tMaxDD\tCalmar\t曝險\t真擇時")
	for _, ba := range []string{"held_high", "ma", "peak"} {
		for _, r := range rows {
			if r.s.basis == ba && r.s.regime == "" {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", ba,
					pct(r.cagr), pct(r.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill))
			}
		}
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func printPick(logf func(string, ...interface{}), title string, r *res, bhCAGR float64) {
	if r == nil {
		logf("### %s\n（無符合條件）\n", title)
		return
	}
	logf("### %s", title)
	logf("- CAGR **%s**（B&H %s）| MaxDD **%s** | Calmar **%s** | 資金利用率 **%s** | 真擇時 %s",
		pct(r.cagr), pct(bhCAGR), pct(r.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill))
	logf("- 情境:`%s`", scenStr(r.s))
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
