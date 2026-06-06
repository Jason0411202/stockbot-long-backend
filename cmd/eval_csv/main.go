package main

// cmd/eval_csv 用本機 CSV 快取 (由 cmd/fetch_data 產生) 對 config.yaml 跑「每月定期定額注資」問題設定下的
// 評估,印出策略 vs B&H(立刻買滿) vs 同曝險 Blend 的 scorecard。完全不依賴 MariaDB / docker。
//
// 兩段輸出:
//  1) Headline — 全期連續回測 (注資動態最明顯):資金加權報酬 (XIRR/MWR)、NAV 真實回撤、資金利用率、現金尾巴。
//  2) Walk-forward 穩健性 — 多視窗中位數 + 五道關卡 (守住 B&H 七成報酬 + 更小回撤 + Calmar 穩定贏 + 真擇時)。
//
// 用途:驗證/重現目前策略表現。先跑 `go run ./cmd/fetch_data` 抓資料,再跑本工具。
import (
	"flag"
	"fmt"
	"math"
	"os"

	"github.com/Jason0411202/stockbot-long-backend/config"
	"github.com/Jason0411202/stockbot-long-backend/kernals"
)

func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	isMonths := flag.Int("is-months", 36, "滾動 OOS:初始樣本內 (IS) 錨定月數")
	foldMonths := flag.Int("fold-months", 12, "滾動 OOS:每折月數")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 config 失敗:", err)
		os.Exit(1)
	}
	series, err := kernals.LoadSeriesFromCSV(*dataDir, cfg.TrackStocks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 CSV 失敗 (先跑 go run ./cmd/fetch_data):", err)
		os.Exit(1)
	}

	full, err := kernals.EvaluateFullSpan(cfg, series)
	if err != nil {
		fmt.Fprintln(os.Stderr, "全期評估失敗:", err)
		os.Exit(1)
	}
	wfp := kernals.WalkForwardParams{WindowMonths: *window, StepMonths: *step, MinTradeDays: *minDays}
	_, agg, err := kernals.EvaluateWalkForward(cfg, series, wfp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk-forward 評估失敗:", err)
		os.Exit(1)
	}
	isoos, err := kernals.EvaluateRollingOOS(cfg, series, wfp, *isMonths, *foldMonths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "滾動 OOS 評估失敗:", err)
		os.Exit(1)
	}

	printHeadline(cfg, full)
	printWalkForward(cfg, *window, *step, agg)
	printRollingOOS(isoos)
}

// printRollingOOS 印出滾動式 walk-forward 樣本外驗證 (反過擬合)。
func printRollingOOS(r kernals.RollingOOSReport) {
	fmt.Println()
	fmt.Printf("滾動 walk-forward OOS (初始 IS 錨定 %d 月;分界日 %s,其後每窗皆樣本外)\n",
		r.ISMonths, r.Anchor.Format("2006-01-02"))
	fmt.Printf("IS %d 視窗 | OOS %d 視窗 (依 %d 月分 %d 折)\n", r.NIS, r.NOOS, r.FoldMonths, len(r.Folds))
	fmt.Println("──────────────────────────────────────────────")
	fmt.Printf("%-16s %12s %12s\n", "", "IS(樣本內)", "OOS(全部)")
	row2("中位 MWR", pct(r.IS.MedStratMWR), pct(r.OOS.MedStratMWR))
	row2("中位 MaxDD", pct(r.IS.MedStratMDD), pct(r.OOS.MedStratMDD))
	row2("中位 Calmar", ratio(r.IS.MedStratCalmar), ratio(r.OOS.MedStratCalmar))
	row2("Calmar 勝率", pct(r.IS.CalmarWinRate), pct(r.OOS.CalmarWinRate))
	row2("五道關卡(G1~5)", gateStr(r.IS), gateStr(r.OOS))
	fmt.Println("── 每折 OOS (跨期一致性) ──")
	minFold := math.Inf(1)
	for _, f := range r.Folds {
		fmt.Printf("  %s 起 (%d 窗): Calmar %s  勝率 %s  關卡 %d/5\n",
			f.FirstStart.Format("2006-01"), f.N, ratio(f.Calmar), pct(f.CalmarWin), f.GatesPass)
		if !math.IsNaN(f.Calmar) && !math.IsInf(f.Calmar, 0) && f.Calmar < minFold {
			minFold = f.Calmar
		}
	}
	fmt.Println("──────────────────────────────────────────────")
	retain := r.OOS.MedStratCalmar / r.IS.MedStratCalmar
	verdict := "穩健 ✅ (OOS 接近 IS、各折皆不崩)"
	if r.IS.MedStratCalmar <= 0 || r.OOS.MedStratCalmar < 0.6*r.IS.MedStratCalmar || minFold < 0.6 {
		verdict = "過擬合疑慮 ⚠️ (OOS 或某折大幅退化)"
	} else if r.OOS.MedStratCalmar < 0.8*r.IS.MedStratCalmar {
		verdict = "尚可,略退化"
	}
	fmt.Printf("判讀:OOS Calmar 為 IS 的 %.0f%%、最差折 Calmar %s → %s\n", retain*100, ratio(minFold), verdict)
}

func row2(label, a, b string) { fmt.Printf("%-16s %12s %12s\n", label, a, b) }

func gateStr(a kernals.AggregateReport) string {
	n := 0
	for _, g := range []bool{a.G1RetParticipation, a.G2RiskReduction, a.G3CalmarVsBH, a.G4Skill, a.G5Robustness} {
		if g {
			n++
		}
	}
	return fmt.Sprintf("%d/5", n)
}

func printHeadline(cfg *config.Config, r kernals.WindowReport) {
	fmt.Printf("問題設定:期初 %s + 每月解鎖 %s,標的 %v\n",
		money(cfg.InitialCash), money(cfg.MonthlyContribution), cfg.TrackStocks)
	fmt.Printf("全期連續回測 %s ~ %s (%.1f 年);投入本金合計 %s\n",
		r.Start.Format("2006-01-02"), r.End.Format("2006-01-02"), r.Years, money(r.TotalIn))
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("%-20s %16s %16s\n", "", "B&H(立刻買)", "Ours(策略)")
	fmt.Println("──────────────────────────────────────────────────────────────")
	row("期末總權益", money(r.TotalIn*r.BH.Multiple), money(r.TotalIn*r.Strat.Multiple))
	row("期末閒置現金", "~$0", money(r.StratFinalCash))
	row("本金倍數", fmt.Sprintf("%.2fx", r.BH.Multiple), fmt.Sprintf("%.2fx", r.Strat.Multiple))
	row("資金加權報酬MWR", pct(r.BH.MWR), pct(r.Strat.MWR))
	row("最大回撤(NAV)", pct(r.BH.MaxDD), pct(r.Strat.MaxDD))
	row("Calmar(MWR/|DD|)", ratio(r.BH.Calmar), ratio(r.Strat.Calmar))
	row("資金利用率", pct(r.BH.AvgExp), pct(r.Strat.AvgExp))
	row("買/賣次數(停利/了結)", fmt.Sprintf("%d/0", r.BHBuys),
		fmt.Sprintf("%d/%d(%d/%d)", r.Buys, r.TrailSells+r.ProfitSells, r.TrailSells, r.ProfitSells))
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()
}

func printWalkForward(cfg *config.Config, window, step int, agg kernals.AggregateReport) {
	fmt.Printf("Walk-forward 穩健性 — 視窗 %d 月 / 步進 %d 月 / %d 個視窗 (每窗獨立注資)\n", window, step, agg.NWindows)
	fmt.Println("──────────────────────────────────────────────")
	fmt.Printf("中位 MWR      策略 %s   vs  B&H %s   (Blend %s)\n", pct(agg.MedStratMWR), pct(agg.MedBHMWR), pct(agg.MedBlendMWR))
	fmt.Printf("中位 MaxDD    策略 %s   vs  B&H %s\n", pct(agg.MedStratMDD), pct(agg.MedBHMDD))
	fmt.Printf("中位 Calmar   策略 %s      vs  B&H %s\n", ratio(agg.MedStratCalmar), ratio(agg.MedBHCalmar))
	fmt.Printf("資金利用率    %s   |   報酬參與率 %s\n", pct(agg.MedStratAvgExp), ratio(agg.MedRetParticipation))
	fmt.Printf("Calmar 勝率   %s   |   真擇時勝率(vs 同曝險) %s\n", pct(agg.CalmarWinRate), pct(agg.BlendSkillRate))
	fmt.Println("──────────────────────────────────────────────")
	fmt.Printf("G1 報酬參與 ≥75%% B&H .......... %s\n", passFail(agg.G1RetParticipation))
	fmt.Printf("G2 回撤 ≤60%% B&H ............... %s\n", passFail(agg.G2RiskReduction))
	fmt.Printf("G3 Calmar 勝率 ≥70%% ........... %s\n", passFail(agg.G3CalmarVsBH))
	fmt.Printf("G4 真擇時 ≥50%% (vs Blend) ..... %s\n", passFail(agg.G4Skill))
	fmt.Printf("綜合 (G1~G4): %s\n", passFail(agg.OverallPass))
}

func row(label, a, b string) { fmt.Printf("%-20s %16s %16s\n", label, a, b) }

func passFail(b bool) string {
	if b {
		return "PASS ✅"
	}
	return "FAIL ❌"
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

func money(x float64) string {
	neg := x < 0
	if neg {
		x = -x
	}
	s := fmt.Sprintf("%.0f", x)
	n := len(s)
	var b []byte
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, s[i])
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return sign + "$" + string(b)
}
