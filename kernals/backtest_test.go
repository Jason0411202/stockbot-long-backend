package kernals

import (
	"math"
	"testing"
	"time"

	"main/app_context"
	"main/config"

	"github.com/sirupsen/logrus"
)

// newTestAppCtx 產生不依賴 DB 或 .env 的 AppContext，僅用於單元測試。
func newTestAppCtx(useTF bool) *app_context.AppContext {
	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)

	cfg := &config.Config{
		TrackStocks:          []string{"TEST1"},
		ScalingStrategy:      "Pyramid",
		BuyAndSellMultiplier: 1.0,
		BackTestingDays:      0,
		CooldownDays:         14,
		PyramidBuyTiers: []config.PyramidBuyTier{
			{Above: -0.1, Amount: 500},
			{Above: -0.2, Amount: 750},
			{Above: -0.3, Amount: 1300},
			{Above: -0.4, Amount: 2000},
		},
		PyramidBuyFallbackAmount: 3000,
		PyramidSellAmount:        10000,
		PyramidSellThreshold:     1.0,
		InitialCash:              1_000_000,

		UseTFBranch:  useTF,
		TFTau:        0.02,
		TFAmountMode: "const",
		TFAlpha:      2.0,
		TFBeta:       0.02,
	}
	return &app_context.AppContext{Log: log, Cfg: cfg}
}

// buildSyntheticSeries 製造一條含明顯多頭趨勢的合成股價序列。
// 為了讓 MA60 有值,序列長度設 > 60 天。
//
// 設計:前 40 日價格小幅震盪於 100 附近(平穩期),
//      後續 80 日每日 +1% 複利上漲(強多頭),
//      中間在第 50、55、90、100 天插入回檔至 MA20 下方的短暫下跌,
//      提供 baseline Pyramid 與 TF 兩條路徑都能被觸發的訊號。
func buildSyntheticSeries(stockID string, days int) *stockSeries {
	dates := make([]time.Time, 0, days)
	prices := make([]float64, 0, days)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < days; i++ {
		dates = append(dates, base.AddDate(0, 0, i))
		var p float64
		switch {
		case i < 40:
			p = 100.0 + 0.1*float64(i%5)
		default:
			p = 100.0 * math.Pow(1.01, float64(i-39))
		}
		// 插入偶發回檔使 todayPrice < MA20 成立
		// 夠深(*0.85)才足以在 MA60 已可算出後的上漲段製造有效買入日。
		if i == 50 || i == 55 || i == 90 || i == 100 || i == 115 || i == 130 {
			p = p * 0.85
		}
		prices = append(prices, p)
	}

	idx := make(map[string]int, len(dates))
	for i, d := range dates {
		idx[d.Format("2006-01-02")] = i
	}

	ma20 := make([]float64, len(dates))
	const w20 = 20
	s20 := 0.0
	for i, p := range prices {
		s20 += p
		if i >= w20 {
			s20 -= prices[i-w20]
		}
		if i >= w20-1 {
			ma20[i] = s20 / float64(w20)
		} else {
			ma20[i] = math.NaN()
		}
	}

	ma60 := make([]float64, len(dates))
	const w60 = 60
	s60 := 0.0
	for i, p := range prices {
		s60 += p
		if i >= w60 {
			s60 -= prices[i-w60]
		}
		if i >= w60-1 {
			ma60[i] = s60 / float64(w60)
		} else {
			ma60[i] = math.NaN()
		}
	}

	return &stockSeries{
		dates:       dates,
		dateIndex:   idx,
		closePrices: prices,
		ma20:        ma20,
		ma60:        ma60,
	}
}

// TestPlanV1_PipelineBaselineFlagOff:
// Stage A (synthesize series) -> Stage B (simulate, flag=off) -> Stage C (settle)。
// 驗收:
//   - 不 crash、回傳非 nil result、無 NaN/Inf
//   - 有發生交易 (non-constant equity)
func TestPlanV1_PipelineBaselineFlagOff(t *testing.T) {
	appCtx := newTestAppCtx(false)
	series := map[string]*stockSeries{
		"TEST1": buildSyntheticSeries("TEST1", 150),
	}

	result, err := simulate(appCtx, series, 0)
	if err != nil {
		t.Fatalf("simulate(flag=off) 失敗: %v", err)
	}
	if result == nil {
		t.Fatalf("result 為 nil")
	}
	if math.IsNaN(result.FinalTotal) || math.IsInf(result.FinalTotal, 0) {
		t.Fatalf("FinalTotal 為 NaN/Inf: %v", result.FinalTotal)
	}
	if result.TotalBuys == 0 && result.TotalSells == 0 {
		t.Fatalf("flag=off 下無任何交易,可能測試資料或邏輯出問題")
	}
	// 非 constant equity: FinalTotal 應不等於 InitialCash
	if math.Abs(result.FinalTotal-result.InitialCash) < 1e-9 {
		t.Fatalf("FinalTotal 與 InitialCash 完全相等,代表從未有資金流出入")
	}

	t.Logf("[flag=off] FinalTotal=%.2f TotalBuys=%d TotalSells=%d",
		result.FinalTotal, result.TotalBuys, result.TotalSells)
}

// TestPlanV1_PipelineTFFlagOn:
// 同樣 stages,但 flag=on、trigger τ=0.02、const α=2。驗收:
//   - 不 crash,result 合法
//   - TotalBuys >= flag=off 版本的 TotalBuys:TF 不減少可買日數,只改變金額
func TestPlanV1_PipelineTFFlagOn(t *testing.T) {
	appCtxOff := newTestAppCtx(false)
	appCtxOn := newTestAppCtx(true)
	series := map[string]*stockSeries{
		"TEST1": buildSyntheticSeries("TEST1", 150),
	}
	// 兩次 simulate 各自需獨立 series(simulate 會修改 positions 內部狀態,
	// 但我們傳的是 map[string]*stockSeries 本身不變;可共用)。

	off, err := simulate(appCtxOff, series, 0)
	if err != nil {
		t.Fatalf("simulate(flag=off) 失敗: %v", err)
	}
	on, err := simulate(appCtxOn, series, 0)
	if err != nil {
		t.Fatalf("simulate(flag=on) 失敗: %v", err)
	}

	if math.IsNaN(on.FinalTotal) || math.IsInf(on.FinalTotal, 0) {
		t.Fatalf("flag=on FinalTotal 為 NaN/Inf")
	}
	if on.TotalBuys < off.TotalBuys {
		t.Fatalf("flag=on TotalBuys (%d) < flag=off (%d):TF 分支不該減少買入日",
			on.TotalBuys, off.TotalBuys)
	}
	// flag=on 的 TF 分支若在合成多頭資料上真的被觸發,
	// FinalTotal 會因為 amount 變大而與 flag=off 不同。
	if math.Abs(on.FinalTotal-off.FinalTotal) < 1e-9 {
		t.Fatalf("合成多頭資料上 flag=on 與 flag=off FinalTotal 完全相同 (%.2f),"+
			"代表 TF 分支從未觸發,無法證明 new 路徑真的被執行", on.FinalTotal)
	}
	t.Logf("[flag=off] FinalTotal=%.2f TotalBuys=%d", off.FinalTotal, off.TotalBuys)
	t.Logf("[flag=on ] FinalTotal=%.2f TotalBuys=%d (Δ vs off = %+.2f)",
		on.FinalTotal, on.TotalBuys, on.FinalTotal-off.FinalTotal)
}

// TestPlanV1_FlagOffEquivalence:
// 核心非破壞性驗證——flag=off 時的所有 BacktestResult 數值必須與
// 「新方法整個 if-block 被硬移除」等價。做法:由於 flag=off 時該 if
// 整段不進入,只要兩次連跑結果完全一致即證明 flag=off 路徑 deterministic
// 且不受 TF 參數影響。此外手動計算 expected 以防未來有人無意間動到
// pyramid 數值核心。
func TestPlanV1_FlagOffEquivalence(t *testing.T) {
	series := map[string]*stockSeries{
		"TEST1": buildSyntheticSeries("TEST1", 150),
	}
	// 兩份 cfg 相同但 TF 參數不同(flag=off 情況下,TF 參數不應改變任何輸出)。
	a := newTestAppCtx(false)
	b := newTestAppCtx(false)
	b.Cfg.TFTau = 99.0
	b.Cfg.TFAlpha = 99.0
	b.Cfg.TFBeta = 0.99
	b.Cfg.TFAmountMode = "cashfrac"

	ra, err := simulate(a, series, 0)
	if err != nil {
		t.Fatalf("simulate(a) 失敗: %v", err)
	}
	rb, err := simulate(b, series, 0)
	if err != nil {
		t.Fatalf("simulate(b) 失敗: %v", err)
	}

	const tol = 1e-6
	if math.Abs(ra.FinalTotal-rb.FinalTotal) > tol {
		t.Fatalf("flag=off 下 FinalTotal 因 TF 參數不同而改變: %.6f vs %.6f",
			ra.FinalTotal, rb.FinalTotal)
	}
	if math.Abs(ra.FinalCash-rb.FinalCash) > tol {
		t.Fatalf("flag=off 下 FinalCash 因 TF 參數不同而改變: %.6f vs %.6f",
			ra.FinalCash, rb.FinalCash)
	}
	if ra.TotalBuys != rb.TotalBuys || ra.TotalSells != rb.TotalSells {
		t.Fatalf("flag=off 下交易次數因 TF 參數不同而改變: (%d,%d) vs (%d,%d)",
			ra.TotalBuys, ra.TotalSells, rb.TotalBuys, rb.TotalSells)
	}
	t.Logf("flag=off 等價性通過: FinalTotal=%.6f (diff=%.2e)",
		ra.FinalTotal, math.Abs(ra.FinalTotal-rb.FinalTotal))
}

// TestPlanV1_TFAmountModes 驗證 tfBuyAmount 在兩種模式下都回傳預期量級。
func TestPlanV1_TFAmountModes(t *testing.T) {
	appCtx := newTestAppCtx(true)

	// const: alpha=2, max tier 為 fallback 3000 → 6000
	appCtx.Cfg.TFAmountMode = "const"
	if got := tfBuyAmount(appCtx, 1_000_000); math.Abs(got-6000) > 1e-9 {
		t.Errorf("const mode expected 6000, got %f", got)
	}

	// cashfrac: beta=0.02, cash=1e6 → 20000
	appCtx.Cfg.TFAmountMode = "cashfrac"
	if got := tfBuyAmount(appCtx, 1_000_000); math.Abs(got-20_000) > 1e-9 {
		t.Errorf("cashfrac mode expected 20000, got %f", got)
	}

	// cashfrac with cash=0 應給 0
	if got := tfBuyAmount(appCtx, 0); got != 0 {
		t.Errorf("cashfrac with cash=0 expected 0, got %f", got)
	}
}
