package main

// cmd/frontier 是「徹底定案」的聯合大掃描:在最佳固定核心 (peak 深度基準 + ma_pos 牛熊判定) 上,
// 同時掃 部署量(ma×mult×bull_band×bull_cd) × 熊市移動停利(trail_bear),
// 算出完整的「報酬 vs 回撤」效率前緣 (Pareto),並用 ASCII 圖畫出來。
//
// 固定核心 (前幾輪實驗已證明最佳):
//   BuyDepthBasis=peak, RegimeMethod=ma_pos(200), buy_tier_ratio=2.0, buy_base_amount=500,
//   sell_amount=10000, sell_rsi=75 (來自 config.yaml), trail_min_gain=0.20, trail_bull=0 (牛市不停利)。

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
	ma, bullcd            int
	mult, band, trailBear float64
}

type row struct {
	c                                   combo
	cagr, mdd, calmar, exp, skill, xirr float64
	pareto                              bool
}

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "baseline 設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	reportPath := flag.String("report", "docs/optimization/frontier-results.md", "報告輸出")
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

	eval := func(c combo) (row, bool) {
		cfg := cloneConfig(base)
		// 固定核心
		cfg.BuyDepthBasis = "peak"
		cfg.RegimeMethod = "ma_pos"
		cfg.RegimeMAWindow = 200
		cfg.BuyTierRatio = 2.0
		cfg.BuyBaseAmount = 500
		cfg.BaselineSellAmount = 10000
		cfg.TrailMinGain = 0.20
		cfg.TrailStopBull = 0
		// 掃描維度
		cfg.MAWindow = c.ma
		cfg.BuyAndSellMultiplier = c.mult
		cfg.BullBuyBand = c.band
		cfg.BullCooldownDays = c.bullcd
		cfg.TrailStopBear = c.trailBear
		_, agg, err := kernals.EvaluateWalkForward(cfg, series, wfp)
		if err != nil {
			return row{}, false
		}
		return row{c: c, cagr: agg.MedStratCAGR, mdd: agg.MedStratMDD, calmar: agg.MedStratCalmar,
			exp: agg.MedStratAvgExp, skill: agg.BlendSkillRate, xirr: agg.MedStratXIRR}, true
	}

	maGrid := []int{5, 10, 20}
	multGrid := []float64{2, 3, 4}
	bandGrid := []float64{0, 0.10, 0.25}
	bullcdGrid := []int{0, 5}
	trailGrid := []float64{0, 0.05, 0.08, 0.12, 0.20}
	total := len(maGrid) * len(multGrid) * len(bandGrid) * len(bullcdGrid) * len(trailGrid)

	var rows []row
	for _, ma := range maGrid {
		for _, mu := range multGrid {
			for _, bd := range bandGrid {
				for _, bc := range bullcdGrid {
					for _, tr := range trailGrid {
						if r, ok := eval(combo{ma: ma, bullcd: bc, mult: mu, band: bd, trailBear: tr}); ok {
							rows = append(rows, r)
						}
					}
				}
			}
		}
	}
	front := paretoFrontier(rows)
	sort.Slice(front, func(i, j int) bool { return math.Abs(front[i].mdd) < math.Abs(front[j].mdd) })

	// 參考點。
	var curCAGR, curMDD, bhCAGR, bhMDD float64
	{
		_, agg, _ := kernals.EvaluateWalkForward(cloneConfig(base), series, wfp)
		curCAGR, curMDD = agg.MedStratCAGR, agg.MedStratMDD
		bhCAGR, bhMDD = agg.MedBHCAGR, agg.MedBHMDD
	}

	logf("# 效率前緣 — regime × 部署量 × 熊市停利 聯合大掃描 (徹底定案)")
	logf("")
	logf("- 固定核心: basis=peak, regime=ma_pos(200), ratio=2.0, base=500, sell=10000, sell_rsi=75, trail_min_gain=0.20, trail_bull=0")
	logf("- 掃描: ma%v × mult%v × bull_band%v × bull_cd%v × trail_bear%v = **%d** 組",
		maGrid, multGrid, bandGrid, bullcdGrid, trailGrid, total)
	logf("- 參考: 現況 config.yaml CAGR %s / MaxDD %s | B&H CAGR %s / MaxDD %s",
		pct(curCAGR), pct(curMDD), pct(bhCAGR), pct(bhMDD))
	logf("")

	logf("## 效率前緣圖 (橫=最大回撤|MaxDD|, 縱=年化報酬CAGR;# 前緣, C 現況, B = B&H)")
	logf("```")
	logf("%s", plot(front, curCAGR, curMDD, bhCAGR, bhMDD))
	logf("```")
	logf("")

	logf("## 效率前緣 (依回撤由小到大) — 每個風險水位能拿到的最高報酬")
	logf("```")
	logf("%s", table(front))
	logf("```")
	logf("")

	// 各回撤上限下的最高 CAGR。
	logf("## 各回撤上限下的最佳配置")
	logf("")
	for _, cap := range []float64{0.15, 0.18, 0.20, 0.22, 0.25, 0.30} {
		b := bestUnderDD(rows, cap)
		if b == nil {
			continue
		}
		logf("- **|MaxDD| ≤ %.0f%%** → CAGR **%s**, MaxDD %s, Calmar %s, 利用率 %s, 真擇時 %s  〔`%s`〕",
			cap*100, pct(b.cagr), pct(b.mdd), ratio2(b.calmar), pct(b.exp), pct(b.skill), comboStr(b.c))
	}
	logf("")
	maxCal := bestBy(rows, func(r row) bool { return true }, func(a, b row) bool { return a.calmar > b.calmar })
	if maxCal != nil {
		logf("- **最高 Calmar (CP值)** → CAGR %s, MaxDD %s, Calmar **%s**, 利用率 %s 〔`%s`〕",
			pct(maxCal.cagr), pct(maxCal.mdd), ratio2(maxCal.calmar), pct(maxCal.exp), comboStr(maxCal.c))
	}

	if err := os.WriteFile(*reportPath, []byte(rep.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 寫報告失敗:", err)
	} else {
		fmt.Fprintln(os.Stderr, "報告已寫入:", *reportPath)
	}
}

func dominates(a, b row) bool {
	ge := a.cagr >= b.cagr-1e-9 && math.Abs(a.mdd) <= math.Abs(b.mdd)+1e-9
	gt := a.cagr > b.cagr+1e-9 || math.Abs(a.mdd) < math.Abs(b.mdd)-1e-9
	return ge && gt
}

func paretoFrontier(rows []row) []row {
	var f []row
	for i := range rows {
		if math.IsNaN(rows[i].cagr) {
			continue
		}
		dom := false
		for j := range rows {
			if i != j && dominates(rows[j], rows[i]) {
				dom = true
				break
			}
		}
		if !dom {
			rows[i].pareto = true
			f = append(f, rows[i])
		}
	}
	return f
}

func bestUnderDD(rows []row, cap float64) *row {
	return bestBy(rows, func(r row) bool { return math.Abs(r.mdd) <= cap }, func(a, b row) bool { return a.cagr > b.cagr })
}

func bestBy(rows []row, ok func(row) bool, better func(a, b row) bool) *row {
	var best *row
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

func comboStr(c combo) string {
	return fmt.Sprintf("ma=%d mult=%.0f bull_band=%.2f bull_cd=%d trail_bear=%.2f", c.ma, c.mult, c.band, c.bullcd, c.trailBear)
}

// plot 畫 ASCII 散點:x=|MaxDD| 0..36%, y=CAGR 0..28%。
func plot(front []row, curCAGR, curMDD, bhCAGR, bhMDD float64) string {
	const rows, cols = 16, 48
	const yMax, xMax = 0.28, 0.36
	grid := make([][]byte, rows)
	for r := range grid {
		grid[r] = make([]byte, cols)
		for c := range grid[r] {
			grid[r][c] = ' '
		}
	}
	put := func(cagr, mdd float64, ch byte) {
		if math.IsNaN(cagr) {
			return
		}
		rr := int((yMax - cagr) / yMax * float64(rows-1))
		cc := int(math.Abs(mdd) / xMax * float64(cols-1))
		if rr < 0 {
			rr = 0
		} else if rr >= rows {
			rr = rows - 1
		}
		if cc < 0 {
			cc = 0
		} else if cc >= cols {
			cc = cols - 1
		}
		grid[rr][cc] = ch
	}
	for _, p := range front {
		put(p.cagr, p.mdd, '#')
	}
	put(curCAGR, curMDD, 'C')
	put(bhCAGR, bhMDD, 'B')

	var b strings.Builder
	for r := 0; r < rows; r++ {
		yLabel := yMax * (1 - float64(r)/float64(rows-1))
		fmt.Fprintf(&b, "%4.0f%% |%s\n", yLabel*100, string(grid[r]))
	}
	b.WriteString("       +")
	b.WriteString(strings.Repeat("-", cols))
	b.WriteString("\n        0")
	b.WriteString(strings.Repeat(" ", cols/2-4))
	fmt.Fprintf(&b, "%.0f%%", xMax*100/2)
	b.WriteString(strings.Repeat(" ", cols/2-5))
	fmt.Fprintf(&b, "%.0f%% |MaxDD|", xMax*100)
	return b.String()
}

func table(rows []row) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "CAGR\tMaxDD\tCalmar\t利用率\t真擇時\tXIRR\t配置")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pct(r.cagr), pct(r.mdd), ratio2(r.calmar), pct(r.exp), pct(r.skill), pct(r.xirr), comboStr(r.c))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
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
