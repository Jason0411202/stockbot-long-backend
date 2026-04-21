package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PyramidBuyTier 金字塔策略中的單一加碼級距。
// 當 (今日股價 - 持有最高買入價) / 持有最高買入價 > Above 時，便以此級距的金額買入，
// 規則由淺至深檢查，遇到第一筆滿足的即採用；若沒有滿足者，則採用 PyramidBuyFallbackAmount。
type PyramidBuyTier struct {
	Above  float64 `yaml:"above"`
	Amount float64 `yaml:"amount"`
}

// Config 為不私密的超參數，由 config.yaml 讀入後供全 app 使用。
type Config struct {
	TrackStocks              []string         `yaml:"track_stocks"`
	ScalingStrategy          string           `yaml:"scaling_strategy"`
	BuyAndSellMultiplier     float64          `yaml:"buy_and_sell_multiplier"`
	MaxBackMonths            int              `yaml:"max_back_months"`
	BackTestingDays          int              `yaml:"back_testing_days"`
	CooldownDays             int              `yaml:"cooldown_days"`
	PyramidBuyTiers          []PyramidBuyTier `yaml:"pyramid_buy_tiers"`
	PyramidBuyFallbackAmount float64          `yaml:"pyramid_buy_fallback_amount"`
	PyramidSellAmount        float64          `yaml:"pyramid_sell_amount"`
	PyramidSellThreshold     float64          `yaml:"pyramid_sell_threshold"`
	InitialCash              float64          `yaml:"initial_cash"`
	InitDBBackMonths         int              `yaml:"init_db_back_months"`

	// --- Plan v1: Trend-Following 加碼分支（預設關閉，開啟後才進入新路徑）---
	// UseTFBranch=false 時，所有 TF 參數皆不會影響行為，baseline 數值保持不變。
	UseTFBranch  bool    `yaml:"use_tf_branch"`
	TFTau        float64 `yaml:"tf_tau"`         // 多頭判定閾值: MA20 > (1+tau)*MA60
	TFAmountMode string  `yaml:"tf_amount_mode"` // "const" or "cashfrac"
	TFAlpha      float64 `yaml:"tf_alpha"`       // const 模式：乘以最大 Pyramid tier 的倍率
	TFBeta       float64 `yaml:"tf_beta"`        // cashfrac 模式：佔當下現金比例
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
		c.ScalingStrategy = "Pyramid"
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

	return &c, nil
}
