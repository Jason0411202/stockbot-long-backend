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

	// ───────────────────────────────────────────────────────────────────────
	// 以下為「可選的策略優化旋鈕」。全部預設為零值 = 關閉 = 與原始 Baseline 行為完全相同,
	// 因此既有測試與上線行為不受影響。由 cmd/optimize 的參數掃描逐一啟用 / 校準。
	// 每個旋鈕都是純函式 DecideBuy / DecideSell (或引擎現金夾取) 上的加法式 gate。
	// ───────────────────────────────────────────────────────────────────────

	// 進場均線長度。<=0 視為 20 (預設,與原始硬編碼一致)。對應方向 #4/#68。
	MAWindow int `yaml:"ma_window"`

	// 長期趨勢均線過濾。BuyLongMAWindow<=0 表示關閉整組長均線濾網。對應方向 #5/#30/#80。
	BuyLongMAWindow         int  `yaml:"buy_long_ma_window"`           // 例如 120 / 200
	BuyRequireAboveLongMA   bool `yaml:"buy_require_above_long_ma"`    // 僅在 收盤 > 長均線 時才允許加碼 (多頭硬濾網)
	BuyRequireLongMASlopeUp bool `yaml:"buy_require_long_ma_slope_up"` // 僅在 長均線斜率>=0 時才允許加碼
	BuyLongMASlopeLookback  int  `yaml:"buy_long_ma_slope_lookback"`   // 斜率回看交易日;<=0 視為 20

	// 乖離率最小深度門檻:僅在 (進場均線-收盤)/進場均線 >= BuyBiasMin 時才買。<=0 關閉。對應 #8/#17。
	BuyBiasMin float64 `yaml:"buy_bias_min"`

	// RSI 進場 gate:僅在 RSI(BuyRSIPeriod) <= BuyRSIMax 時才買。BuyRSIPeriod<=0 關閉。對應 #34。
	BuyRSIPeriod int     `yaml:"buy_rsi_period"`
	BuyRSIMax    float64 `yaml:"buy_rsi_max"`

	// 止穩確認:僅在 今日收盤 > 昨日收盤 時才買 (避免接刀)。對應 #33。
	BuyConfirmUp bool `yaml:"buy_confirm_up"`

	// RSI 出場 gate:僅在 RSI(SellRSIPeriod) >= SellRSIMin (超買) 時才賣。SellRSIPeriod<=0 關閉。對應 #39/#94。
	SellRSIPeriod int     `yaml:"sell_rsi_period"`
	SellRSIMin    float64 `yaml:"sell_rsi_min"`

	// 現金下限:保留 CashFloorFrac × InitialCash 不部署 (dry powder)。<=0 關閉。對應 #22。
	CashFloorFrac float64 `yaml:"cash_floor_frac"`

	// 買入金額曲線幾何化 (#9/#49):當 BuyBaseAmount>0 且 BuyTierRatio>0 時,
	// 各 tier 金額改用幾何級數 base×ratio^i (i=tier 索引, fallback 用 ratio^len),
	// 取代 config 中手寫的 4 個 tier 金額 + fallback。boundaries (above) 仍沿用 BaselineBuyTiers。
	// ratio=1 表示「不論跌多深都買一樣多」(平坦);ratio>1 表示「跌越深買越多」(金字塔)。
	BuyBaseAmount float64 `yaml:"buy_base_amount"`
	BuyTierRatio  float64 `yaml:"buy_tier_ratio"`

	// ── 牛熊 regime 感知 (實驗中) ──────────────────────────────────────────
	// 買入加碼「深度基準」:決定 baseline tier 的判斷值要拿什麼當「跌多深」。
	//   "" / "held_high":(今價-持倉最高買入價)/持倉最高買入價 (原始,較粗糙)
	//   "ma"            :(今價-進場均線)/進場均線 — 乖離率,市場相對均線的折價
	//   "peak"          :(今價-近 BuyPeakLookback 日最高價)/最高價 — 距近期高點的回撤
	BuyDepthBasis   string `yaml:"buy_depth_basis"`
	BuyPeakLookback int    `yaml:"buy_peak_lookback"` // peak 基準回看交易日;<=0 視為 252

	// 牛熊判定方法:決定每個交易日屬於 bull 或 bear。"" 表示關閉 (永遠視為 bear/中性,行為不變)。
	//   "ma_pos"  :收盤 > RegimeMAWindow 日均線 → bull
	//   "ma_slope":RegimeMAWindow 日均線 > RegimeLookback 日前的同均線 → bull (趨勢向上)
	//   "mom"     :收盤 > RegimeLookback 日前的收盤 → bull (絕對動能為正)
	RegimeMethod   string `yaml:"regime_method"`
	RegimeMAWindow int    `yaml:"regime_ma_window"` // <=0 視為 200
	RegimeLookback int    `yaml:"regime_lookback"`  // slope/mom 回看交易日;<=0 視為 200/252

	// 牛市行為:bull regime 時放寬進場,讓資金在多頭也能部署 (解決「多頭抱現金、利用率低」)。
	//   BullBuyBand>0:bull 時改為「今價 < 進場均線×(1+BullBuyBand)」才買 — band 越大越容易買,
	//                 甚至允許站上均線一點點也買 (順勢)。bear 仍維持嚴格「今價<均線」。
	//   BullCooldownDays>0:bull 時改用較短的冷卻天數 (買更勤);<=0 沿用 CooldownDays。
	BullBuyBand      float64 `yaml:"bull_buy_band"`
	BullCooldownDays int     `yaml:"bull_cooldown_days"`

	// ── 賣出端 regime 切換 (實驗;賣點好壞大幅影響回撤) ─────────────────────
	// 牛熊不同的「獲利出場門檻」(gain 相對持倉最低成本)。<=0 表示沿用 BaselineSellThreshold。
	//   牛市可設較高 (讓贏家多跑);熊市可設較低 (提早鎖利,少還給市場 → 降回撤)。
	SellThresholdBull float64 `yaml:"sell_threshold_bull"`
	SellThresholdBear float64 `yaml:"sell_threshold_bear"`

	// 牛熊不同的「移動停利」:價 <= 持倉期間峰值×(1-trail) 時「全數出場」(保護式)。<=0 表示關閉。
	//   熊市用緊一點 (early 保護、砍回撤);牛市用鬆一點或關閉 (不被洗掉、續抱趨勢)。
	TrailStopBull float64 `yaml:"trail_stop_bull"`
	TrailStopBear float64 `yaml:"trail_stop_bear"`
	// 移動停利「武裝門檻」:僅當 (峰值/持倉最低成本 - 1) >= TrailMinGain 時才啟動移動停利,
	// 避免在尚未獲利時就把逢低買進的部位停損掉 (與「買最深的回檔」哲學衝突)。<=0 表示一律啟動。
	TrailMinGain float64 `yaml:"trail_min_gain"`

	// 牛市「固定大額買入」(#解決多頭只買最小額→靠高頻交易→手續費的問題):
	// bull regime 且 BullBuyAmount>0 時,買入金額固定為 BullBuyAmount (不走 depth 表),
	// 搭配較長的 BullCooldownDays → 用「少次、大額」部署,取代「多次、小額」,大幅降低換手與手續費。
	// bear 仍走 depth 表 (越深越大),不受影響。<=0 表示關閉 (bull 也走 depth 表)。
	BullBuyAmount float64 `yaml:"bull_buy_amount"`

	// 砍倉 (停損) — 多形態:CutLossPct>0 啟用,虧損達門檻就砍。
	//   CutPerLot   :true = 只砍「該筆相對自己買價虧損達門檻的 lot」(留住便宜的好倉);
	//                 false = 整倉相對「加權平均成本」虧損達門檻時全數出場。
	//   CutBearOnly :true = 只在空頭砍 (多頭反彈機會大,不砍);false = 牛熊皆砍。
	CutLossPct  float64 `yaml:"cut_loss_pct"`
	CutPerLot   bool    `yaml:"cut_per_lot"`
	CutBearOnly bool    `yaml:"cut_bear_only"`

	// 買入金額計算基準 (金字塔形狀不變,只變「絕對額 vs 動態比例」):
	//   "" / "fixed":固定金額 (金字塔×multiplier,目前作法,不隨帳戶大小變)。
	//   "cash"      :把金額按 (當前現金 / 初始現金) 等比縮放 — 現金多買多、少買少,自我節制。
	//   "equity"    :把金額按 (當前總權益 / 初始現金) 等比縮放 — 隨帳戶成長複利放大買入。
	BuySizeMode string `yaml:"buy_size_mode"`

	// 分批出場 (取代「一次全賣 / 賣固定金額」):
	//   SellFracOfPosition>0:獲利了結改為「賣掉當前持股的這個比例」(round(frac×持股)),取代固定 baseline_sell_amount。
	//   TrailStopSellFrac  >0:移動停利觸發時改為「賣掉這個比例」而非全數出場 (0 或 >=1 視為全賣)。
	SellFracOfPosition float64 `yaml:"sell_frac_of_position"`
	TrailStopSellFrac  float64 `yaml:"trail_stop_sell_frac"`

	// 賣出階梯 (取代固定獲利了結):依「相對均價的獲利」分 +50/+100/+150/+200% 四級,各級觸發一次。
	//   "pyramid":各級賣當前持股 10%/20%/35%/50% — 越漲越賣多 (賣在強勢,留少量續抱)。
	//   "inverse":各級賣 50%/30%/20%/15% — 越早賣越多 (提早鎖利,留尾巴讓利潤奔跑)。
	// "" = 關閉,沿用固定金額/比例的獲利了結。
	SellLadderMode string `yaml:"sell_ladder_mode"`
}

// MAWindowOrDefault 回傳實際使用的進場均線長度 (<=0 時為 20)。
func (c *Config) MAWindowOrDefault() int {
	if c.MAWindow <= 0 {
		return 20
	}
	return c.MAWindow
}

// LongMASlopeLookbackOrDefault 回傳長均線斜率回看天數 (<=0 時為 20)。
func (c *Config) LongMASlopeLookbackOrDefault() int {
	if c.BuyLongMASlopeLookback <= 0 {
		return 20
	}
	return c.BuyLongMASlopeLookback
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
