// internal/config/config_test.go 驗證設定載入、欄位預設值套用及 per-stock override 邏輯。
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// iptr 將 int 值包裝成指標，供 StockParams override 欄位使用。
func iptr(v int) *int { return &v }

// fptr 將 float64 值包裝成指標，供 StockParams override 欄位使用。
func fptr(v float64) *float64 { return &v }

// ForStock 應套用該股 override、不動到其他股、不改 base,無 override 時回傳原指標。
func TestForStock_Overrides(t *testing.T) {
	base := &Config{
		RegimeMAWindow: 95, BullBuyFrac: 0.20, TrailStopBear: 0.10, TrailMinGain: 0.10,
		StockOverrides: map[string]StockParams{
			"00631L": {RegimeMAWindow: iptr(60), BullBuyFrac: fptr(0.15)},
		},
	}

	// 有 override 的股:被覆寫的欄位變更,未列的繼承。
	eff := base.ForStock("00631L")
	if eff.RegimeMAWindow != 60 || eff.BullBuyFrac != 0.15 {
		t.Fatalf("override 未生效: regMA=%d bullFrac=%.2f", eff.RegimeMAWindow, eff.BullBuyFrac)
	}
	if eff.TrailStopBear != 0.10 || eff.TrailMinGain != 0.10 {
		t.Fatalf("未列欄位應繼承共用值, 得 trail=%.2f tmin=%.2f", eff.TrailStopBear, eff.TrailMinGain)
	}
	// base 不可被改動。
	if base.RegimeMAWindow != 95 || base.BullBuyFrac != 0.20 {
		t.Fatalf("ForStock 不應改動 base: regMA=%d bullFrac=%.2f", base.RegimeMAWindow, base.BullBuyFrac)
	}
	// 無 override 的股:回傳原指標 (零成本)。
	if got := base.ForStock("00830"); got != base {
		t.Fatalf("無 override 應回傳原 *Config 指標")
	}
}

// ForStock 應套用「每一個」可覆寫欄位,且不動到 base。
func TestForStock_AllFieldsOverridden(t *testing.T) {
	// Arrange
	base := &Config{
		MAWindow: 10, RegimeMAWindow: 95, BullBuyBand: 0.05, CooldownDays: 14, BullCooldownDays: 14,
		BullBuyFrac: 0.20, BearBuyFrac: 0.02, BuyTierRatio: 2.5, BaselineSellThreshold: 1.0,
		SellFracOfPosition: 0.33, TrailStopBear: 0.10, TrailMinGain: 0.10,
		StockOverrides: map[string]StockParams{
			"X": {
				MAWindow: iptr(5), RegimeMAWindow: iptr(60), BullBuyBand: fptr(0.08),
				CooldownDays: iptr(7), BullCooldownDays: iptr(3), BullBuyFrac: fptr(0.25),
				BearBuyFrac: fptr(0.03), BuyTierRatio: fptr(3.0), BaselineSellThreshold: fptr(0.8),
				SellFracOfPosition: fptr(0.5), TrailStopBear: fptr(0.12), TrailMinGain: fptr(0.05),
			},
		},
	}

	// Act
	e := base.ForStock("X")

	// Assert — 每個欄位都被覆寫成 override 值。
	if e.MAWindow != 5 || e.RegimeMAWindow != 60 || e.BullBuyBand != 0.08 || e.CooldownDays != 7 ||
		e.BullCooldownDays != 3 || e.BullBuyFrac != 0.25 || e.BearBuyFrac != 0.03 || e.BuyTierRatio != 3.0 ||
		e.BaselineSellThreshold != 0.8 || e.SellFracOfPosition != 0.5 || e.TrailStopBear != 0.12 || e.TrailMinGain != 0.05 {
		t.Fatalf("not all fields overridden: %+v", e)
	}
	// base 不被改動。
	if base.MAWindow != 10 || base.TrailMinGain != 0.10 {
		t.Fatalf("ForStock mutated base")
	}
}

// writeConfig 將 body 寫入暫存目錄的 config.yaml 並回傳其路徑。
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write tmp config: %v", err)
	}
	return p
}

const baseYaml = `
track_stocks:
  - "006208"
scaling_strategy: Baseline
max_back_months: 1
cooldown_days: 14
buy_frac_basis: cash
bull_buy_frac: 0.20
bear_buy_frac: 0.02
buy_tier_ratio: 2.5
buy_depth_basis: peak
baseline_buy_tiers:
  - { above: -0.1 }
  - { above: -0.2 }
baseline_sell_threshold: 1.0
sell_frac_of_position: 0.33
initial_cash: 1000000
`

// TestLoad_BackTestingMonthsSanityCheck 驗證 back_testing_months 超過 init_db_back_months 時 Load 回傳錯誤。
func TestLoad_BackTestingMonthsSanityCheck(t *testing.T) {
	cases := []struct {
		name              string
		initDBBackMonths  int
		backTestingMonths int
		wantErr           bool
		wantErrContains   string
	}{
		{
			name:              "disabled backtest passes regardless of init months",
			initDBBackMonths:  1,
			backTestingMonths: -1,
			wantErr:           false,
		},
		{
			name:              "zero backtest passes regardless of init months",
			initDBBackMonths:  1,
			backTestingMonths: 0,
			wantErr:           false,
		},
		{
			name:              "exact equality passes (60 == 60)",
			initDBBackMonths:  60,
			backTestingMonths: 60,
			wantErr:           false,
		},
		{
			name:              "just under passes (59 < 60)",
			initDBBackMonths:  60,
			backTestingMonths: 59,
			wantErr:           false,
		},
		{
			name:              "just over rejects (61 > 60)",
			initDBBackMonths:  60,
			backTestingMonths: 61,
			wantErr:           true,
			wantErrContains:   "back_testing_months=61",
		},
		{
			name:              "the original README footgun: months mismatch by ~3x",
			initDBBackMonths:  60,
			backTestingMonths: 180,
			wantErr:           true,
			wantErrContains:   "init_db_back_months=60",
		},
		{
			name:              "180 init supports 180 backtest",
			initDBBackMonths:  180,
			backTestingMonths: 180,
			wantErr:           false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := baseYaml +
				"init_db_back_months: " + itoa(tc.initDBBackMonths) + "\n" +
				"back_testing_months: " + itoa(tc.backTestingMonths) + "\n"
			p := writeConfig(t, body)

			_, err := Load(p)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// Load 應對省略的欄位套用合理預設值。
func TestLoad_AppliesDefaults(t *testing.T) {
	// Arrange — 給必要欄位 + 現金比例旋鈕 (Baseline 必填),其餘留空看預設。
	body := `
track_stocks:
  - "00631L"
initial_cash: 100000
init_db_back_months: 60
back_testing_months: -1
buy_frac_basis: cash
bull_buy_frac: 0.20
bear_buy_frac: 0.02
sell_frac_of_position: 0.33
`
	// Act
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Assert — 預設:ScalingStrategy=Baseline、CooldownDays=14。
	if cfg.ScalingStrategy != "Baseline" {
		t.Fatalf("ScalingStrategy default = %q, want Baseline", cfg.ScalingStrategy)
	}
	if cfg.CooldownDays != 14 {
		t.Fatalf("CooldownDays default = %d, want 14", cfg.CooldownDays)
	}
}

// Load 應正確解析完整設定 (含現金比例旋鈕、tier 邊界、per-stock override)。
func TestLoad_FullValid(t *testing.T) {
	// Arrange
	body := baseYaml + `
init_db_back_months: 60
back_testing_months: 60
regime_method: ma_pos
regime_ma_window: 95
stock_overrides:
  "00631L":
    regime_ma_window: 60
`
	// Act
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Assert — 核心欄位 + tier 邊界 + per-stock override 都正確讀入。
	if cfg.BuyFracBasis != "cash" || cfg.BullBuyFrac != 0.20 || cfg.BearBuyFrac != 0.02 {
		t.Fatalf("frac knobs misparsed: %+v", cfg)
	}
	if len(cfg.BaselineBuyTiers) != 2 || cfg.BaselineBuyTiers[0].Above != -0.1 {
		t.Fatalf("baseline tiers misparsed: %+v", cfg.BaselineBuyTiers)
	}
	if eff := cfg.ForStock("00631L"); eff.RegimeMAWindow != 60 {
		t.Fatalf("per-stock override not parsed: regimeMA=%d, want 60", eff.RegimeMAWindow)
	}
}

// Baseline 策略缺少現金比例旋鈕時應 fail-fast (避免靜默不交易)。
func TestLoad_BaselineRequiresFracKnobs(t *testing.T) {
	// Arrange — 有 track_stocks 但完全沒有現金比例旋鈕。
	body := `
track_stocks:
  - "00631L"
scaling_strategy: Baseline
initial_cash: 100000
init_db_back_months: 60
back_testing_months: -1
`
	// Act + Assert
	if _, err := Load(writeConfig(t, body)); err == nil {
		t.Fatalf("expected error when cash-fraction knobs missing for Baseline")
	}

	// 補齊四個旋鈕後應通過。
	ok := body + "buy_frac_basis: cash\nbull_buy_frac: 0.2\nbear_buy_frac: 0.02\nsell_frac_of_position: 0.33\n"
	if _, err := Load(writeConfig(t, ok)); err != nil {
		t.Fatalf("complete cash-fraction config should load, got %v", err)
	}
}

// TestLoad_FileNotFound 驗證設定檔不存在時 Load 回傳錯誤。
func TestLoad_FileNotFound(t *testing.T) {
	// Arrange + Act + Assert
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected error for missing config file")
	}
}

// TestLoad_InvalidYAML 驗證 YAML 格式錯誤時 Load 回傳解析錯誤。
func TestLoad_InvalidYAML(t *testing.T) {
	// Arrange — 壞掉的 YAML。
	if _, err := Load(writeConfig(t, "track_stocks: [unclosed")); err == nil {
		t.Fatalf("expected parse error for malformed yaml")
	}
}

// TestLoad_TrackStocksRequired 驗證 track_stocks 為空時 Load 回傳錯誤。
func TestLoad_TrackStocksRequired(t *testing.T) {
	body := `
scaling_strategy: Baseline
max_back_months: 1
init_db_back_months: 60
back_testing_months: -1
baseline_buy_fallback_amount: 3000
baseline_sell_threshold: 1.0
baseline_sell_amount: 10000
initial_cash: 1000000
`
	p := writeConfig(t, body)
	if _, err := Load(p); err == nil {
		t.Fatalf("expected error for empty track_stocks")
	}
}

// itoa 將整數轉換為十進位字串，不依賴 strconv，供測試 YAML 組裝使用。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
