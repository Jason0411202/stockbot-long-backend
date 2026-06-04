package main

// cmd/gridsearch 對「部署量相關旋鈕」做**聯合**笛卡兒積掃描 (coordinate-ascent 抓不到的組合),
// 目標:找出「總報酬高、風險低、資金利用率高 (平均曝險高)」的最佳組合,直接對治
// 「策略過多時間抱現金、資金利用率不夠」的問題。
//
// 掃描維度:ma_window × multiplier × buy_tier_ratio × buy_base_amount × sell_amount × cooldown_days。
// 其餘旗標沿用 config.yaml (含已採納的 sell_rsi)。
//
// 輸出:
//   1. Pareto 前緣 (在 報酬↑ / 回撤↓ / 曝險↑ 三軸上不被支配的組合) — 這就是「報酬/風險/利用率」的取捨選單。
//   2. 各曝險帶 (40-50/50-60/60-70/70%+) 內 Calmar 最佳的組合 — 看「每個利用率水位能拿到多少 CP 值」。
//   3. 幾個具名建議 (高利用率最佳CP值 / 平衡 / 衝報酬控風險) + 對應 config 覆寫。

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

type combo struct {
	ma, cd                  int
	mult, ratio, base, sell float64
}

type row struct {
	c                                   combo
	cagr, mdd, calmar, exp, skill, xirr float64
	part                                float64
}

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "baseline 設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	reportPath := flag.String("report", "docs/optimization/gridsearch-results.md", "報告輸出")
	expFloor := flag.Float64("exp-floor", 0.60, "「高利用率」建議的最低平均曝險")
	ddCap := flag.Float64("dd-cap", 0.25, "「低風險」建議的最大可接受 |MaxDD|")
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

	// 掃描網格 (刻意精簡以控制組合數;涵蓋從保守到積極部署的光譜)。
	maGrid := []int{5, 10, 20}
	multGrid := []float64{2, 3, 4}
	ratioGrid := []float64{1.5, 2.0, 2.5}
	baseGrid := []float64{500, 800, 1200}
	sellGrid := []float64{10000, 20000}
	cdGrid := []int{7, 14}

	var rep strings.Builder
	logf := func(f string, a ...interface{}) {
		s := fmt.Sprintf(f, a...)
		fmt.Println(s)
		rep.WriteString(s + "\n")
	}

	eval := func(c combo) (row, bool) {
		cfg := cloneConfig(base)
		cfg.MAWindow = c.ma
		cfg.CooldownDays = c.cd
		cfg.BuyAndSellMultiplier = c.mult
		cfg.BuyTierRatio = c.ratio
		cfg.BuyBaseAmount = c.base
		cfg.BaselineSellAmount = c.sell
		_, agg, err := kernals.EvaluateWalkForward(cfg, series, wfp)
		if err != nil {
			return row{}, false
		}
		return row{
			c: c, cagr: agg.MedStratCAGR, mdd: agg.MedStratMDD, calmar: agg.MedStratCalmar,
			exp: agg.MedStratAvgExp, skill: agg.BlendSkillRate, xirr: agg.MedStratXIRR,
			part: agg.MedRetParticipation,
		}, true
	}

	total := len(maGrid) * len(multGrid) * len(ratioGrid) * len(baseGrid) * len(sellGrid) * len(cdGrid)
	logf("# 聯合網格掃描 — 找「高報酬 / 低風險 / 高資金利用率」最佳組合")
	logf("")
	logf("- 資料 %v;視窗 %d 月 / 步進 %d 月 / 21 視窗;共 **%d** 種組合", base.TrackStocks, *window, *step, total)
	logf("- 掃描維度:ma_window%v × multiplier%v × buy_tier_ratio%v × buy_base_amount%v × sell_amount%v × cooldown%v",
		maGrid, multGrid, ratioGrid, baseGrid, sellGrid, cdGrid)
	logf("- 資金利用率 = 平均持股佔比 (曝險);越高代表現金越少閒置")
	logf("")

	// baseline (目前 config.yaml) 參考。
	var bhCAGR, bhMDD float64
	{
		_, agg, _ := kernals.EvaluateWalkForward(cloneConfig(base), series, wfp)
		bhCAGR, bhMDD = agg.MedBHCAGR, agg.MedBHMDD
		logf("目前 config.yaml: CAGR %s | MaxDD %s | Calmar %s | 曝險 %s | 真擇時 %s",
			pct(agg.MedStratCAGR), pct(agg.MedStratMDD), ratio2(agg.MedStratCalmar), pct(agg.MedStratAvgExp), pct(agg.BlendSkillRate))
		logf("Buy&Hold 參考: CAGR %s | MaxDD %s", pct(bhCAGR), pct(bhMDD))
		logf("")
	}

	rows := make([]row, 0, total)
	for _, ma := range maGrid {
		for _, mult := range multGrid {
			for _, r := range ratioGrid {
				for _, b := range baseGrid {
					for _, s := range sellGrid {
						for _, cd := range cdGrid {
							if rr, ok := eval(combo{ma: ma, cd: cd, mult: mult, ratio: r, base: b, sell: s}); ok {
								rows = append(rows, rr)
							}
						}
					}
				}
			}
		}
	}

	// ── Pareto 前緣:報酬↑ / |回撤|↓ / 曝險↑ 三軸不被支配 ──
	front := paretoFrontier(rows)
	sort.Slice(front, func(i, j int) bool { return front[i].exp < front[j].exp })
	logf("## Pareto 前緣 (報酬↑ / 風險↓ / 利用率↑ 三者不被任何組合同時打敗) — 共 %d 組", len(front))
	logf("```")
	logf("%s", table(front))
	logf("```")
	logf("")

	// ── 各曝險帶最佳 Calmar ──
	logf("## 各「資金利用率帶」內 CP 值 (Calmar) 最佳組合")
	logf("```")
	logf("%s", bandTable(rows))
	logf("```")
	logf("")

	// ── 具名建議 ──
	logf("## 具名建議")
	logf("")
	recHighUtil := bestBy(rows, func(r row) bool { return r.exp >= *expFloor && math.Abs(r.mdd) <= *ddCap }, byCalmar)
	recBalanced := bestBy(rows, func(r row) bool { return r.exp >= 0.50 && math.Abs(r.mdd) <= 0.22 }, byCalmar)
	recReturn := bestBy(rows, func(r row) bool { return math.Abs(r.mdd) <= *ddCap }, byCAGR)
	recMaxCalmar := bestBy(rows, func(r row) bool { return true }, byCalmar)

	printRec(logf, "① 高利用率·最佳CP值", fmt.Sprintf("曝險≥%.0f%% 且 |MaxDD|≤%.0f%% 中 Calmar 最高", *expFloor*100, *ddCap*100), recHighUtil, bhCAGR)
	printRec(logf, "② 平衡", "曝險≥50% 且 |MaxDD|≤22% 中 Calmar 最高", recBalanced, bhCAGR)
	printRec(logf, "③ 衝報酬·控風險", fmt.Sprintf("|MaxDD|≤%.0f%% 中 CAGR 最高", *ddCap*100), recReturn, bhCAGR)
	printRec(logf, "④ 無約束最高 Calmar (對照)", "全體 Calmar 最高", recMaxCalmar, bhCAGR)

	if err := os.WriteFile(*reportPath, []byte(rep.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 寫報告失敗:", err)
	} else {
		fmt.Fprintln(os.Stderr, "報告已寫入:", *reportPath)
	}
}

// ── Pareto / 選擇 ──

func dominates(a, b row) bool {
	// a 支配 b:三軸皆不差且至少一軸更好 (報酬↑、|回撤|↓、曝險↑)。
	ge := a.cagr >= b.cagr-1e-9 && math.Abs(a.mdd) <= math.Abs(b.mdd)+1e-9 && a.exp >= b.exp-1e-9
	gt := a.cagr > b.cagr+1e-9 || math.Abs(a.mdd) < math.Abs(b.mdd)-1e-9 || a.exp > b.exp+1e-9
	return ge && gt
}

func paretoFrontier(rows []row) []row {
	var f []row
	for i, r := range rows {
		dominated := false
		for j, o := range rows {
			if i == j {
				continue
			}
			if dominates(o, r) {
				dominated = true
				break
			}
		}
		if !dominated {
			f = append(f, r)
		}
	}
	return f
}

func byCalmar(a, b row) bool { return a.calmar > b.calmar }
func byCAGR(a, b row) bool   { return a.cagr > b.cagr }

func bestBy(rows []row, ok func(row) bool, less func(a, b row) bool) *row {
	var best *row
	for i := range rows {
		r := rows[i]
		if !ok(r) || math.IsNaN(r.calmar) || math.IsInf(r.calmar, 0) {
			continue
		}
		if best == nil || less(r, *best) {
			rr := r
			best = &rr
		}
	}
	return best
}

// bandTable 在每個曝險帶內挑 Calmar 最高的組合。
func bandTable(rows []row) string {
	bands := []struct {
		lo, hi float64
		name   string
	}{
		{0.0, 0.5, "40-50%"}, {0.5, 0.6, "50-60%"}, {0.6, 0.7, "60-70%"}, {0.7, 1.01, "70%+"},
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "曝險帶\tCAGR\tMaxDD\tCalmar\t曝險\t真擇時\t組合(ma/mult/ratio/base/sell/cd)")
	for _, bd := range bands {
		best := bestBy(rows, func(r row) bool { return r.exp >= bd.lo && r.exp < bd.hi }, byCalmar)
		if best == nil {
			fmt.Fprintf(w, "%s\t(無)\n", bd.name)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", bd.name,
			pct(best.cagr), pct(best.mdd), ratio2(best.calmar), pct(best.exp), pct(best.skill), comboStr(best.c))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func table(rows []row) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "CAGR\tMaxDD\tCalmar\t曝險\t真擇時\tXIRR\t組合(ma/mult/ratio/base/sell/cd)")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pct(r.cagr), pct(r.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill), pct(r.xirr), comboStr(r.c))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func printRec(logf func(string, ...interface{}), title, crit string, r *row, bhCAGR float64) {
	logf("### %s", title)
	if r == nil {
		logf("（無符合條件的組合）")
		logf("")
		return
	}
	logf("- 條件:%s", crit)
	logf("- 結果:CAGR **%s**（B&H %s）| MaxDD **%s** | Calmar **%s** | 資金利用率(曝險) **%s** | 真擇時 %s",
		pct(r.cagr), pct(bhCAGR), pct(r.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill))
	logf("- 組合:`%s`", comboStr(r.c))
	logf("")
}

func comboStr(c combo) string {
	return fmt.Sprintf("ma=%d mult=%.1f ratio=%.2f base=%.0f sell=%.0f cd=%d", c.ma, c.mult, c.ratio, c.base, c.sell, c.cd)
}

// ── 格式化 / 複製 ──

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
