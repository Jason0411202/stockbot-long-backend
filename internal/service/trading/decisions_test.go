// internal/service/trading/decisions_test.go 驗證買賣決策規則、冷卻、深度權重與 regime 判定。
package trading

import (
	"math"
	"testing"
)

// decisions_test.go 為買賣決策純函式的單元測試 (金字塔底層)。
// 決策函式只看 Snapshot + cfg、無副作用,故每個規則 (均線、band、冷卻、深度權重、移動停利、獲利了結)
// 都能以一組凍結輸入獨立驗證。現行演算法 = 現金比例買入 + 比例賣出。

// --- DecideBuy ---

// TestDecideBuy_RejectsInvalidPriceOrMA 驗證價格為零或 MA 為 NaN 時 DecideBuy 不產生買入決策。
func TestDecideBuy_RejectsInvalidPriceOrMA(t *testing.T) {
	cfg := decideCfg()
	cases := []struct {
		name  string
		price float64
		ma    float64
	}{
		{"non-positive price", 0, 100},
		{"NaN MA (insufficient data)", 100, math.NaN()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			snap := Snapshot{TodayPrice: c.price, MA20: c.ma, Cash: 1_000_000, LowestHeldPrice: -1}
			// Act + Assert
			if got := DecideBuy(cfg, snap); got.Should {
				t.Fatalf("expected no buy, got %+v", got)
			}
		})
	}
}

// TestDecideBuy_BandGate 驗證牛熊市 band 閘門:空頭要求嚴格低於 MA,牛市允許在 MA×(1+band) 內買入。
func TestDecideBuy_BandGate(t *testing.T) {
	cfg := decideCfg()
	cfg.BullBuyBand = 0.05
	cases := []struct {
		name    string
		bull    bool
		price   float64
		wantBuy bool
	}{
		{"bear strict: price>=MA rejected", false, 100, false},
		{"bear strict: price<MA accepted", false, 99, true},
		{"bull relaxed: price within MA*1.05 accepted", true, 104, true},
		{"bull relaxed: price>=MA*1.05 rejected", true, 105, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange — 無持倉、無冷卻、現金充足,僅測 band 閘門。
			snap := Snapshot{TodayPrice: c.price, MA20: 100, IsBull: c.bull, Cash: 1_000_000, LowestHeldPrice: -1}
			// Act
			got := DecideBuy(cfg, snap)
			// Assert
			if got.Should != c.wantBuy {
				t.Fatalf("band gate %s: got Should=%v, want %v (%+v)", c.name, got.Should, c.wantBuy, got)
			}
		})
	}
}

// TestDecideBuy_CooldownBlocksThenAllowsAtBoundary 驗證冷卻天數內阻擋買入,恰好達冷卻邊界時放行。
func TestDecideBuy_CooldownBlocksThenAllowsAtBoundary(t *testing.T) {
	cfg := decideCfg() // CooldownDays 14
	base := Snapshot{TodayPrice: 90, MA20: 100, Cash: 1_000_000, LowestHeldPrice: 100, HasLastBuy: true}

	// Arrange — 9 天前買過 (< 14)。
	inside := base
	inside.Today = mustDate(t, "2024-06-10")
	inside.LastBuyDate = mustDate(t, "2024-06-01")
	// Act + Assert
	if DecideBuy(cfg, inside).Should {
		t.Fatalf("expected no buy inside cooldown")
	}

	// Arrange — 剛好 14 天。
	boundary := base
	boundary.Today = mustDate(t, "2024-06-15")
	boundary.LastBuyDate = mustDate(t, "2024-06-01")
	// Act + Assert
	if got := DecideBuy(cfg, boundary); !got.Should || got.Shares <= 0 {
		t.Fatalf("expected buy at cooldown boundary, got %+v", got)
	}
}

// TestDecideBuy_BreakBudgetOverridesCooldown 驗證冷卻期內有打破冷卻額度時可放行並標記 BrokeCooldown。
func TestDecideBuy_BreakBudgetOverridesCooldown(t *testing.T) {
	cfg := decideCfg()
	cfg.CooldownBreakBudget = 2

	// Arrange — 在冷卻內,但尚有打破冷卻額度。
	snap := Snapshot{
		Today: mustDate(t, "2024-06-05"), TodayPrice: 90, MA20: 100,
		Cash: 1_000_000, LowestHeldPrice: 100,
		HasLastBuy: true, LastBuyDate: mustDate(t, "2024-06-01"),
		CooldownBreaksLeft: 1,
	}

	// Act
	got := DecideBuy(cfg, snap)

	// Assert — 放行且標記動用了一次打破冷卻。
	if !got.Should || !got.BrokeCooldown {
		t.Fatalf("expected buy via break budget with BrokeCooldown=true, got %+v", got)
	}
}

// TestDecideBuy_NoCashNoBuy 驗證現金為零時 DecideBuy 不產生買入決策。
func TestDecideBuy_NoCashNoBuy(t *testing.T) {
	cfg := decideCfg()
	// Arrange — 現金基準為 0 → buyAmount 0 → 0 股。
	snap := Snapshot{TodayPrice: 90, MA20: 100, Cash: 0, LowestHeldPrice: -1}
	// Act + Assert
	if got := DecideBuy(cfg, snap); got.Should {
		t.Fatalf("expected no buy with zero cash basis, got %+v", got)
	}
}

// TestDecideBuy_BullSharesFromCashFraction 驗證牛市依現金比例計算買入股數 (20% 現金 ÷ 價格,四捨五入)。
func TestDecideBuy_BullSharesFromCashFraction(t *testing.T) {
	cfg := decideCfg() // BullBuyFrac 0.20
	// Arrange — bull、現金 1,000,000、價 100 → 目標 200,000 → 2000 股。
	snap := Snapshot{TodayPrice: 100, MA20: 100, IsBull: true, Cash: 1_000_000, LowestHeldPrice: -1}
	cfg.BullBuyBand = 0.05
	// Act
	got := DecideBuy(cfg, snap)
	// Assert
	if !got.Should || got.Shares != 2000 || got.Price != 100 {
		t.Fatalf("expected 2000 shares @100 from 20%% of cash, got %+v", got)
	}
}

// --- DecideSell ---

// TestDecideSell_NoPositions 驗證無持倉時 DecideSell 不產生賣出決策。
func TestDecideSell_NoPositions(t *testing.T) {
	cfg := decideCfg()
	// Arrange — LowestHeldPrice<=0 表示無持倉。
	snap := Snapshot{TodayPrice: 999, LowestHeldPrice: -1, IsBull: true}
	// Act + Assert
	if DecideSell(cfg, snap).Should {
		t.Fatalf("expected no sell with no positions")
	}
}

// TestDecideSell_ProfitTakeOnlyInBull 驗證獲利了結僅在牛市且漲幅達門檻時觸發,空頭不啟動獲利了結。
func TestDecideSell_ProfitTakeOnlyInBull(t *testing.T) {
	cfg := decideCfg() // threshold 1.0 (翻倍), SellFracOfPosition 0.33
	cases := []struct {
		name  string
		bull  bool
		price float64
		want  bool
	}{
		{"bull gain<100% no sell", true, 150, false},
		{"bull gain>=100% sells", true, 210, true},
		{"bear gain>=100% no profit-take", false, 210, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange — 最低成本 100,持股 99 (避免移動停利路徑混入,bear 無 peak)。
			snap := Snapshot{TodayPrice: c.price, LowestHeldPrice: 100, IsBull: c.bull, HeldShares: 99}
			// Act
			got := DecideSell(cfg, snap)
			// Assert
			if got.Should != c.want {
				t.Fatalf("%s: Should=%v want %v (%+v)", c.name, got.Should, c.want, got)
			}
			if got.Should {
				if got.Reason != "profit" || got.TargetShares != int(math.Round(0.33*99)) {
					t.Fatalf("%s: want profit sell of round(0.33*99), got %+v", c.name, got)
				}
			}
		})
	}
}

// TestDecideSell_SellFractionRoundsUpToOne 驗證持股量極小時賣出股數四捨五入後至少為 1 股。
func TestDecideSell_SellFractionRoundsUpToOne(t *testing.T) {
	cfg := decideCfg()
	// Arrange — 持股 2,round(0.33*2)=round(0.66)=1。
	snap := Snapshot{TodayPrice: 210, LowestHeldPrice: 100, IsBull: true, HeldShares: 2}
	// Act
	got := DecideSell(cfg, snap)
	// Assert
	if !got.Should || got.TargetShares != 1 {
		t.Fatalf("expected 1 share sold (min), got %+v", got)
	}
}

// TestDecideSell_BearTrailStop 驗證空頭移動停利:峰值漲幅達門檻後武裝,價格跌破峰值×(1-trail%) 時全出。
func TestDecideSell_BearTrailStop(t *testing.T) {
	cfg := decideCfg()
	cfg.TrailStopBear = 0.10
	cfg.TrailMinGain = 0.10

	cases := []struct {
		name  string
		peak  float64
		price float64
		held  int
		want  bool
	}{
		// 最低成本 100。peak 130 → peakGain 0.30 >= 0.10 (武裝);價 <= 130*0.9=117 → 觸發。
		{"armed and breached → sell all", 130, 116, 50, true},
		// peak 130 武裝,但價 118 > 117 → 未觸發。
		{"armed but not breached", 130, 118, 50, false},
		// peak 105 → peakGain 0.05 < 0.10 → 未武裝。
		{"not armed (peak gain too small)", 105, 90, 50, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange — 空頭、有持倉峰值。
			snap := Snapshot{
				TodayPrice: c.price, LowestHeldPrice: 100, IsBull: false,
				PeakSinceHold: c.peak, HeldShares: c.held,
			}
			// Act
			got := DecideSell(cfg, snap)
			// Assert
			if got.Should != c.want {
				t.Fatalf("%s: Should=%v want %v (%+v)", c.name, got.Should, c.want, got)
			}
			if got.Should && (got.Reason != "trail" || got.TargetShares != c.held) {
				t.Fatalf("%s: want full trail exit of %d, got %+v", c.name, c.held, got)
			}
		})
	}
}

// --- sub-functions ---

// TestFracBasis 驗證 fracBasis 依 BuyFracBasis 設定正確回傳現金、總權益或零作為買入基準。
func TestFracBasis(t *testing.T) {
	snap := Snapshot{Cash: 500, Equity: 1200}

	cash := decideCfg()
	cash.BuyFracBasis = "cash"
	if got := fracBasis(cash, snap); got != 500 {
		t.Fatalf("cash basis = %g, want 500", got)
	}

	equity := decideCfg()
	equity.BuyFracBasis = "equity"
	if got := fracBasis(equity, snap); got != 1200 {
		t.Fatalf("equity basis = %g, want 1200", got)
	}

	none := decideCfg()
	none.BuyFracBasis = ""
	if got := fracBasis(none, snap); got != 0 {
		t.Fatalf("empty basis = %g, want 0", got)
	}
}

// TestBearDepthWeight 驗證 bearDepthWeight 依跌幅分層回傳正確的倍率權重 (淺→1,中→ratio,深→ratio²)。
func TestBearDepthWeight(t *testing.T) {
	cfg := decideCfg() // ratio 2.5, tiers above -0.1/-0.2
	// depthPct > -0.1 (淺) → 命中 tier0 → ratio^0 = 1。
	if w := bearDepthWeight(cfg, -0.05); math.Abs(w-1) > 1e-9 {
		t.Fatalf("shallow depth weight = %g, want 1", w)
	}
	// -0.2 < depthPct <= -0.1 → 命中 tier1 → ratio^1 = 2.5。
	if w := bearDepthWeight(cfg, -0.15); math.Abs(w-2.5) > 1e-9 {
		t.Fatalf("mid depth weight = %g, want 2.5", w)
	}
	// 跌破最深 tier → ratio^len = 2.5^2 = 6.25。
	if w := bearDepthWeight(cfg, -0.30); math.Abs(w-6.25) > 1e-9 {
		t.Fatalf("deep depth weight = %g, want 6.25", w)
	}
}

// TestBuyDepthPct 驗證 buyDepthPct 依 BuyDepthBasis 設定,以持高、MA 或近期峰值計算跌幅百分比。
func TestBuyDepthPct(t *testing.T) {
	cases := []struct {
		name  string
		basis string
		snap  Snapshot
		want  float64
	}{
		{"held_high", "held_high", Snapshot{TodayPrice: 90, HighestHeldPrice: 100}, -0.10},
		{"ma", "ma", Snapshot{TodayPrice: 95, MA20: 100}, -0.05},
		{"peak", "peak", Snapshot{TodayPrice: 80, RecentPeak: 100}, -0.20},
		{"peak with zero peak → 0", "peak", Snapshot{TodayPrice: 80, RecentPeak: 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := decideCfg()
			cfg.BuyDepthBasis = c.basis
			if got := buyDepthPct(cfg, c.snap); math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("buyDepthPct %s = %g, want %g", c.name, got, c.want)
			}
		})
	}
}

// TestAmountToShares 驗證 amountToShares 將金額除以單價後四捨五入,價格或金額為零時回傳零。
func TestAmountToShares(t *testing.T) {
	cases := []struct {
		amount, price float64
		want          int
	}{
		{1000, 100, 10},
		{1050, 100, 11}, // 10.5 → 四捨五入 11
		{0, 100, 0},
		{1000, 0, 0},
	}
	for _, c := range cases {
		if got := amountToShares(c.amount, c.price); got != c.want {
			t.Fatalf("amountToShares(%g,%g) = %d, want %d", c.amount, c.price, got, c.want)
		}
	}
}

// TestPassesCooldown 驗證 passesCooldown 在各種冷卻狀態 (無紀錄/已過/未過/有額度) 的放行與阻擋行為。
func TestPassesCooldown(t *testing.T) {
	cfg := decideCfg() // CooldownDays 14
	today := mustDate(t, "2024-06-20")

	// 無上次買入 → 通過、未動用額度。
	if ok, broke := passesCooldown(cfg, Snapshot{}); !ok || broke {
		t.Fatalf("no last buy should pass without break, got ok=%v broke=%v", ok, broke)
	}
	// 已過冷卻 → 通過。
	past := Snapshot{HasLastBuy: true, Today: today, LastBuyDate: mustDate(t, "2024-06-01")}
	if ok, broke := passesCooldown(cfg, past); !ok || broke {
		t.Fatalf("past cooldown should pass, got ok=%v broke=%v", ok, broke)
	}
	// 冷卻內、無額度 → 擋下。
	cfg.CooldownBreakBudget = 0
	inside := Snapshot{HasLastBuy: true, Today: today, LastBuyDate: mustDate(t, "2024-06-15")}
	if ok, _ := passesCooldown(cfg, inside); ok {
		t.Fatalf("inside cooldown without budget should block")
	}
	// 冷卻內、有額度 → 通過並標記。
	cfg.CooldownBreakBudget = 2
	inside.CooldownBreaksLeft = 1
	if ok, broke := passesCooldown(cfg, inside); !ok || !broke {
		t.Fatalf("inside cooldown with budget should pass+broke, got ok=%v broke=%v", ok, broke)
	}
}

// TestPassesCooldown_BullUsesBullCooldown 驗證牛市採用較短的 BullCooldownDays,與空頭的 CooldownDays 互相獨立。
func TestPassesCooldown_BullUsesBullCooldown(t *testing.T) {
	cfg := decideCfg()
	cfg.CooldownDays = 30
	cfg.BullCooldownDays = 5
	// Arrange — 7 天前買過。空頭 (30天) 應擋;牛市 (5天) 應放行。
	snap := Snapshot{HasLastBuy: true, Today: mustDate(t, "2024-06-08"), LastBuyDate: mustDate(t, "2024-06-01")}
	if ok, _ := passesCooldown(cfg, snap); ok {
		t.Fatalf("bear 30d cooldown should block at 7 days")
	}
	snap.IsBull = true
	if ok, _ := passesCooldown(cfg, snap); !ok {
		t.Fatalf("bull 5d cooldown should pass at 7 days")
	}
}

// TestRegimeBull 驗證 regimeBull 在 ma_pos 與 mom 方法下正確判斷牛熊市,資料不足時回傳 false。
func TestRegimeBull(t *testing.T) {
	// Arrange — 上升序列,maAt 在足夠資料後有效。
	up := seriesFrom(mustDate(t, "2020-01-01"), linRamp(120, 50, 170))
	cfg := decideCfg()

	t.Run("ma_pos: close above MA → bull", func(t *testing.T) {
		cfg.RegimeMethod = "ma_pos"
		cfg.RegimeMAWindow = 20
		if !regimeBull(cfg, up, 119) {
			t.Fatalf("rising series should be bull under ma_pos")
		}
	})
	t.Run("ma_pos: insufficient data → false", func(t *testing.T) {
		cfg.RegimeMethod = "ma_pos"
		cfg.RegimeMAWindow = 200 // 比資料長 → NaN → false
		if regimeBull(cfg, up, 119) {
			t.Fatalf("insufficient MA data should be bear (false)")
		}
	})
	t.Run("mom: close above N-days-ago → bull", func(t *testing.T) {
		cfg.RegimeMethod = "mom"
		cfg.RegimeLookback = 50
		if !regimeBull(cfg, up, 119) {
			t.Fatalf("rising series should be bull under mom")
		}
	})
	t.Run("empty method → false", func(t *testing.T) {
		cfg.RegimeMethod = ""
		if regimeBull(cfg, up, 119) {
			t.Fatalf("empty regime method should be false")
		}
	})
}
