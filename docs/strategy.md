# 買賣邏輯

> **最新且完整的交易規則 (牛熊 regime 感知、全程現金比例) 見 [optimization/BEST-STRATEGY.md](optimization/BEST-STRATEGY.md) 與專案 README。**
> 早期「固定金額金字塔」版本 (`buy_and_sell_multiplier`、`baseline_buy_fallback_amount`、`bull_buy_amount`、`buy_base_amount`、`buy_size_mode`、`baseline_sell_amount`) 已**從程式碼移除**,現行只保留現金比例路徑。

## 問題設定 (problem setting):每月解鎖新資金

回測 / 評估的資金情境為**定期定額注資**:期初 `initial_cash` (100,000),之後在「每個日曆月第一個交易日」
再注入 `monthly_contribution` (2,500) 可動用資金 (起始月除外)。因有持續外部注資,報酬用資金加權 (MWR/XIRR)、
回撤用 NAV 單位淨值。`monthly_contribution=0` 即退化回「期初一次性資金」舊行為。詳見 [backtest.md](backtest.md)。
注資僅作用於回測 / 評估;上線交易的真實餘額由 BotState 還原,不在此自動注資。

* 主攻台股 ETF (00631L, 00830) 長線 + 波段交易
* 每檔股票各自有自己的冷卻期，彼此獨立計算
* 買入與賣出皆以「股數」為基本交易單位：將目標金額換算成最接近的股數 (四捨五入) 後實際下單
* 先以 regime 均線判斷牛/熊,再決定進場閾值與買入比例 (詳見 BEST-STRATEGY.md)，該股票買入後 `cooldown_days` 天內不再買入 (另給「打破冷卻」額度)
* 當某支追蹤的股票，其最低購買價的獲利超過 `baseline_sell_threshold` (100%,僅多頭) 時分批獲利了結；熊市則以移動停利出場，皆無冷卻

## 加減碼邏輯 (現行:全程現金比例)

目前專案中唯一的交易策略。`scaling_strategy` 只接受 `Baseline`。買賣金額一律為「基準的固定比例」而非固定金額：

* **買入金額**：基準 (`buy_frac_basis`,定版 `cash` = 當前現金) 乘上比例 —
  * 牛市 = 現金 × `bull_buy_frac` (定版 0.20)。
  * 熊市 = 現金 × `bear_buy_frac` (定版 0.02) × 幾何深度權重 (`buy_tier_ratio`^命中 `baseline_buy_tiers` 索引;跌越深買越大比例)。
  * 花固定比例的現金在數學上永遠歸不了零 → 深跌時仍有銀彈。
* **賣出股數**：獲利了結賣「當前持股的 `sell_frac_of_position`」(定版 0.33);熊市移動停利則全數出場。
* 目標金額最後除以當天股價、四捨五入得到「實際買/賣股數」，再經 no-borrow 夾取 (見下)。

## 資金安全 (no-borrow 不變量)

回測 / 模擬交易的可利用資金**僅等於當前持有現金** (起始現金 + 已實現賣出收入 − 已支出買入成本)，
不得借錢、也不允許透支。實作上在引擎買入套用 (`applyBuy`) 時以下列方式防守：

1. 每次買入前計算 `maxAffordable = floor(cash / price)`。若策略目標股數超過 `maxAffordable`，則夾取到 `maxAffordable`。
2. 若夾取後仍為 0，則跳過該次買進並累計到 `SkippedBuys`。
3. 任何時刻若 `cash < 0`，回測立即以錯誤中止 (不變量違反)。
