package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BaselineBuyTier baseline 策略中的單一加碼級距。
// 當 (深度判斷值) > Above 時便以此級距買入,由淺至深檢查取第一筆滿足者。
// 幾何加碼曲線啟用時 (BuyBaseAmount>0 && BuyTierRatio>0),Amount 由曲線覆寫,只剩 Above 邊界有效。
type BaselineBuyTier struct {
	Above  float64 `yaml:"above"`
	Amount float64 `yaml:"amount"`
}

// Config 為不私密的超參數，由 config.yaml 讀入後供全 app 使用。
//
// 「牛熊 regime 感知逢低加碼」策略的旋鈕 (見 docs/optimization/BEST-STRATEGY.md)。
// 每個旋鈕都經 walk-forward 掃描驗證採納;預設零值 = 原始 Baseline 行為,既有測試不受影響。
type Config struct {
	TrackStocks               []string          `yaml:"track_stocks"`
	ScalingStrategy           string            `yaml:"scaling_strategy"`
	BuyAndSellMultiplier      float64           `yaml:"buy_and_sell_multiplier"`
	MaxBackMonths             int               `yaml:"max_back_months"`
	BackTestingMonths         int               `yaml:"back_testing_months"`
	CooldownDays              int               `yaml:"cooldown_days"`
	BaselineBuyTiers          []BaselineBuyTier `yaml:"baseline_buy_tiers"`
	BaselineBuyFallbackAmount float64           `yaml:"baseline_buy_fallback_amount"`
	BaselineSellAmount        float64           `yaml:"baseline_sell_amount"`
	BaselineSellThreshold     float64           `yaml:"baseline_sell_threshold"`
	InitialCash               float64           `yaml:"initial_cash"`
	InitDBBackMonths          int               `yaml:"init_db_back_months"`

	// 進場均線長度。<=0 視為 20。
	MAWindow int `yaml:"ma_window"`

	// 加碼「深度基準」(決定 tier 判斷值要拿什麼當「跌多深」):
	//   "" / "held_high":(今價-持倉最高買入價)/持倉最高買入價 (原始,較粗糙)
	//   "ma"            :(今價-進場均線)/進場均線 (乖離率)
	//   "peak"          :(今價-近 BuyPeakLookback 日最高價)/最高價 (距高點回撤;最佳)
	BuyDepthBasis   string `yaml:"buy_depth_basis"`
	BuyPeakLookback int    `yaml:"buy_peak_lookback"` // peak 基準回看交易日;<=0 視為 252

	// 牛熊判定:"" = 關閉 (恆為 bear/中性)。
	//   "ma_pos"  :收盤 > RegimeMAWindow 日均線 → bull (掃描證實最佳)
	//   "ma_slope":RegimeMAWindow 日均線 > RegimeLookback 日前 → bull
	//   "mom"     :收盤 > RegimeLookback 日前收盤 → bull
	RegimeMethod   string `yaml:"regime_method"`
	RegimeMAWindow int    `yaml:"regime_ma_window"` // <=0 視為 200
	RegimeLookback int    `yaml:"regime_lookback"`  // slope/mom 回看交易日;<=0 視為 200/252

	// 牛市行為:放寬進場 + 固定大額,讓資金在多頭也能部署。
	//   BullBuyBand >0  :bull 時改為「今價 < 進場均線×(1+BullBuyBand)」才買 (bear 仍嚴格 <均線)。
	//   BullCooldownDays>0:bull 用此冷卻;<=0 沿用 CooldownDays。
	//   BullBuyAmount>0 :bull 時買入金額固定為此值 (×multiplier,不走 depth 表);<=0 = 走 depth 表。
	BullBuyBand      float64 `yaml:"bull_buy_band"`
	BullCooldownDays int     `yaml:"bull_cooldown_days"`
	BullBuyAmount    float64 `yaml:"bull_buy_amount"`

	// 幾何加碼曲線:BuyBaseAmount>0 且 BuyTierRatio>0 時,各 tier 金額 = base×ratio^i
	// (i=命中 tier 索引,跌破最深 tier 用 ratio^len 當 fallback),沿用 BaselineBuyTiers 的 above 邊界。
	BuyBaseAmount float64 `yaml:"buy_base_amount"`
	BuyTierRatio  float64 `yaml:"buy_tier_ratio"`

	// 動態部位大小 (金字塔形狀不變,只縮放絕對額):
	//   "" / "fixed":固定金額。"cash":按 現金/初始現金 縮放。"equity":按 總權益/初始現金 縮放 (複利)。
	BuySizeMode string `yaml:"buy_size_mode"`

	// 賣出:獲利了結 (可分批) + 熊市移動停利。
	//   SellFracOfPosition>0:獲利了結改賣「當前持股的此比例」,取代固定 BaselineSellAmount。
	//   TrailStopBear>0     :熊市移動停利 — 價 <= 持倉期間峰值×(1-trail) 時全數出場。
	//   TrailMinGain        :移動停利僅在 (峰值/最低成本-1) >= 此值後才武裝 (不停損逢低買進)。
	SellFracOfPosition float64 `yaml:"sell_frac_of_position"`
	TrailStopBear      float64 `yaml:"trail_stop_bear"`
	TrailMinGain       float64 `yaml:"trail_min_gain"`
}

// MAWindowOrDefault 回傳實際使用的進場均線長度 (<=0 時為 20)。
func (c *Config) MAWindowOrDefault() int {
	if c.MAWindow <= 0 {
		return 20
	}
	return c.MAWindow
}

// Load 讀取指定路徑的 yaml 設定檔。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}

	if c.ScalingStrategy == "" {
		c.ScalingStrategy = "Baseline"
	}
	if c.BuyAndSellMultiplier == 0 {
		c.BuyAndSellMultiplier = 1.0
	}
	if c.CooldownDays <= 0 {
		c.CooldownDays = 14
	}
	if c.MaxBackMonths < 0 {
		c.MaxBackMonths = 1
	}
	if c.InitDBBackMonths <= 0 {
		c.InitDBBackMonths = c.MaxBackMonths
	}
	if len(c.TrackStocks) == 0 {
		return nil, fmt.Errorf("track_stocks must not be empty")
	}

	// 跨欄位 sanity check:back_testing_months 不能超過 init_db_back_months,
	// 否則 backtest 會靜默退化成「DB 有多少資料就跑多少」,讓使用者誤以為跑滿了 N 個月。
	if c.BackTestingMonths > 0 && c.BackTestingMonths > c.InitDBBackMonths {
		return nil, fmt.Errorf(
			"back_testing_months=%d 超過 init_db_back_months=%d;請增加 init_db_back_months 或降低 back_testing_months",
			c.BackTestingMonths, c.InitDBBackMonths)
	}

	return &c, nil
}
