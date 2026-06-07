// internal/config/config.go 定義 config.yaml 的資料模型與載入驗證流程。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BaselineBuyTier 為熊市加碼深度的單一級距邊界。
// 當「跌幅深度判斷值」> Above 時命中此級距 (由淺至深取第一筆滿足者),
// 用來決定 bearDepthWeight 的幾何權重 (ratio^命中索引) → 跌越深、買入現金比例放越大。
type BaselineBuyTier struct {
	Above float64 `yaml:"above"` // 深度門檻;跌幅超過此值即命中本級距
}

// Config 為不私密的超參數，由 config.yaml 讀入後供全 app 使用。
//
// 「牛熊 regime 感知逢低加碼」策略的旋鈕 (見 docs/optimization/BEST-STRATEGY.md)。
// 每個旋鈕都經 walk-forward 掃描驗證採納;預設零值 = 原始 Baseline 行為,既有測試不受影響。
type Config struct {
	TrackStocks           []string          `yaml:"track_stocks"`
	ScalingStrategy       string            `yaml:"scaling_strategy"`
	MaxBackMonths         int               `yaml:"max_back_months"`
	BackTestingMonths     int               `yaml:"back_testing_months"`
	CooldownDays          int               `yaml:"cooldown_days"`
	BaselineBuyTiers      []BaselineBuyTier `yaml:"baseline_buy_tiers"`
	BaselineSellThreshold float64           `yaml:"baseline_sell_threshold"`
	InitialCash           float64           `yaml:"initial_cash"`
	InitDBBackMonths      int               `yaml:"init_db_back_months"`

	// ── 問題設定 (problem setting):定期定額注資 ──
	// 除了期初 InitialCash 外,回測 / 評估會在「每個日曆月的第一個交易日」再注入 MonthlyContribution 元
	// 可動用資金 (起始月不注入)。模擬「每月解鎖一筆新資金」的真實使用情境。
	//   - 因有持續外部注資,報酬一律用資金加權 (XIRR / MWR),回撤用 NAV/單位淨值 (扣除注資的真實投資回撤)。
	//   - <=0 視為關閉 (退化回「期初一次性資金、無注資」的舊行為,所有指標與舊版一致)。
	//   - 僅作用於回測 / 評估;上線交易的真實餘額仍由 BotState 還原,不在此自動注資。
	MonthlyContribution float64 `yaml:"monthly_contribution"`

	// 進場均線長度。<=0 視為 20。
	MAWindow int `yaml:"ma_window"`

	// 決策成交價基準:決定「當日用什麼價格成交」與「指標可見到哪一天的收盤」。
	//   "" / "close":當日收盤價成交,指標含當日收盤 (盤後決策的舊行為)。
	//   "open"      :當日開盤價成交,指標只看到前一交易日 (含) 的收盤 (開盤即時決策,無未來資訊)。
	// 線上與回測共用同一基準,使回測能忠實預測線上行為。
	DecisionPriceBasis string `yaml:"decision_price_basis"`

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

	// 牛市行為:放寬進場,讓資金在多頭也能部署。
	//   BullBuyBand >0  :bull 時改為「今價 < 進場均線×(1+BullBuyBand)」才買 (bear 仍嚴格 <均線)。
	//   BullCooldownDays>0:bull 用此冷卻;<=0 沿用 CooldownDays。
	BullBuyBand      float64 `yaml:"bull_buy_band"`
	BullCooldownDays int     `yaml:"bull_cooldown_days"`

	// BuyTierRatio:熊市加碼幾何權重底數,深度權重 = ratio^命中 tier 索引 (跌越深買越大比例)。
	BuyTierRatio float64 `yaml:"buy_tier_ratio"`

	// 賣出:獲利了結 (分批) + 熊市移動停利。
	//   SellFracOfPosition:獲利了結賣「當前持股的此比例」(分批出場)。
	//   TrailStopBear>0   :熊市移動停利 — 價 <= 持倉期間峰值×(1-trail) 時全數出場。
	//   TrailMinGain      :移動停利僅在 (峰值/最低成本-1) >= 此值後才武裝 (不停損逢低買進)。
	SellFracOfPosition float64 `yaml:"sell_frac_of_position"`
	TrailStopBear      float64 `yaml:"trail_stop_bear"`
	TrailMinGain       float64 `yaml:"trail_min_gain"`

	// ── 部位大小:買入「現金 / 權益基準的固定比例」(像獲利了結賣固定比例那樣) ──
	//   BuyFracBasis: "cash" = 比例基準為現金 (定版);"equity" = 比例基準為總權益。
	//   BullBuyFrac:牛市買入金額 = BuyFracBasis 基準 × BullBuyFrac。
	//   此「現金比例」會隨現金遞減而自然減速,是把回撤壓在預算內的關鍵煞車。
	BuyFracBasis string  `yaml:"buy_frac_basis"`
	BullBuyFrac  float64 `yaml:"bull_buy_frac"`

	// ── Idea 2 (採納):打破冷卻額度 (滾動視窗) ──
	//   每檔在「近 CooldownBreakWindowDays 日曆日」內,最多可動用 CooldownBreakBudget 次「無視冷卻」提前買入,
	//   撿回被冷卻期錯過的深跌買點。改用滾動視窗 (取代舊的「每 engine 生命週期 N 次」),讓回測/連續/上線三模式語意一致。
	CooldownBreakBudget     int `yaml:"cooldown_break_budget"`
	CooldownBreakWindowDays int `yaml:"cooldown_break_window_days"` // 滾動視窗 (日曆日);<=0 視為 365 (≈252 交易日≈1 年)

	// ── 熊市也用「現金比例」買入,根治「深跌時沒現金」(實測把深跌沒錢 79→0) ──
	//   BearBuyFrac:熊市買入 = 現金 × BearBuyFrac × 幾何深度權重 (ratio^i)。需搭配 BuyFracBasis。
	//   花現金的固定比例在數學上永遠歸不了零 → 每個更深更便宜的跌點都還有錢買。
	BearBuyFrac float64 `yaml:"bear_buy_frac"`

	// ── Per-stock 量身訂做:個股可覆寫部分旋鈕 (其餘繼承上面的共用值) ──
	//   每支股票性質不同 (如槓桿倍數),可各自設最適參數。空 map = 全部共用 (與舊行為一致)。
	//   ⚠️ per-stock 調參極易過擬合 → 採用前務必用 IS/OOS (cmd/eval_csv -split) 確認 OOS 不退化。
	StockOverrides map[string]StockParams `yaml:"stock_overrides"`
}

// StockParams 為單一個股可覆寫的策略旋鈕 (指標型;nil = 繼承共用值,YAML 省略即 nil)。
type StockParams struct {
	MAWindow              *int     `yaml:"ma_window"`               // 覆寫進場均線長度
	RegimeMAWindow        *int     `yaml:"regime_ma_window"`        // 覆寫牛熊判定均線長度
	BullBuyBand           *float64 `yaml:"bull_buy_band"`           // 覆寫牛市買入帶寬
	CooldownDays          *int     `yaml:"cooldown_days"`           // 覆寫冷卻天數
	BullCooldownDays      *int     `yaml:"bull_cooldown_days"`      // 覆寫牛市冷卻天數
	BullBuyFrac           *float64 `yaml:"bull_buy_frac"`           // 覆寫牛市買入現金比例
	BearBuyFrac           *float64 `yaml:"bear_buy_frac"`           // 覆寫熊市買入現金比例
	BuyTierRatio          *float64 `yaml:"buy_tier_ratio"`          // 覆寫加碼幾何權重底數
	BaselineSellThreshold *float64 `yaml:"baseline_sell_threshold"` // 覆寫獲利了結觸發門檻
	SellFracOfPosition    *float64 `yaml:"sell_frac_of_position"`   // 覆寫獲利了結賣出比例
	TrailStopBear         *float64 `yaml:"trail_stop_bear"`         // 覆寫熊市移動停利回撤幅度
	TrailMinGain          *float64 `yaml:"trail_min_gain"`          // 覆寫移動停利啟動最低獲利門檻
}

// ForStock 回傳「套用該股 override 後」的有效設定。無 override 時回傳原指標 (零成本)。
// 回傳的是淺拷貝 (slice / map 欄位共享,皆唯讀);決策端不得改動回傳的 Config。
func (c *Config) ForStock(stockID string) *Config {
	ov, ok := c.StockOverrides[stockID]
	if !ok {
		return c
	}
	cp := *c
	if ov.MAWindow != nil {
		cp.MAWindow = *ov.MAWindow
	}
	if ov.RegimeMAWindow != nil {
		cp.RegimeMAWindow = *ov.RegimeMAWindow
	}
	if ov.BullBuyBand != nil {
		cp.BullBuyBand = *ov.BullBuyBand
	}
	if ov.CooldownDays != nil {
		cp.CooldownDays = *ov.CooldownDays
	}
	if ov.BullCooldownDays != nil {
		cp.BullCooldownDays = *ov.BullCooldownDays
	}
	if ov.BullBuyFrac != nil {
		cp.BullBuyFrac = *ov.BullBuyFrac
	}
	if ov.BearBuyFrac != nil {
		cp.BearBuyFrac = *ov.BearBuyFrac
	}
	if ov.BuyTierRatio != nil {
		cp.BuyTierRatio = *ov.BuyTierRatio
	}
	if ov.BaselineSellThreshold != nil {
		cp.BaselineSellThreshold = *ov.BaselineSellThreshold
	}
	if ov.SellFracOfPosition != nil {
		cp.SellFracOfPosition = *ov.SellFracOfPosition
	}
	if ov.TrailStopBear != nil {
		cp.TrailStopBear = *ov.TrailStopBear
	}
	if ov.TrailMinGain != nil {
		cp.TrailMinGain = *ov.TrailMinGain
	}
	return &cp
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

	// 決策成交價基準:空字串退化為 "close"(舊盤後行為);只接受 close / open,其餘 fail-fast。
	if c.DecisionPriceBasis == "" {
		c.DecisionPriceBasis = "close"
	}
	if c.DecisionPriceBasis != "close" && c.DecisionPriceBasis != "open" {
		return nil, fmt.Errorf("decision_price_basis 只接受 \"close\" 或 \"open\",得 %q", c.DecisionPriceBasis)
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

	// 現行 Baseline 策略一律走「現金比例」買賣;缺這些旋鈕會讓策略「靜默不交易」(買 0 股 / 不賣),
	// 故在載入時就 fail-fast,避免上線後才發現整天沒下任何單。
	if c.ScalingStrategy == "Baseline" {
		if c.BuyFracBasis == "" || c.BullBuyFrac <= 0 || c.BearBuyFrac <= 0 || c.SellFracOfPosition <= 0 {
			return nil, fmt.Errorf(
				"Baseline 策略需設定 buy_frac_basis / bull_buy_frac / bear_buy_frac / sell_frac_of_position (現金比例買賣);"+
					"得 buy_frac_basis=%q bull_buy_frac=%g bear_buy_frac=%g sell_frac_of_position=%g",
				c.BuyFracBasis, c.BullBuyFrac, c.BearBuyFrac, c.SellFracOfPosition)
		}
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
