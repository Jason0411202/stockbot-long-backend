package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BaselineBuyTier baseline 策略中的單一加碼級距。
// 當 (今日股價 - 持有最高買入價) / 持有最高買入價 > Above 時，便以此級距的金額買入，
// 規則由淺至深檢查，遇到第一筆滿足的即採用；若沒有滿足者，則採用 BaselineBuyFallbackAmount。
type BaselineBuyTier struct {
	Above  float64 `yaml:"above"`
	Amount float64 `yaml:"amount"`
}

// Config 為不私密的超參數，由 config.yaml 讀入後供全 app 使用。
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
	// 兩者同單位 (月),比較完全精確,不需要估算每月交易日數。
	if c.BackTestingMonths > 0 && c.BackTestingMonths > c.InitDBBackMonths {
		return nil, fmt.Errorf(
			"back_testing_months=%d 超過 init_db_back_months=%d;請增加 init_db_back_months 或降低 back_testing_months",
			c.BackTestingMonths, c.InitDBBackMonths)
	}

	return &c, nil
}
