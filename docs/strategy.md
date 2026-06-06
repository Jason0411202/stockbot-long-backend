# 買賣邏輯

> **最新且完整的交易規則 (牛熊 regime 感知、全程現金比例) 見 [optimization/BEST-STRATEGY.md](optimization/BEST-STRATEGY.md)。**
> 本頁下方「加減碼邏輯」段落為早期固定金額金字塔版的歷史說明,現行策略已改為現金比例,請以 BEST-STRATEGY.md 為準。

## 問題設定 (problem setting):每月解鎖新資金

回測 / 評估的資金情境為**定期定額注資**:期初 `initial_cash` (100,000),之後在「每個日曆月第一個交易日」
再注入 `monthly_contribution` (2,500) 可動用資金 (起始月除外)。因有持續外部注資,報酬用資金加權 (MWR/XIRR)、
回撤用 NAV 單位淨值。`monthly_contribution=0` 即退化回「期初一次性資金」舊行為。詳見 [backtest.md](backtest.md)。
注資僅作用於回測 / 評估;上線交易的真實餘額由 BotState 還原,不在此自動注資。

* 主攻台股 ETF (006208, 00830) 長線 + 波段交易
* 每檔股票各自有自己的冷卻期，彼此獨立計算
* 買入與賣出皆以「股數」為基本交易單位：將目標金額換算成最接近的股數 (四捨五入) 後實際下單
* 當股價來到 20 MA 以下時執行買入操作，買入股數由 baseline 加減碼邏輯 + 目前單價計算得出，該股票買入後 `cooldown_days` 天內不再買入
* 當某支追蹤的股票，其最低購買價的獲利超過 `baseline_sell_threshold` (100%) 時，執行賣出操作，賣出股數由賣出金額換算得出，本操作沒有冷卻

## 加減碼邏輯

### Baseline method (原金字塔策略)

目前專案中唯一的交易策略。`scaling_strategy` 只接受 `Baseline`；此策略在舊版本中稱為「金字塔策略 (Pyramid)」。

* 買入金額按照當前股價相對於持有中最高買入價的比例，越低買越多 (預設 tiers 見 `config.yaml`)：
  * -10% 內：500
  * -20% 內：750
  * -30% 內：1300
  * -40% 內：2000
  * 超過 -40%：3000 (`baseline_buy_fallback_amount`)
* 觸發賣出時的目標金額為 `baseline_sell_amount` (預設 10000)，實際會乘以 `buy_and_sell_multiplier`
* 所有金額最後都會除以當天股價並四捨五入得到「實際買/賣股數」

## 資金安全 (no-borrow 不變量)

回測 / 模擬交易的可利用資金**僅等於當前持有現金** (起始現金 + 已實現賣出收入 − 已支出買入成本)，
不得借錢、也不允許透支。實作上在 `runBacktestOnSeries` 中以下列方式防守：

1. 每次買入前計算 `maxAffordable = floor(cash / price)`。若策略目標股數超過 `maxAffordable`，則夾取到 `maxAffordable`。
2. 若夾取後仍為 0，則跳過該次買進並累計到 `SkippedBuys`。
3. 任何時刻若 `cash < 0`，回測立即以錯誤中止 (不變量違反)。
