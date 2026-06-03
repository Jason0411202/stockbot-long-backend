package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"text/tabwriter"

	"main/app_context"
	"main/kernals"
	"main/sqls"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

// cmd/evaluate 為 walk-forward 策略評估 runner:
//  1. 載入 .env 與 config.yaml
//  2. 連線 DB (使用既有歷史資料,不觸發爬蟲)
//  3. 在「2 檔共同有效資料期」內以滾動視窗評估策略 vs 三個對照組
//  4. 印出每視窗明細 + 彙整 scorecard + PASS/FAIL 結論 + 方法論揭露
//
// 與 cmd/research_run 的差別:research_run 是「單一長區間、單一數字」的舊回測;
// evaluate 是「多視窗、benchmark 相對、資金加權、防作弊」的新評估。
func main() {
	windowMonths := flag.Int("window", 24, "每個視窗長度 (日曆月)")
	stepMonths := flag.Int("step", 3, "視窗起點間隔 (日曆月)")
	dcaEvery := flag.Int("dca-every", 21, "naive DCA 投入頻率 (交易日)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日 (低於此略過)")
	stocksCSV := flag.String("stocks", "", "覆寫追蹤標的 (逗號分隔,例如 00631L 或 006208,00830);留空用 config.yaml")
	flag.Parse()

	if err := godotenv.Load(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "[warn] 未找到 .env,改用系統環境變數:", err)
	}

	appCtx := app_context.NewAppContext()
	if *stocksCSV != "" {
		var stocks []string
		for _, s := range strings.Split(*stocksCSV, ",") {
			if s = strings.TrimSpace(s); s != "" {
				stocks = append(stocks, s)
			}
		}
		appCtx.Cfg.TrackStocks = stocks
		appCtx.Log.Infof("已覆寫追蹤標的為 %v", stocks)
	}
	if err := sqls.ConnectToMariadb(appCtx); err != nil {
		appCtx.Log.Fatalf("ConnectToMariadb 失敗: %v", err)
	}
	if err := sqls.ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		appCtx.Log.Fatalf("ConnectToDatabase 失敗: %v", err)
	}

	p := kernals.WalkForwardParams{
		WindowMonths: *windowMonths,
		StepMonths:   *stepMonths,
		DCAEveryDays: *dcaEvery,
		MinTradeDays: *minDays,
	}
	reports, agg, err := kernals.RunWalkForward(appCtx, p)
	if err != nil {
		appCtx.Log.Fatalf("RunWalkForward 失敗: %v", err)
	}
	printReport(appCtx.Cfg.TrackStocks, p, reports, agg)
}

// --- 格式化小工具 (處理 Inf/NaN) ---

func pct(x float64) string {
	if math.IsNaN(x) {
		return "  n/a"
	}
	if math.IsInf(x, 0) {
		return "  inf"
	}
	return fmt.Sprintf("%+.1f%%", x*100)
}

func ratio(x float64) string {
	if math.IsNaN(x) {
		return " n/a"
	}
	if math.IsInf(x, 1) {
		return " inf"
	}
	if math.IsInf(x, -1) {
		return "-inf"
	}
	return fmt.Sprintf("%.2f", x)
}

// partCell 顯示參與率 (策略 CAGR / B&H CAGR);B&H CAGR <= 0 時比值無意義,顯示 "—"。
func partCell(stratCAGR, bhCAGR float64) string {
	if bhCAGR <= 0 || math.IsNaN(bhCAGR) {
		return "   —"
	}
	return ratio(stratCAGR / bhCAGR)
}

func yesNo(b bool) string {
	if b {
		return "✓"
	}
	return "·"
}

func passFail(b bool) string {
	if b {
		return "PASS ✅"
	}
	return "FAIL ❌"
}

func printReport(stocks []string, p kernals.WalkForwardParams, reports []kernals.WindowReport, agg kernals.AggregateReport) {
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Println("  WALK-FORWARD 策略評估 — 相對 benchmark / 資金加權 / 多視窗")
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Printf("  追蹤標的      : %v\n", stocks)
	fmt.Printf("  視窗設定      : %d 個月長 / 每 %d 個月滾動一次 / 最少 %d 交易日\n",
		p.WindowMonths, p.StepMonths, p.MinTradeDays)
	fmt.Printf("  naive DCA     : 每 %d 交易日定額投入 (自動投滿整池)\n", p.DCAEveryDays)
	if len(reports) > 0 {
		fmt.Printf("  共同有效資料期: %s ~ %s (共 %d 個視窗)\n",
			reports[0].Start.Format("2006-01-02"),
			reports[len(reports)-1].End.Format("2006-01-02"),
			len(reports))
	}
	fmt.Println()

	// --- 每視窗明細 ---
	fmt.Println("  ── 每視窗明細 (CAGR=年化報酬, MDD=最大回撤, Cal=Calmar, Exp=平均持股佔比) ──")
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 1, ' ', 0)
	fmt.Fprintln(w, "  進場\t結束\t年\tStrat\t \t \t \tB&H\t \t \tBlend\tDCA\t參與\t判定")
	fmt.Fprintln(w, "  (start)\t(end)\t數\tCAGR\tMDD\tCal\tExp\tCAGR\tMDD\tCal\tCal\tCAGR\t率\tC>B Skill")
	fmt.Fprintln(w, "  ───────\t───────\t──\t─────\t─────\t────\t───\t─────\t─────\t────\t────\t─────\t────\t─────────")
	for _, r := range reports {
		fmt.Fprintf(w, "  %s\t%s\t%.1f\t%s\t%s\t%s\t%.0f%%\t%s\t%s\t%s\t%s\t%s\t%s\t%s %s\n",
			r.Start.Format("06-01-02"), r.End.Format("06-01-02"), r.Years,
			pct(r.Strat.CAGR), pct(r.Strat.MaxDD), ratio(r.Strat.Calmar), r.Strat.AvgExp*100,
			pct(r.BH.CAGR), pct(r.BH.MaxDD), ratio(r.BH.Calmar),
			ratio(r.Blend.Calmar), pct(r.DCA.CAGR),
			partCell(r.Strat.CAGR, r.BH.CAGR),
			yesNo(r.CalmarBeatsBH), yesNo(r.BeatsBlendBoth))
	}
	w.Flush()
	fmt.Println()

	// --- 彙整 ---
	fmt.Println("  ── 跨視窗彙整 (中位數) ──")
	participation := "n/a"
	if agg.MedBHCAGR > 0 {
		participation = ratio(agg.MedStratCAGR / agg.MedBHCAGR) // 中位數之比 (與左右兩個中位一致)
	}
	fmt.Printf("    年化報酬 CAGR       策略 %s  vs  B&H %s   (參與率 %s = 策略中位/B&H中位)\n",
		pct(agg.MedStratCAGR), pct(agg.MedBHCAGR), participation)
	fmt.Printf("    最大回撤 MaxDD      策略 %s  vs  B&H %s\n",
		pct(agg.MedStratMDD), pct(agg.MedBHMDD))
	fmt.Printf("    風險調整 Calmar     策略 %s    vs  B&H %s\n",
		ratio(agg.MedStratCalmar), ratio(agg.MedBHCalmar))
	fmt.Printf("    資金加權 XIRR(投入) 策略 %s  vs  B&H %s   (僅 %d/%d 視窗可唯一求解)\n",
		pct(agg.MedStratXIRR), pct(agg.MedBHXIRR), agg.NStratXIRRSolvable, agg.NWindows)
	fmt.Printf("    平均持股佔比        策略 %.0f%%   ← 策略低回撤有多少來自『抱現金』而非擇時;\n",
		agg.MedStratAvgExp*100)
	fmt.Printf("                       ⚠️ 上方策略 XIRR 是『僅約 %.0f%% 本金』的報酬,基數小、易被放大,不可與 B&H 池報酬 %s 直接比較\n",
		agg.MedStratAvgExp*100, pct(agg.MedBHCAGR))
	fmt.Printf("    Calmar 勝率(vs B&H) %.0f%%   |   真擇時勝率(vs 同曝險Blend) %.0f%%\n",
		agg.CalmarWinRate*100, agg.BlendSkillRate*100)
	fmt.Printf("    穩定度: CAGR 離散(std) %s   |   最差視窗 CAGR %s  MaxDD %s (B&H 最差 MaxDD %s)\n",
		pct(agg.DispersionStratCAGR), pct(agg.WorstStratCAGR), pct(agg.WorstStratMDD), pct(agg.WorstBHMDD))
	fmt.Println()

	// --- Scorecard ---
	fmt.Println("  ── SCORECARD:『守住 B&H 七成報酬 + 顯著更小回撤 + Calmar 穩定贏 + 真擇時』──")
	fmt.Printf("    G1 報酬參與  中位 Strat CAGR ≥ 75%% 中位 B&H CAGR ............ %s\n", passFail(agg.G1RetParticipation))
	fmt.Printf("    G2 風險降低  中位 |Strat MDD| ≤ 60%% 中位 |B&H MDD| .......... %s\n", passFail(agg.G2RiskReduction))
	fmt.Printf("    G3 風險調整  Calmar 勝率 ≥ 70%% (vs B&H) ................... %s\n", passFail(agg.G3CalmarVsBH))
	fmt.Printf("    G4 真擇時    擇時勝率 ≥ 50%% (vs 同曝險 Blend,防『抱現金』作弊) %s\n", passFail(agg.G4Skill))
	fmt.Printf("    G5 穩健性    最差視窗回撤 ≤ B&H 最差視窗回撤 ............... %s\n", passFail(agg.G5Robustness))
	fmt.Println("    ────────────────────────────────────────────────────────────────")
	fmt.Printf("    綜合結論 (G1~G4): %s\n", verdict(agg.OverallPass))
	fmt.Println()

	printDisclosures(stocks, agg)
}

func verdict(pass bool) string {
	if pass {
		return "這是一個『穩定、低風險、有競爭力且具真實擇時能力』的好策略 ✅"
	}
	return "尚未全部通過 —— 見下方揭露,逐關卡判斷差在哪、是否只是『抱現金』的假優勢 ⚠️"
}

func printDisclosures(stocks []string, agg kernals.AggregateReport) {
	fmt.Println("  ── 必要揭露 (避免被回測誤導) ──")
	fmt.Printf("    1. 倖存者/選股偏誤:%v 是『事後』挑選且都存活、長期向上的大盤型 ETF。\n", stocks)
	fmt.Println("       逢低加碼策略在『會跌但會回來』的多頭市場本就占優,本結果是『樂觀上界』,")
	fmt.Println("       不保證套用到任意或長期下跌的標的。建議另跑一檔走勢不佳的標的對照。")
	fmt.Println("    2. 未計交易成本:本評估為 gross(未扣手續費 0.1425%/邊、ETF 證交稅 0.1%)。")
	fmt.Println("       策略換手率遠高於『只買一次』的 B&H,計入成本後相對優勢會縮小。")
	fmt.Printf("    3. 低回撤≠擇時強:策略平均持股僅約 %.0f%%,大量現金本身就會壓低回撤。\n", agg.MedStratAvgExp*100)
	if agg.G4Skill {
		fmt.Println("       但 G4(對『同曝險 Blend』)已通過 → 這份優勢確實含擇時成分,不只是抱現金。")
	} else {
		fmt.Println("       且 G4(對『同曝險 Blend』)未通過 → 低回撤『主要來自抱現金』而非擇時,審慎看待。")
	}
	fmt.Println("    4. 比較基準:所有對照組與策略共用同一起始資金池、同一視窗、同一交易日歷,")
	fmt.Println("       且 benchmark 不替『尚未上市』的股票預留現金 (無未來資訊);MA20 採共同有效期起算。")
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}
