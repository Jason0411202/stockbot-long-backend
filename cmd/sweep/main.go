// cmd/sweep/main.go 對策略旋鈕做暴力網格搜尋,以 walk-forward + 滾動 OOS 護欄評分排序最佳參數。
package main

// cmd/sweep 用本機 CSV (由 cmd/fetch_data 產生) 對一組策略旗標的笛卡兒網格逐一回測,
// 每個組合都跑「全期 + walk-forward 五道關卡 + 滾動 IS/OOS」三項評估,
// 先以「通過 G1~G4 四道關卡」硬性過濾 (專案採用門檻),再依全期 Calmar 排序,
// 並同時列出 OOS 保留率與最差折 Calmar,避免挑到只在單一長區間漂亮的過擬合組合。
//
// 完全不依賴 MariaDB / docker;不修改 config.yaml,只印出排行供人工核可。
// 用法:go run ./cmd/sweep  (先確保 data/*.csv 已存在)。
import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// leveragedStock 為唯一採用 per-stock 覆寫的標的 (2x 槓桿);網格只對它掃 regime MA 與再進場冷卻。
const leveragedStock = "00631L"

// combo 為一組待評估的策略旗標 (共用旋鈕 + 00631L 覆寫)。
type combo struct {
	regimeMA      int     // 共用 regime_ma_window
	cdBreakBudget int     // cooldown_break_budget
	bullBand      float64 // bull_buy_band
	bullFrac      float64 // bull_buy_frac
	trailStop     float64 // trail_stop_bear
	trailMin      float64 // trail_min_gain
	ovRegimeMA    int     // 00631L 覆寫 regime_ma_window
	ovReentryCd   int     // 00631L 覆寫 trail_reentry_cooldown_days
}

// result 為單一組合的評估指標彙整。
type result struct {
	c          combo
	fullCalmar float64 // 全期 Strat Calmar
	fullMWR    float64 // 全期 Strat 資金加權報酬
	fullDD     float64 // 全期 Strat NAV 最大回撤 (<=0)
	fullMult   float64 // 全期 Strat 本金倍數
	wfMedCal   float64 // walk-forward 中位 Strat Calmar
	wfPass     bool    // walk-forward G1~G4 全過
	oosRet     float64 // 滾動 OOS Calmar 保留率 (OOS / IS)
	oosWorst   float64 // 滾動 OOS 最差折 Calmar
	ok         bool    // 三項評估皆成功
}

// main 載入 CSV、產生網格、平行評估、過濾排序後印出排行與相對 baseline 的全面勝出組合。
func main() {
	dataDir := flag.String("data", "data", "CSV 快取目錄")
	cfgPath := flag.String("config", "config.yaml", "基底設定檔")
	window := flag.Int("window", 24, "walk-forward 視窗 (月)")
	step := flag.Int("step", 3, "視窗步進 (月)")
	minDays := flag.Int("min-days", 200, "視窗最少交易日")
	isMonths := flag.Int("is-months", 36, "滾動 OOS 初始樣本內錨定月數")
	foldMonths := flag.Int("fold-months", 12, "滾動 OOS 每折月數")
	topN := flag.Int("top", 25, "列出前 N 名")
	flag.Parse()

	// 載入基底設定與 CSV 序列 (一次,供所有組合共享唯讀)。
	base, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "載入 config 失敗:", err)
		os.Exit(1)
	}
	// 驗證 CSV 可載入 (fail fast);每個 worker 之後各自載入一份獨立序列,
	// 因為 *trading.StockSeries 內含 lazy memo cache (peakAt 會寫 map),不可跨 goroutine 共享。
	if _, err := backtest.LoadSeriesFromCSV(*dataDir, base.TrackStocks); err != nil {
		fmt.Fprintln(os.Stderr, "載入 CSV 失敗 (先跑 go run ./cmd/fetch_data):", err)
		os.Exit(1)
	}
	wfp := backtest.WalkForwardParams{WindowMonths: *window, StepMonths: *step, MinTradeDays: *minDays}

	// 定義暴力網格 (每個值列皆含現行 config 值,使 baseline 一併入表可比)。
	grid := struct {
		regimeMA      []int
		cdBreakBudget []int
		bullBand      []float64
		bullFrac      []float64
		trailStop     []float64
		trailMin      []float64
		ovRegimeMA    []int
		ovReentryCd   []int
	}{
		regimeMA:      []int{60, 70, 85, 95, 110},
		cdBreakBudget: []int{2, 3, 4},
		bullBand:      []float64{0.05, 0.08, 0.11},
		bullFrac:      []float64{0.15, 0.20, 0.25},
		trailStop:     []float64{0.06, 0.08, 0.10},
		trailMin:      []float64{0.10, 0.15, 0.20},
		ovRegimeMA:    []int{50, 60, 70},
		ovReentryCd:   []int{0, 30, 42, 60},
	}

	// 展開所有組合。
	var combos []combo
	for _, rm := range grid.regimeMA {
		for _, cb := range grid.cdBreakBudget {
			for _, bb := range grid.bullBand {
				for _, bf := range grid.bullFrac {
					for _, ts := range grid.trailStop {
						for _, tm := range grid.trailMin {
							for _, orm := range grid.ovRegimeMA {
								for _, orc := range grid.ovReentryCd {
									combos = append(combos, combo{rm, cb, bb, bf, ts, tm, orm, orc})
								}
							}
						}
					}
				}
			}
		}
	}

	workers := runtime.NumCPU()
	fmt.Printf("Sweep: %d 組合, 標的 %v, workers=%d\n", len(combos), base.TrackStocks, workers)

	// 平行評估:worker pool 各自評估一段索引,結果寫入對應槽位 (無共享寫入,免鎖)。
	results := make([]result, len(combos))
	var idx int64 = -1
	var done int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每個 worker 載入專屬序列副本 (獨立的 memo cache),避免跨 goroutine 並行寫 map。
			s, err := backtest.LoadSeriesFromCSV(*dataDir, base.TrackStocks)
			if err != nil {
				fmt.Fprintln(os.Stderr, "worker 載入 CSV 失敗:", err)
				return
			}
			for {
				i := atomic.AddInt64(&idx, 1)
				if int(i) >= len(combos) {
					return
				}
				results[i] = evalCombo(base, s, wfp, *isMonths, *foldMonths, combos[i])
				if n := atomic.AddInt64(&done, 1); n%1000 == 0 {
					fmt.Fprintf(os.Stderr, "  ...%d/%d\n", n, len(combos))
				}
			}
		}()
	}
	wg.Wait()

	// 找出 baseline 組合 (對齊現行 config.yaml) 作為比較基準。
	baseCombo := combo{
		regimeMA: base.RegimeMAWindow, cdBreakBudget: base.CooldownBreakBudget,
		bullBand: base.BullBuyBand, bullFrac: base.BullBuyFrac,
		trailStop: base.TrailStopBear, trailMin: base.TrailMinGain,
		ovRegimeMA:  ovInt(base, leveragedStock, base.RegimeMAWindow, func(p config.StockParams) *int { return p.RegimeMAWindow }),
		ovReentryCd: ovInt(base, leveragedStock, base.TrailReentryCooldownDays, func(p config.StockParams) *int { return p.TrailReentryCooldownDays }),
	}
	var baseRes result
	for _, r := range results {
		if r.c == baseCombo {
			baseRes = r
			break
		}
	}

	// 過濾出通過 G1~G4 四道關卡的有效組合。
	var pass []result
	for _, r := range results {
		if r.ok && r.wfPass {
			pass = append(pass, r)
		}
	}

	// 印出 baseline 參考列。
	fmt.Println()
	fmt.Println("Baseline (現行 config.yaml):")
	printHeader()
	printRow(0, baseRes, true)
	fmt.Printf("\n通過 G1~G4 的組合: %d / %d\n", len(pass), len(combos))

	// 依全期 Calmar 由高到低排序,印出前 N 名。
	sort.Slice(pass, func(i, j int) bool { return pass[i].fullCalmar > pass[j].fullCalmar })
	fmt.Printf("\n== 通過四道關卡者,依全期 Calmar 排序 前 %d 名 ==\n", *topN)
	printHeader()
	for i := 0; i < *topN && i < len(pass); i++ {
		printRow(i+1, pass[i], pass[i].c == baseCombo)
	}

	// 找出「在每一條軸 (Calmar / MWR / 回撤 / OOS 最差折) 都不輸 baseline 且 Calmar 嚴格更高」的組合。
	fmt.Println("\n== 全面勝出 baseline 的組合 (Calmar↑ 且 MWR / 回撤 / OOS最差折 皆不輸,通過四關卡) ==")
	var dom []result
	for _, r := range pass {
		if r.c == baseCombo {
			continue
		}
		if r.fullCalmar > baseRes.fullCalmar+1e-9 &&
			r.fullMWR >= baseRes.fullMWR-1e-9 &&
			r.fullDD >= baseRes.fullDD-1e-9 && // DD<=0,值較大 = 回撤較淺
			r.oosWorst >= baseRes.oosWorst-1e-9 {
			dom = append(dom, r)
		}
	}
	sort.Slice(dom, func(i, j int) bool { return dom[i].fullCalmar > dom[j].fullCalmar })
	if len(dom) == 0 {
		fmt.Println("  (無:沒有任何組合在全部四條軸上都不輸現行 baseline)")
	} else {
		printHeader()
		for i := 0; i < len(dom) && i < *topN; i++ {
			printRow(i+1, dom[i], false)
		}
	}
}

// evalCombo 以組合的旗標覆寫基底設定,跑全期 + walk-forward + 滾動 OOS,回傳指標彙整。
func evalCombo(base *config.Config, series map[string]*trading.StockSeries, wfp backtest.WalkForwardParams, isMonths, foldMonths int, c combo) result {
	// 淺拷貝基底並覆寫共用旋鈕;StockOverrides 換成本組合專屬的新 map (避免並行共享)。
	cfg := *base
	cfg.RegimeMAWindow = c.regimeMA
	cfg.CooldownBreakBudget = c.cdBreakBudget
	cfg.BullBuyBand = c.bullBand
	cfg.BullBuyFrac = c.bullFrac
	cfg.TrailStopBear = c.trailStop
	cfg.TrailMinGain = c.trailMin
	ovReg, ovCd := c.ovRegimeMA, c.ovReentryCd
	cfg.StockOverrides = map[string]config.StockParams{
		leveragedStock: {RegimeMAWindow: &ovReg, TrailReentryCooldownDays: &ovCd},
	}

	full, err := backtest.EvaluateFullSpan(&cfg, series)
	if err != nil {
		return result{c: c}
	}
	_, agg, err := backtest.EvaluateWalkForward(&cfg, series, wfp)
	if err != nil {
		return result{c: c}
	}
	isoos, err := backtest.EvaluateRollingOOS(&cfg, series, wfp, isMonths, foldMonths)
	if err != nil {
		return result{c: c}
	}

	// 計算 OOS Calmar 保留率與最差折 Calmar (僅取有限值)。
	ret := math.NaN()
	if isoos.IS.MedStratCalmar != 0 {
		ret = isoos.OOS.MedStratCalmar / isoos.IS.MedStratCalmar
	}
	worst := math.Inf(1)
	for _, f := range isoos.Folds {
		if !math.IsNaN(f.Calmar) && !math.IsInf(f.Calmar, 0) && f.Calmar < worst {
			worst = f.Calmar
		}
	}
	if math.IsInf(worst, 1) {
		worst = math.NaN()
	}

	return result{
		c:          c,
		fullCalmar: full.Strat.Calmar,
		fullMWR:    full.Strat.MWR,
		fullDD:     full.Strat.MaxDD,
		fullMult:   full.Strat.Multiple,
		wfMedCal:   agg.MedStratCalmar,
		wfPass:     agg.OverallPass,
		oosRet:     ret,
		oosWorst:   worst,
		ok:         true,
	}
}

// ovInt 讀取某股 override 的指標型 int 欄位;無覆寫時回傳 fallback。
func ovInt(c *config.Config, stockID string, fallback int, pick func(config.StockParams) *int) int {
	if ov, ok := c.StockOverrides[stockID]; ok {
		if v := pick(ov); v != nil {
			return *v
		}
	}
	return fallback
}

// printHeader 印出排行表頭。
func printHeader() {
	fmt.Printf("%-4s %5s %3s %5s %5s %5s %5s | %6s %7s | %7s %7s %7s %6s | %6s %6s %7s\n",
		"#", "regMA", "cdB", "band", "frac", "trail", "tmin", "631reg", "reentry",
		"fCalmar", "fMWR", "fDD", "fMult", "wfCal", "oosRet", "oosWrst")
}

// printRow 印出單列結果;marked=true 時於行末標註為 baseline。
func printRow(rank int, r result, marked bool) {
	tag := ""
	if marked {
		tag = "  <= baseline"
	}
	fmt.Printf("%-4d %5d %3d %5.2f %5.2f %5.2f %5.2f | %6d %7d | %7.2f %+6.1f%% %+6.1f%% %5.1fx | %6.2f %5.0f%% %7.2f%s\n",
		rank, r.c.regimeMA, r.c.cdBreakBudget, r.c.bullBand, r.c.bullFrac, r.c.trailStop, r.c.trailMin,
		r.c.ovRegimeMA, r.c.ovReentryCd,
		r.fullCalmar, r.fullMWR*100, r.fullDD*100, r.fullMult, r.wfMedCal, r.oosRet*100, r.oosWorst, tag)
}
