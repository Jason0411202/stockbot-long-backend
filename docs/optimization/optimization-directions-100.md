# Baseline 逢低加碼策略 — 100 個可回測優化方向

> **唯一目標**：衝高回測表現（walk-forward 的中位 CAGR / 資金加權 XIRR、降低 MaxDD、提升 Calmar 與真擇時勝率、通過更多 G1~G5 關卡）。
> **硬限制**：每個方向都做到「簡單、不會 overfit、可用既有 walk-forward + benchmark 框架 A/B 或掃描驗證」。
> 素材來源見 [trading-knowledge.md](trading-knowledge.md)（20 類 / 336 個交易知識點）。

**統計**：優先級 high 30 / medium 61 / low 9；過擬合風險 low 61 / medium 39。

**類別分布**：
- 參數掃描：15
- 買入進場：13
- 賣出出場：13
- 部位規模：9
- 冷卻頻率：10
- 市場濾網：15
- 風險回撤：13
- 資金部署：12

## ⭐ 高優先速覽（priority = high）

| # | 類別 | 標題 | 過擬合 |
| --- | --- | --- | --- |
| 1 | 參數掃描 | 建立可重用 grid sweep harness | low |
| 2 | 賣出出場 | 目標報酬門檻掃描找高原 | low |
| 3 | 冷卻頻率 | cooldown_days 全域掃描找高原 | low |
| 4 | 買入進場 | MA 長度掃描(固定20改可調參數) | low |
| 5 | 風險回撤 | MA200 長線多空硬濾網(僅多頭加碼) | low |
| 6 | 市場濾網 | MA200 空頭縮碼軟濾網 | low |
| 7 | 風險回撤 | 災難停損出場(取代死抱) | medium |
| 8 | 買入進場 | 乖離率(BIAS)最小深度門檻 | low |
| 9 | 部位規模 | 金額曲線幾何化(2旋鈕取代4手調金額) | low |
| 10 | 部位規模 | 現金緩衝感知的金額縮放(降 SkippedBuys) | low |
| 11 | 資金部署 | 定期定額底倉+逢低加碼混合(拉高曝險攻G4) | low |
| 12 | 賣出出場 | 峰值回撤移動停利(trailing stop) | medium |
| 13 | 賣出出場 | 跌破 MA20 觸發停利(零新指標) | low |
| 14 | 參數掃描 | Calmar 為主、風險gate為硬約束的複合目標函式 | low |
| 15 | 資金部署 | 回收現金即時再部署(賣出所得不閒置) | low |
| 16 | 部位規模 | 分數Kelly/上限縮放取代固定multiplier=2.0 | low |
| 17 | 冷卻頻率 | 買入no-trade band(乖離不夠不進場) | low |
| 18 | 風險回撤 | 權益曲線剎車(回撤過深暫停加碼) | medium |
| 19 | 參數掃描 | 前段IS挑參數、後段OOS驗證 | low |
| 20 | 資金部署 | 處理順序去偏:依當日折扣深度排序配現金 | low |
| 21 | 部位規模 | 買賣multiplier分離(獨立調節加碼/出場速度) | low |
| 22 | 風險回撤 | 現金下限保留(dry-powder reserve) | low |
| 23 | 風險回撤 | 單檔曝險上限(per-name position cap) | low |
| 24 | 買入進場 | 連跌N日後暫停一日(避免接刀) | low |
| 25 | 參數掃描 | 成本敏感度掃描(加手續費/證交稅後重掃) | low |
| 26 | 資金部署 | exposure-matched底倉地板(直攻G4防抱現金) | medium |
| 27 | 風險回撤 | 分批停利階梯(取代單一+100%) | medium |
| 28 | 參數掃描 | 2D粗網格掃(cooldown×sell_threshold)找盆地 | low |
| 29 | 冷卻頻率 | 最小加碼間距改用價格步長 | low |
| 30 | 市場濾網 | MA斜率濾網(均線非陡降才買) | low |

## 全部 100 個方向（依類別）

### 參數掃描（15）

#### #1 建立可重用 grid sweep harness  `priority:high` `overfit:low`
- **改動**：在 cmd/evaluate 加 -sweep 模式:讀 sweep.yaml 定義參數與值域,for-loop 複製 cfg 改單一參數後呼叫 walkForwardOnSeries,輸出每組 AggregateReport 成 CSV。決策純函式完全不動。
- **預期衝高**：所有掃描方向的基礎設施,讓優化從猜數字變看曲線挑高原,間接支撐所有指標。
- **回測驗證**：先以 cooldown_days∈{7,10,14,21,28} 驗證輸出與手動 evaluate 一致。

#### #14 Calmar 為主、風險gate為硬約束的複合目標函式  `priority:high` `overfit:low`
- **改動**：sweep harness 加 scoreConfig 純函式:objective=median(Calmar),對未通過 G2/G5 的組合重罰或淘汰,平手用 XIRR/Sortino 當 tie-breaker。
- **預期衝高**：純最大化 CAGR/XIRR 會誘導過度曝險衝撞 G2/G5;以 Calmar 為目標+風險 gate 確保最佳點同時改善 MaxDD。
- **回測驗證**：對任一單參數掃描套用,確認選的點讓 G2/G3/G5 維持 PASS,與純挑最高 CAGR 對照。

#### #19 前段IS挑參數、後段OOS驗證  `priority:high` `overfit:low`
- **改動**：harness 加時間切分:用 common-support 前 60~70% 視窗挑參數(IS),保留後 30~40% 完全不參與,最後用挑出參數驗一次(OOS)。
- **預期衝高**：防過擬合核心:回報 OOS/IS 績效比,OOS 沒崩代表是真訊號而非曲線擬合,守護 G1~G5 可信度。
- **回測驗證**：每個掃描方向先 IS 選點再印 OOS 的 MedStratCAGR/Calmar 與比值(>0.6 視為穩健),崩潰者否決。

#### #25 成本敏感度掃描(加手續費/證交稅後重掃)  `priority:high` `overfit:low`
- **改動**：runStrategyWindow 的 OnCashflow 加 costBps 旗標(買賣各扣 0.1425%+賣方 0.1%),掃 costBps∈{0,實際,2x} 檢查最佳區間是否位移。
- **預期衝高**：策略換手率遠高於 B&H,gross 最佳點計入成本後可能改變,確保挑出的高原在 net 條件下仍站得住保護 G1/G3。
- **回測驗證**：對 cooldown 與 sell_threshold 各跑 cost=0 與實際兩版,比較最佳區間中心是否位移,採 net 高原中心。

#### #28 2D粗網格掃(cooldown×sell_threshold)找盆地  `priority:high` `overfit:low`
- **改動**：做兩參數 coarse grid(cooldown∈{7,14,21}×sell∈{0.75,1.0,1.5}),畫 Calmar 熱力圖找整片都不錯的盆地中心。
- **預期衝高**：單參數掃可能忽略交互作用,2D 盆地是更強穩健性證據,中心對 Calmar/勝率敏感度低過擬合風險小。
- **回測驗證**：9 格各記 MedStratCalmar 與 OverallPass,挑被高分鄰格包圍的中心格,檢查盆地邊緣不掉出 G2/G3。

#### #42 跨標的留一驗證(leave-one-stock-out)  `priority:medium` `overfit:low`
- **改動**：用 -stocks flag 分別跑{006208}、{00830}、{兩檔}三組同一參數,要求最佳參數在每檔都不顯著變差。
- **預期衝高**：baseline 參數可能只對某檔有利,要求對所有標的一致受益避免針對特定標的硬調,呼應 G4 真擇時。
- **回測驗證**：對候選最佳參數分別跑單檔與雙檔的 MedStratCalmar/CAGR,某檔大幅劣於 baseline 則否決。

#### #49 單旋鈕等比縮放買入tier陡度  `priority:medium` `overfit:medium`
- **改動**：引入 tierSlope 乘數:amount_i=base*tierSlope^i,用1個旋鈕掃 tierSlope∈{1.3,1.5,1.7,2.0,2.5} 控制跌越深加碼越兇的陡度。
- **預期衝高**：金字塔陡度影響攤平與曝險,單旋鈕掃比4個自由參數大幅降自由度降過擬合,找對 Calmar/XIRR 穩定的陡度區間。
- **回測驗證**：掃 tierSlope 5 組,看 MedStratXIRR/Calmar/AvgExposure,確認最佳點落平原中段對 baseline tier A/B。

#### #50 baseline_sell_threshold穩定出場門檻掃描  `priority:medium` `overfit:low`
- **改動**：掃 baseline_sell_threshold∈{0.5,0.75,1.0,1.25,1.5,2.0},觀察對 MedStratCAGR、換手、MedStratMDD、CalmarWinRate 影響挑高原。
- **預期衝高**：出場門檻決定兌現節奏,過低增換手成本過高等於不賣,掃描找對 Calmar/XIRR 穩定區間對應 G1/G3。
- **回測驗證**：6 組看 MedStratCAGR/XIRR/MDD/Sells,找相鄰點一致的平原取中心與 baseline(1.0) A/B。

#### #55 buy_and_sell_multiplier對SkippedBuys與曝險影響  `priority:medium` `overfit:low`
- **改動**：掃 multiplier∈{0.5,1.0,1.5,2.0,3.0},同表輸出 MedStratAvgExp、SkippedBuys、CAGR、MDD,找曝險夠高但 SkippedBuys 不爆衝的點。
- **預期衝高**：multiplier 過大撞現金上限(SkippedBuys≈252 根因)使曝險與決策脫節,掃描找曝險-XIRR 甜蜜區提升 G1 與 G4。
- **回測驗證**：5 組對照 AvgExp 與 SkippedBuys 拐點,選 SkippedBuys 急升前最大 multiplier 與 baseline A/B 比 G1/G4。

#### #57 DispersionStratCAGR為穩健性次要篩選  `priority:medium` `overfit:low`
- **改動**：scoreConfig 把既有 agg.DispersionStratCAGR 納入:Calmar 相近的候選中優先選 CAGR 跨視窗離散更小者作高原判定代理。
- **預期衝高**：低離散=參數對不同進場時點穩定=接近 plateau,當 tie-breaker 避免挑到報酬高但忽上忽下的脆弱點鞏固 G5。
- **回測驗證**：對任一掃描列 (MedStratCalmar,DispersionStratCAGR),在前段候選選離散最小者,對照純挑最高 Calmar 的最差視窗表現。

#### #64 設最少視窗/最少交易數的統計顯著性門檻  `priority:medium` `overfit:low`
- **改動**：harness 加守門:若某參數組合視窗數 NWindows 太少(<6)或總 Buys 太少,標記樣本不足、結論不可信而非照常報最佳。
- **預期衝高**：過短共同有效期下掃描易在 3~4 視窗挑出虛假最佳(資料探勘偏差),最小樣本門檻是最簡單有效的 overfit 紅旗防線。
- **回測驗證**：統計各組合 NWindows 與總交易數,NWindows<6 一律不納入最佳候選並報表標註,確認最終建議參數樣本充足。

#### #68 掃描MA視窗長度確認baseline=20非僥倖  `priority:medium` `overfit:medium`
- **改動**：把硬編 window=20 抽成 cfg.MABuyWindow,DecideBuy 比較 TodayPrice<MA,掃{10,15,20,30,40,60} 找對 Calmar 不敏感的平原。
- **預期衝高**：若 Calmar/CAGR 在一段視窗長度內都穩定代表訊號穩健(G3/G4),過度敏感則 baseline=20 可能僥倖,需驗證。
- **回測驗證**：grid 看 MedStratCalmar 與 BlendSkillRate 曲線是否平緩,挑高原中心並對 baseline=20 A/B。

#### #71 掃描walk-forward視窗設定本身的穩健性  `priority:medium` `overfit:low`
- **改動**：固定策略參數,改掃評估設定 window∈{18,24,36}、step∈{2,3,6}、min-days∈{150,200,250},看 OverallPass 與 Calmar 是否對視窗設定穩定。
- **預期衝高**：若結論只在 window=24/step=3 成立換視窗就翻盤代表是評估框架巧合,此掃描驗證結論對視窗不敏感是最直接的過擬合紅旗偵測。
- **回測驗證**：3x3x3 組合跑 baseline,統計 OverallPass 通過比例與 MedStratCalmar 離散,通過率高且離散小=穩健。

#### #75 Monte Carlo視窗重採樣評估參數穩定性  `priority:medium` `overfit:low`
- **改動**：harness 加重採樣:對既有 WindowReport 做 bootstrap(有放回抽樣N次),統計每組 MedStratCalmar 信賴區間與 OverallPass 機率,而非只看點估計。
- **預期衝高**：單點 scorecard 可能是少數視窗的運氣,bootstrap 給此參數通過 G1~G5 的機率與 Calmar 區間,挑高機率穩定通過者降過擬合。
- **回測驗證**：對 baseline 與候選各做 1000 次視窗 bootstrap,比 OverallPass 機率與 Calmar 5~95% 區間,偏好區間窄且下界高者。

#### #86 級距數量(粒度)掃描:3~8階等距  `priority:medium` `overfit:low`
- **改動**：保持等距邊界與單調遞增金額,僅把 tier 數量參數化(N階,邊界=-step*i,金額=base*growth^i),用同一公式生成掃 N。
- **預期衝高**：tier 數是訊號半衰期vs平滑度的旋鈕,太少台階跳動大太多對雜訊敏感,參數高原找穩定 N 避免硬定4階,優化 Calmar 穩定度與 CAGR 離散。
- **回測驗證**：固定 base/growth 掃 N∈{3,4,5,6,8} 與 step∈{0.05,0.08,0.10},看 DispersionStratCAGR、Calmar勝率是否對 N 平坦。

### 買入進場（13）

#### #4 MA 長度掃描(固定20改可調參數)  `priority:high` `overfit:low`
- **改動**：把 engine.go 寫死的 window=20 抽成 config 參數 buy_ma_period,loadStockSeries 依該值算 MA;DecideBuy 仍用 TodayPrice<MA。
- **預期衝高**：MA 長度是最核心旋鈕,掃描可找對 MaxDD/Calmar 最穩的長度高原,直接影響 G2/G3。
- **回測驗證**：掃 {10,15,20,30,40,60},看中位 Calmar 與 MaxDD 是否形成平坦高原,取高原中心。

#### #8 乖離率(BIAS)最小深度門檻  `priority:high` `overfit:low`
- **改動**：DecideBuy 把 TodayPrice<MA 改為 bias=(TodayPrice-MA)/MA<=-buy_bias_threshold;門檻=0 等同現況可平滑回退。
- **預期衝高**：只跌破一點常是雜訊接刀,要求最小乖離過濾淺回檔降低無效買入,改善 Calmar 與成本後 XIRR。
- **回測驗證**：掃 {0,0.01,0.02,0.03,0.05},看 CAGR/Calmar 是否在某區間上升且買入次數下降。

#### #24 連跌N日後暫停一日(避免接刀)  `priority:high` `overfit:low`
- **改動**：Snapshot 帶近 N 日連續下跌天數 downStreak;DecideBuy 要求 downStreak<=buy_max_down_streak 才買。
- **預期衝高**：純逢低在崩跌段每天接刀墊高成本,限制連跌天數=粗略止穩確認,降低最差視窗回撤(G5)改善 Calmar。
- **回測驗證**：掃 {99,5,4,3,2}(99=不啟用),看最差視窗 MaxDD 與中位 Calmar,找抑制接刀又不錯失反彈區間。

#### #31 均線種類改用EMA(對回檔更靈敏)  `priority:medium` `overfit:low`
- **改動**：loadStockSeries 增 EMA 計算(標準遞迴無未來資訊),config 加 buy_ma_type:SMA|EMA,進場門檻讀選定均線。
- **預期衝高**：EMA 對近期下跌反應快進場更貼近真實回檔,可能提升真擇時(G4)與 XIRR,換公式不增旋鈕風險低。
- **回測驗證**：固定長度 A/B SMA vs EMA,比中位 CAGR、Calmar勝率、G4,兩者皆掃同長度確認非巧合。

#### #33 止穩確認:今日收盤>昨日收盤才買  `priority:medium` `overfit:low`
- **改動**：Snapshot 帶昨日收盤 prevClose;DecideBuy 在跌破 MA 基礎上加選配 require_up_day:今日須收紅。
- **預期衝高**：把純左側接刀改為跌破MA區間內出現反彈日再進場,大幅降低買在半山腰機率改善每筆進場與 G5。
- **回測驗證**：A/B require_up_day true/false,比中位 CAGR、Calmar、最差視窗 MaxDD 與買入次數變化。

#### #34 RSI(14)超賣gate  `priority:medium` `overfit:low`
- **改動**：loadStockSeries 預算 RSI(14) 存進 Snapshot;DecideBuy 增加 snap.RSI<=buy_rsi_max(預設100=不啟用)的 gate。
- **預期衝高**：避免在跌破MA但仍偏強時過早進場,集中子彈在真超賣區提升每筆進場品質與真擇時(G4)。
- **回測驗證**：掃 buy_rsi_max∈{100,50,40,35,30},walk-forward 比中位 Calmar 與 G4 勝率,找改善又不過度減少交易的門檻。

#### #52 量能確認:放量下殺才進場  `priority:medium` `overfit:medium`
- **改動**：loadStockSeries 載 volume 算 volume MA(20),Snapshot 帶 RVOL=今量/量MA;DecideBuy 增加 snap.RVOL>=buy_rvol_min(預設0=不啟用)。
- **預期衝高**：放量下殺常是賣壓宣洩打底,無量陰跌易續跌,以相對量過濾挑較高勝率進場改善 G4 與每筆品質。
- **回測驗證**：掃 buy_rvol_min∈{0,1.0,1.3,1.5,2.0},比中位 Calmar、買入後短期報酬,確認兩檔方向一致。

#### #61 布林%B下軌gate(均值回歸進場)  `priority:medium` `overfit:low`
- **改動**：預算20日布林帶(中軌MA、±k*std),Snapshot 帶 %B;DecideBuy 增加 snap.PercentB<=buy_percentb_max(預設1=不啟用)。
- **預期衝高**：%B 把回檔深度用波動率標準化,跨標的(低波vs高波)一致比較避免對單檔硬調,穩健進場濾網利於 G2/G3。
- **回測驗證**：掃 buy_percentb_max∈{1.0,0.5,0.2,0.0}、bandK∈{2.0,2.5},看 Calmar勝率與 MaxDD,確認兩檔方向一致。

#### #69 KD隨機指標超賣gate  `priority:medium` `overfit:low`
- **改動**：loadStockSeries 用 high/low/close 算 Stochastic %K(9日),Snapshot 帶 K;DecideBuy 增加 snap.K<=buy_k_max(預設100=不啟用)。
- **預期衝高**：KD 與 RSI 屬不同擺盪族,可作互換或疊加的超賣濾網挑更極端回檔點提升進場品質與真擇時(G4)。
- **回測驗證**：掃 buy_k_max∈{100,30,20,15},與 RSI gate 做單獨對照(只開一個)避免交易過稀,比中位 Calmar。

#### #76 回檔深度分層gate(越深越易觸發越淺越挑剔)  `priority:medium` `overfit:medium`
- **改動**：DecideBuy 把 pct 分層也用作准入:淺 tier 需更嚴格 gate(更低 RSI/%B),深 tier 放寬,集中資金在深回檔。
- **預期衝高**：現行淺回檔也買子彈分散,分層准入讓越深買越多越易觸發越淺越挑剔提升資金加權 XIRR 與真擇時(G4)。
- **回測驗證**：A/B 開/關分層 gate,並掃淺 tier RSI 門檻∈{35,40,45},比中位 XIRR、買入次數是否往深 tier 移動。

#### #91 連續金額曲線:移除離散tier改平滑函數  `priority:medium` `overfit:low`
- **改動**：DecideBuy 用 amount=base*(1+slope*max(0,-drawdownPct)) 或 base*exp(k*|drawdown|),以 base 與 slope/k 兩旋鈕取代 tier 陣列。
- **預期衝高**：連續函數消除邊界附近金額跳階的路徑依賴雜訊,使加碼對下跌深度單調平滑降對特定邊界的過擬合,通常使 CAGR/Calmar 對參數更不敏感。
- **回測驗證**：掃 slope/k 的 5~6 個值,與離散4階 A/B,比 Calmar勝率、CAGR 離散度與 G4,確認平滑版高原更寬。

#### #92 RSI牛背離gate(僅最深tier重倉)  `priority:low` `overfit:medium`
- **改動**：用 RSI 陣列偵測簡易牛背離(價創近N日新低但 RSI 未創新低),DecideBuy 在最深 tier 加選配 require_bullish_div gate。
- **預期衝高**：背離是底部高機率訊號,僅用於最深 tier 在大跌段挑更精準重倉點提升資金加權 XIRR 與真擇時(G4),不增多餘旋鈕。
- **回測驗證**：A/B 深 tier 開/關背離 gate(lookback=20),比中位 XIRR、G4勝率、深 tier 進場後60日報酬,確認兩檔一致。

#### #96 多濾網計分制准入  `priority:low` `overfit:medium`
- **改動**：把 RSI/%B/RVOL/斜率等濾網各記 0/1 分,DecideBuy 要求 score>=buy_min_score 才進場(門檻=0 等同現況)。
- **預期衝高**：計分制用多指標共振取代任一硬門檻,較不依賴單一旋鈕的神奇值是抗過擬合的穩健聚合法,平滑提升 Calmar 與 G3/G4。
- **回測驗證**：各 gate 用前述掃描出的穩健門檻,buy_min_score 掃{0,1,2,3},看中位 Calmar勝率與交易次數權衡取平坦段。

### 賣出出場（13）

#### #2 目標報酬門檻掃描找高原  `priority:high` `overfit:low`
- **改動**：不改邏輯,把 baseline_sell_threshold 當主旋鈕在 walk-forward 系統掃 0.3~1.5。目前 +100% 在 24 月視窗幾乎不觸發使賣出側形同失效。
- **預期衝高**：找能真正觸發出場、降 MedStratMDD 並拉高 Calmar 勝率與 MedStratXIRR 的穩定區間。
- **回測驗證**：掃 threshold∈{0.3,0.4..1.5},畫 MedCAGR/MedMDD/Calmar勝率/XIRR 曲線取平坦高原中段。

#### #12 峰值回撤移動停利(trailing stop)  `priority:high` `overfit:medium`
- **改動**：預算開倉以來最高收盤 PeakSinceOpen;DecideSell 在 gain 達 arm 門檻(如+30%)後,今日價<=峰值*(1-trail_pct) 觸發賣出。
- **預期衝高**：用回撤鎖利取代固定目標,砍掉曲線高點回吐改善 MaxDD/最差視窗回撤(G2/G5),Calmar 上升。
- **回測驗證**：掃 trail_pct∈{8%,12%,15%,20%},arm 固定 +30%,看 MedMDD 與 Calmar 勝率高原並確認兩檔一致。

#### #13 跌破 MA20 觸發停利(零新指標)  `priority:high` `overfit:low`
- **改動**：DecideSell 在 gain 達 arm 門檻(如+30%)後,若今日收盤<MA20 視為趨勢轉弱出場;MA20 已預算改動最小。
- **預期衝高**：用既有 MA20 當結構性 trailing,趨勢續強抱滿轉弱鎖利,壓低高點回吐改善 MaxDD/Calmar 且不增參數。
- **回測驗證**：掃 arm_gain∈{20%,30%,40%,50%},A/B vs +100% baseline,看 MedMDD 降、Calmar勝率升是否兩檔一致。

#### #38 ATR吊燈出場(Chandelier Exit)  `priority:medium` `overfit:medium`
- **改動**：預算 ATR(14) 與開倉以來峰值;DecideSell 在 gain 達 arm 後若今日價<=峰值-k*ATR 觸發賣出(k約2.5~3.5)。
- **預期衝高**：波動率標準化停利對高波 00830 與低波 006208 自動調鬆緊,比固定百分比穩健較不 overfit,壓低 MaxDD 拉高 Calmar。
- **回測驗證**：掃 k∈{2.5,3.0,3.5},arm 固定,比兩檔各自 MedMDD/Calmar 是否同向改善並對照固定百分比 trailing。

#### #39 RSI超買gate才允許出場  `priority:medium` `overfit:medium`
- **改動**：預算 RSI(14);DecideSell 在 gain 達標基礎上加一道閘:RSI>=rsi_sell(如70) 才賣,否則延後讓利潤奔跑。
- **預期衝高**：只在動能過熱時出場避免趨勢中途過早賣,維持上漲參與(護G1)同時在高點附近出場改善 Calmar/G5。
- **回測驗證**：掃 rsi_sell∈{65,70,75},A/B 對照純 gain 門檻,看是否不傷 G1 下提升 Calmar 勝率與 XIRR。

#### #47 出場規模隨gain放大(倒金字塔減碼)  `priority:medium` `overfit:low`
- **改動**：DecideSell 維持單一觸發門檻但賣出金額=baseline_sell_amount*f(gain),gain 越高賣越多(線性或級距倍率)。
- **預期衝高**：高獲利加速兌現低獲利少賣,鎖住極端漲幅利潤改善 Calmar 與 XIRR(G3),保留中段參與護 G1,只多一個斜率旋鈕。
- **回測驗證**：掃放大斜率(每超門檻+25%多賣0.5x),A/B vs 固定金額,看 XIRR 與 Calmar勝率提升且 MDD 不惡化。

#### #48 賣出部位規模改為持倉比例(scale-out)  `priority:medium` `overfit:low`
- **改動**：DecideSell 目標股數改為 sell_fraction*該檔總持股(如0.25),而非固定金額。
- **預期衝高**：固定金額在大部位只兌現一小塊、小部位可能一次清光,比例式 scale-out 讓兌現與部位規模對齊改善階梯出場平滑曲線與 Calmar/G5。
- **回測驗證**：掃 sell_fraction∈{0.2,0.25,0.33,0.5},對照現行固定金額,比中位 CAGR、Calmar勝率、MaxDD 與 XIRR。

#### #65 以成本均價取代最低lot作為gain基準  `priority:medium` `overfit:low`
- **改動**：DecideSell 的 gain 基準從 LowestHeldPrice 改為持倉加權平均成本(Snapshot 加 AvgCost,由 lots 純算),語意改為整體部位獲利達標才出場。
- **預期衝高**：以最低 lot 計 gain 會過早對整體部位觸發出場,改用均價使出場更貼近真實獲利降低過早賣飛,穩定 G1 參與率不傷 Calmar。
- **回測驗證**：A/B lowest-lot vs avg-cost 基準(門檻同步重掃),比 MedCAGR(G1)、Calmar勝率、觸發次數分佈。

#### #81 降低目標+RSI/KD超買雙閘  `priority:medium` `overfit:medium`
- **改動**：baseline_sell_threshold 降到易觸發區(+40~60%),但加 RSI 或 KD 超買 gate,只在獲利達標且超買時賣。
- **預期衝高**：降門檻提升觸發頻率與 XIRR/現金回收,超買 gate 防趨勢初段賣飛,兼顧 G1 參與率與 G2/G3,旋鈕少機制清楚。
- **回測驗證**：2D 掃 threshold×rsi_sell 網格,挑 G1~G4 同時通過且指標平坦的高原格點,對照單獨降門檻是否更穩。

#### #88 布林上軌%B分批出場gate  `priority:medium` `overfit:medium`
- **改動**：預算布林%B(20,2),DecideSell 在 gain 達標後於 %B>bb_high(0.9~1.0觸碰上軌)分批賣出。
- **預期衝高**：在統計上偏離均值的高檔分批兌現讓出場集中相對高點提升 Calmar 與 XIRR(G3),布林與 MA20 同源旋鈕少跨標的一致性佳。
- **回測驗證**：掃 bb_high∈{0.85,0.9,0.95},A/B vs 純 gain 門檻,看 MedMDD/Calmar勝率改善且兩檔方向一致。

#### #93 MACD柱狀體縮腳/頂背離出場gate  `priority:low` `overfit:medium`
- **改動**：預算 MACD 柱狀體(DIF-DEA),DecideSell 在 gain 達標後若柱狀體連續 N 日縮減(動能衰竭)才出場,作為領先出場閘。
- **預期衝高**：用動能衰竭領先訊號出場賣在轉折前緣改善最差視窗回撤與 Calmar(G3/G5),MACD 通用對兩檔一致參數集中。
- **回測驗證**：掃柱狀體縮減 N∈{2,3,4} 日,A/B vs 純 gain 門檻,確認 Calmar勝率上升且 G1 參與率不顯著受損。

#### #94 KD超買區出場gate  `priority:low` `overfit:medium`
- **改動**：預算 Stochastic KD(9,3,3),DecideSell 在 gain 達標後需 K>=kd_high(如80) 或 K 自高檔下穿 D 才出場。
- **預期衝高**：KD 對台股 ETF 反轉點敏感,作出場時點過濾讓賣在相對高檔提升 Calmar勝率與最差視窗回撤(G3/G5),通用指標不針對日期。
- **回測驗證**：掃 kd_high∈{75,80,85},A/B vs 純 gain 門檻,確認兩檔一致改善 Calmar 且不顯著拉低 G1。

#### #98 ATR倍數固定停利門檻(波動率標準化目標)  `priority:low` `overfit:medium`
- **改動**：把 +100% gain 門檻改成今日價>=最低成本+m*ATR(從開倉)的波動率標準化目標(m約6~12),取代或並列百分比門檻。
- **預期衝高**：用 ATR 把目標對齊各標的波動避免對 00830 太鬆對 006208 太緊,跨兩檔一致性更好較不易 overfit 改善 Calmar 與 XIRR 穩定度。
- **回測驗證**：掃 m∈{6,8,10,12},A/B vs 百分比門檻,看兩檔觸發頻率與 Calmar勝率是否比固定百分比更平坦。

### 部位規模（9）

#### #9 金額曲線幾何化(2旋鈕取代4手調金額)  `priority:high` `overfit:low`
- **改動**：新增 base_amount 與 growth,amount=base*growth^tierIndex,邊界仍等距固定。把 500/750/1300/2000/3000 收斂成平滑曲線。
- **預期衝高**：5 個獨立旋鈕降為 2 個可掃高原而非硬猜,降過擬合;越深加碼越多提升下跌段部署推升 CAGR/XIRR。
- **回測驗證**：掃 growth∈{1.2,1.35,1.5,1.7,2.0}、base∈{300,500,750},看 G1/G4 是否在 growth 1.4~1.7 形成平台。

#### #10 現金緩衝感知的金額縮放(降 SkippedBuys)  `priority:high` `overfit:low`
- **改動**：目標金額改成 min(tierAmount*mult, cash*max_buy_fraction),讓深 tier 在現金不足時平滑縮小而非被 floor 硬夾到 0。
- **預期衝高**：現行夾取讓深跌段(最該加碼)常被跳過約252次,破壞金字塔初衷;按現金比例分配保住部署節奏提升 G1/G4。
- **回測驗證**：掃 max_buy_fraction∈{0.1,0.15,0.2,0.3},對照硬夾,比 SkippedBuys、中位 CAGR/XIRR、G5 MaxDD。

#### #16 分數Kelly/上限縮放取代固定multiplier=2.0  `priority:high` `overfit:low`
- **改動**：新增 kelly_fraction,放大係數=clamp(base_multiplier*kelly_fraction,0,cap),以保守固定 kelly_fraction 做 half-Kelly 全域縮放。
- **預期衝高**：multiplier=2.0 等於滿倉式放大易觸發 no-borrow 夾取(252 SkippedBuys 即症狀),分數 Kelly 把部署收斂到可持續區間降破壞風險與 MaxDD。
- **回測驗證**：掃有效 multiplier∈{1.0,1.5,2.0,2.5,3.0},回報 SkippedBuys、MaxDD、CAGR、XIRR,找夾取低且 Calmar 高的高原。

#### #21 買賣multiplier分離(獨立調節加碼/出場速度)  `priority:high` `overfit:low`
- **改動**：把 BuyAndSellMultiplier 拆成 buy_multiplier 與 sell_multiplier(預設皆 2.0 相容),DecideSell 用 sell_multiplier。
- **預期衝高**：現行共用無法獨立調出場部位規模,分離後可在不動加碼曲線下改善 +100% 後兌現節奏與 Calmar(G3)、G5。
- **回測驗證**：固定 buy=2.0,掃 sell_multiplier∈{1.0,1.5,2.0,3.0},看中位 CAGR、MaxDD、Calmar勝率與 XIRR 取捨。

#### #32 用MA20乖離分層取代相對持倉最高價pct  `priority:medium` `overfit:low`
- **改動**：DecideBuy 用 bias=(TodayPrice-MA20)/MA20 落點選 tier 金額,tier 語意從相對持倉成本改成相對均線便宜程度。
- **預期衝高**：現行 pct 依賴 HighestHeldPrice,首筆或清倉後失去分層意義;BIAS 讓越便宜加碼越多對所有標的一致提升 G4/G1。
- **回測驗證**：A/B pct 分層 vs BIAS 分層(邊界掃 -2%~-12%),比中位 CAGR、Calmar勝率、G4。

#### #37 波動率反比調整買入金額(inverse-vol sizing)  `priority:medium` `overfit:medium`
- **改動**：Snapshot 帶近20日報酬std sigma;DecideBuy 最終金額乘 clamp(target_vol/sigma,lo,hi),其餘分層不變。
- **預期衝高**：vol-targeting 高波動縮小單筆低波動放大,平滑曲線壓低 MaxDD(G2/G5)提升 Calmar,單旋鈕通用不挑標的。
- **回測驗證**：掃 target_vol∈{0.10,0.15,0.20,0.25},clamp 固定[0.5,2.0],看中位 MaxDD、Calmar勝率、CAGR 穩定段。

#### #59 首筆建倉金額與後續加碼分離(seeding tier)  `priority:medium` `overfit:low`
- **改動**：新增 first_buy_amount,無持倉時用 first_buy_amount,有持倉才走下跌分層;現行此情況落在最淺 tier(500)。
- **預期衝高**：建倉太小讓策略長期低曝險過度抱現金(G4 被質疑點),適度放大首筆提高基礎曝險縮小與 blend 差距推升 CAGR/XIRR 強化 G4。
- **回測驗證**：掃 first_buy_amount∈{500,1000,1500,2000},看 MedStratAvgExp、CAGR、G4 勝率是否同向改善而不爆 MaxDD。

#### #79 乖離深度加權:BIAS越深金額放大係數越大  `priority:medium` `overfit:low`
- **改動**：既有 tier 金額乘 (1+bias_gain*|bias|),bias=(MA20-TodayPrice)/MA20,bias_gain 唯一新旋鈕,tier 邊界不變。
- **預期衝高**：把分層(粗粒度)與乖離深度(細粒度)解耦,tier 給節奏 bias_gain 給強度,讓加碼對便宜程度更靈敏提升下跌段資金效率 CAGR/XIRR/Calmar。
- **回測驗證**：固定現行 tiers 掃 bias_gain∈{0,1,2,4},bias_gain=0 對照,看中位 CAGR、Calmar勝率、MaxDD 高原位置。

#### #82 反金字塔vs正金字塔方向掃描(加碼單調性)  `priority:medium` `overfit:low`
- **改動**：用 growth 旋鈕讓金額隨下跌遞增(現行,growth>1)/遞減(<1)/等額(=1),在同一公式下切換掃描。
- **預期衝高**：越跌加碼越多是可被檢定的假設而非定論,連續掃描(含<1)機制性驗證最佳方向避免把信念寫死,找對 CAGR/Calmar 真正穩健的形狀。
- **回測驗證**：掃 growth∈{0.7,0.85,1.0,1.3,1.6,2.0},兩檔各自與合併下看 CAGR、MaxDD、G4,確認最佳區間是否落在 growth>1。

### 冷卻頻率（10）

#### #3 cooldown_days 全域掃描找高原  `priority:high` `overfit:low`
- **改動**：只把 cooldown_days 當唯一旋鈕在 walk-forward 做格點掃描,取相鄰值績效都不崩的平台中心。
- **預期衝高**：找讓中位 CAGR 與 Calmar 同時站上高原的天數,取代魔術數 14,降低過度交易拖累提升 XIRR。
- **回測驗證**：掃 {5,7,10,14,18,21,28,35},選相鄰3點都優於 baseline 的區段中位值。

#### #17 買入no-trade band(乖離不夠不進場)  `priority:high` `overfit:low`
- **改動**：DecideBuy 把今日價<MA20 改成 (MA20-今日價)/MA20>=buy_band_pct,band 預設 0 可調。
- **預期衝高**：只在剛低於 MA20 就買會產生大量貼著均線的雜訊交易,要求最低乖離濾掉低訊號攤平成本提升每筆品質與 XIRR。
- **回測驗證**：掃 {0,0.005,0.01,0.02,0.03,0.05},看 Buys 次數下降 vs 中位 CAGR/Calmar 找高原。

#### #29 最小加碼間距改用價格步長  `priority:high` `overfit:low`
- **改動**：DecideBuy 冷卻併入今日價需較 lastBuyPrice 再跌 min_price_step_pct 才再買;Snapshot 加 LastBuyPrice,與天數取較嚴者。
- **預期衝高**：用價格步長讓加碼點與下跌幅度掛鉤,橫盤少交易急跌才多次進場,提升成本效率與深跌參與利於 XIRR/G4。
- **回測驗證**：A/B 對純天數冷卻,掃 min_price_step_pct∈{0.03,0.05,0.08},比 Buys 次數與中位 Calmar。

#### #40 賣後二次冷卻(避免賣完馬上抄底)  `priority:medium` `overfit:medium`
- **改動**：DecideBuy 額外檢查 today-lastSellDate>=rebuy_cooldown_days(用 lastSell map),低於門檻不買。
- **預期衝高**：防止賣出→隔幾天又買回的乒乓交易,削減來回成本與稅,直接降換手率改善淨報酬與 XIRR。
- **回測驗證**：A/B rebuy_cooldown_days∈{0,14,30,60,90},看總 Buys+Sells 下降幅度 vs 中位 CAGR 是否守住。

#### #56 持倉空檔較短冷卻(空倉放寬有倉收緊)  `priority:medium` `overfit:medium`
- **改動**：DecideBuy 依是否有持倉切換冷卻:無倉用 entry_cooldown_days(較短),有倉用較長 addon_cooldown_days。
- **預期衝高**：建倉首筆不應被長冷卻卡住(影響 G1 參與率),加碼可拉長間隔控換手,兩段式只多一個旋鈕邏輯清楚不挑標的。
- **回測驗證**：固定 addon=cooldown_days,掃 entry_cooldown_days∈{1,3,5,7,14},比中位 CAGR(G1) 與總 Buys。

#### #62 換手率vs成本權衡:導入成本模型後重掃cooldown  `priority:medium` `overfit:low`
- **改動**：OnCashflow 加證交稅0.1%+手續費(含最低20元)+滑價的成本扣減,再重跑 cooldown 掃描。
- **預期衝高**：零成本回測系統性高估高換手策略,有成本後最佳 cooldown 右移,掃出的高原才是淨報酬/淨 Calmar 真正最佳區。
- **回測驗證**：同一 cooldown 格點對照含成本 vs 零成本兩組,看最佳天數位移與中位 XIRR 差距。

#### #66 賣出冷卻期/分批間隔(避免單視窗一次清光)  `priority:medium` `overfit:low`
- **改動**：Engine 加 lastSell 狀態,DecideSell 加 sell_cooldown_days,觸發後 N 天內不再賣,搭配固定金額形成自然分批。
- **預期衝高**：把一次性大額出場拆成時間分散階梯平滑權益曲線降低再買回換手,改善曲線平滑度與最差回撤(G5),XIRR 現金流更分散。
- **回測驗證**：掃 sell_cooldown_days∈{0,7,14,21},看換手率、MedMDD、Calmar 是否穩定,避免過長冷卻抱回高點。

#### #77 單檔每年加碼次數上限(annual entry cap)  `priority:medium` `overfit:medium`
- **改動**：Engine 記錄每檔當年度買入次數(滾動365天),DecideBuy 超過 max_buys_per_year 即跳過。
- **預期衝高**：硬性封頂換手率砍掉長期累積的微交易尾巴削減成本拖累,單旋鈕控制交易頻率對所有標的一致。
- **回測驗證**：掃 max_buys_per_year∈{6,8,10,12,16,∞},比中位 CAGR vs 總交易次數,找砍交易但不掉報酬平台。

#### #84 全市場層級全域交易冷卻(限制每日跨檔總交易)  `priority:medium` `overfit:medium`
- **改動**：Engine 記錄最後一次任意成交日,ProcessDay 在 global_cooldown_days 內限制新買入,避免兩檔同波同時密集進場。
- **預期衝高**：006208 與 00830 在系統性下殺時高度相關會同時觸發加碼造成資金與曝險瞬間集中,全域節流降最差視窗回撤(G5)保留乾火藥(G4)。
- **回測驗證**：掃 global_cooldown_days∈{0,1,2,3,5},比 G5 worst MDD 與 G4 blend勝率,確認 G1 不明顯退化。

#### #89 年化換手率上限作為驗收護欄  `priority:medium` `overfit:low`
- **改動**：walk-forward 報表新增每視窗年化換手率(成交額/平均權益)輸出,設軟上限作為各方向 A/B 篩選條件。
- **預期衝高**：提供統一指標量化交易頻率,讓所有冷卻方向在相同或更低換手下比 Calmar/CAGR,避免靠多交易灌水強化 G4 真擇時論證。
- **回測驗證**：對每個候選參數計算年化換手率,只接受換手不升而中位 Calmar 升的設定,繪換手-Calmar 散點挑帕雷托前緣。

### 市場濾網（15）

#### #6 MA200 空頭縮碼軟濾網  `priority:high` `overfit:low`
- **改動**：預算 MA200 加進 Snapshot;DecideBuy 在 TodayPrice<MA200 時把 tier 金額乘 bear_buy_scale(<1),多頭維持原額。
- **預期衝高**：空頭縮碼降低下跌段曝險改善 G2/G5,但保留部分曝險不殺 CAGR(G1),比硬開關更平滑。
- **回測驗證**：掃 bear_buy_scale∈{1.0,0.75,0.5,0.25,0},看 MedStratMDD、Calmar勝率、G2/G3/G5 是否同向改善。

#### #30 MA斜率濾網(均線非陡降才買)  `priority:high` `overfit:low`
- **改動**：Snapshot 帶 MA 斜率(MA[i]-MA[i-lookback])/MA;DecideBuy 要求斜率>=-buy_ma_slope_max 限制只在均線非陡降時進場。
- **預期衝高**：MA 急墜代表趨勢正壞逢低易連續套牢,斜率門檻過濾瀑布段,主攻 G5 最差回撤與 Calmar,單旋鈕穩健。
- **回測驗證**：lookback 固定5,掃 buy_ma_slope_max∈{∞,0.02,0.01,0.005,0},看最差視窗 MaxDD 與中位 Calmar 高原。

#### #35 雙均線回檔(長均線上方才逢低買短均線)  `priority:medium` `overfit:low`
- **改動**：預算長MA(MA60/120),DecideBuy 要求跌破短均線但仍>長均線*(1-buffer),即多頭結構內的回檔。
- **預期衝高**：用長均線當趨勢濾網只在中長期未轉空時逢低,避開長空段接刀,提升參與率(G1)同時控住 G5。
- **回測驗證**：掃 long_ma_period∈{60,120,200}、buffer∈{0,0.03,0.05},看 G1 參與率與 G5 是否同步改善。

#### #36 ATR標準化的下跌延伸度進場門檻  `priority:medium` `overfit:medium`
- **改動**：loadStockSeries 載 high/low 算 ATR(14);DecideBuy 要求 (MA-TodayPrice)/ATR>=buy_atr_dist 才買。
- **預期衝高**：用 ATR 取代固定百分比衡量回檔深度自動適應各標的波動,高低波標的用同一旋鈕,跨標的一致防 overfit 改善 Calmar。
- **回測驗證**：掃 buy_atr_dist∈{0,0.5,1.0,1.5,2.0},看中位 Calmar/MaxDD 高原,對照固定 BIAS 門檻何者更穩。

#### #43 波動率regime:ATR比值高時縮碼  `priority:medium` `overfit:low`
- **改動**：預算 ATR(14) 與長期均值,Snapshot 帶 volRatio=ATR_now/ATR_avg;DecideBuy 在 volRatio 偏高時把 tier 金額乘 vol_scale(<1)。
- **預期衝高**：高波動 regime 常伴隨崩盤,縮碼等同波動率目標化倉位平滑曲線降 MaxDD 提升 Calmar 與 Sortino(G3)。
- **回測驗證**：掃 vol_ratio_threshold∈{1.2,1.5,2.0} 與 vol_scale∈{0.5,0.75},看參數高原是否穩定改善 G2/G3。

#### #51 黃金/死亡交叉(MA50 vs MA200)regime開關  `priority:medium` `overfit:low`
- **改動**：預算 MA50、MA200,Snapshot 帶 regimeBull=MA50>=MA200;DecideBuy 死叉用較小 tier 金額黃金叉用原額,單一布林 regime 旋鈕。
- **預期衝高**：50/200 交叉是低旋鈕跨標的一致的趨勢狀態判定,死叉縮碼降空頭曝險改善 G2/G5黃金叉維持參與守 G1。
- **回測驗證**：A/B 有無 regime 開關+bear_scale∈{0,0.5},比 MedStratMDD 與 BlendSkillRate(G4)確認優勢來自擇時。

#### #60 regime相依賣出門檻(空頭早一點止盈)  `priority:medium` `overfit:medium`
- **改動**：DecideSell 的門檻改 regime 相依:多頭維持 +100%,空頭/高波動 regime 用較低門檻(如+70%)讓獲利部位更早落袋。
- **預期衝高**：risk-off regime 提早分批止盈鎖住獲利減回吐,平滑曲線改善 Sortino 與最差視窗回撤(G5),單旋鈕控制不易過擬合。
- **回測驗證**：掃 bear_sell_threshold∈{0.6,0.7,0.8,1.0},看 MedStratMDD 與 Calmar勝率是否改善而 CAGR 不顯著下降。

#### #63 波動率目標化倉位取代固定multiplier  `priority:medium` `overfit:medium`
- **改動**：buy_and_sell_multiplier 改動態:effective_mult=target_vol/realized_vol(N日報酬std),夾在[min,max],高波動自動縮低波動自動放。
- **預期衝高**：vol targeting 是通用單旋鈕機制把跨 regime 曝險標準化,提升風險調整報酬(Calmar/Sortino,G3)不需猜固定倍數。
- **回測驗證**：掃 target_vol(年化)∈{15%,20%,25%,30%} 與夾取上下限,比 MedStratCAGR/MDD 與 baseline 固定2.0。

#### #73 ATR標準化下跌延伸度加碼門檻(跨標的一致)  `priority:medium` `overfit:low`
- **改動**：用 ATR 把進場門檻標準化:要求 price 距 MA20 至少 k*ATR 才允許加碼(取代/疊加固定 -10% tier 邊界),k 單旋鈕。
- **預期衝高**：固定百分比對低波 006208 與高波 00830 不公平,ATR 標準化讓兩檔用同套邏輯跨標的一致防 overfit 改善 G4。
- **回測驗證**：掃 k∈{0.5,1.0,1.5,2.0} ATR,看兩檔合併 walk-forward 是否出現穩定最佳區並比較對 00830 的 MaxDD 影響。

#### #74 risk-off時拉長冷卻(regime-adaptive cooldown)  `priority:medium` `overfit:low`
- **改動**：固定 cooldown_days 改 regime 相依:多頭用 base_cooldown,空頭/高波動 regime 用較長(base*factor)拉開接刀間距。
- **預期衝高**：下跌趨勢中拉長加碼間距=降低接刀頻率與單位時間曝險增速,壓低最差視窗回撤(G5)減少 SkippedBuys 浪費,Calmar 受益。
- **回測驗證**：掃 bear_cooldown_factor∈{1,1.5,2,3},A/B 對照固定 cooldown,看 G2/G5 與交易次數變化。

#### #78 雙重確認(趨勢+波動)regime-permission矩陣  `priority:medium` `overfit:medium`
- **改動**：用 MA200 多空×ATR 高低波組 4 格 regime 矩陣,各格給曝險係數(多頭低波=1.0、空頭高波=0.25),DecideBuy 查表縮放金額。
- **預期衝高**：把分散的單一濾網收斂成一張對所有標的一致的查表,旋鈕少可解釋,同時改善 G2/G3/G5 並用 G4 驗證確為擇時。
- **回測驗證**：先各別掃出 MA200/ATR 門檻穩定區,再 A/B 整張矩陣 vs baseline,比 5 道 gate 通過數與 BlendSkillRate。

#### #80 MA200斜率濾網(趨勢方向確認)  `priority:medium` `overfit:low`
- **改動**：預算 MA200 的 N 日斜率;DecideBuy 僅當斜率>=0(走平或上彎)才全額加碼,斜率為負時套用縮碼係數。
- **預期衝高**：純價格 vs MA200 在均線下方頻繁假突破,加斜率過濾緩跌段接刀提升真擇時(G4 對 blend 勝率)與 Calmar(G3),通用非硬調。
- **回測驗證**：掃 slope_lookback∈{40,60,90,120} 交易日,找 Calmar勝率與 G4 同時穩定的高原區。

#### #85 ADX趨勢強度濾網(弱勢盤才加碼)  `priority:medium` `overfit:medium`
- **改動**：預算 ADX(14)(用 high/low/close 算 true range),DecideBuy 僅在 ADX 低於門檻(無強趨勢)時允許逆勢加碼,ADX 高時縮碼或暫停。
- **預期衝高**：逢低加碼本質是均值回歸,在強趨勢下跌段最危險,ADX 過濾避免崩盤趨勢中連續接刀降 MaxDD(G2/G5)升 Calmar(G3)。
- **回測驗證**：掃 adx_threshold∈{20,25,30,35},觀察是否存在穩定區間使 G2/G3 改善而 G1 不破。

#### #95 +DI/-DI方向濾網(DMI)  `priority:low` `overfit:medium`
- **改動**：預算 +DI、-DI,DecideBuy 在 -DI 明顯壓過 +DI(空方主導)時套用縮碼係數,+DI 領先維持原額。
- **預期衝高**：DMI 直接給趨勢方向避免在 -DI 主導下跌段加大加碼,對應降低最差視窗回撤(G5)與提升 blend 勝率(G4)。
- **回測驗證**：A/B 加 DMI 方向濾網 vs baseline,掃 di_gap_threshold∈{0,5,10} 找穩定區。

#### #97 布林帶寬(BBW)趨勢/橫盤regime偵測  `priority:low` `overfit:medium`
- **改動**：預算20日std與布林帶寬 BBW,帶寬擠壓(低波橫盤)時正常加碼,帶寬急放(趨勢/崩跌啟動)時縮碼。
- **預期衝高**：BBW 擴張常標誌趨勢段開始,此時暫緩逆勢加碼避開崩跌初期接刀降 MaxDD(G2)提升 Calmar(G3),機制通用易掃描。
- **回測驗證**：掃 bbw_percentile_threshold(帶寬高於過去一年70/80百分位)觸發縮碼,找穩定高原。

### 風險回撤（13）

#### #5 MA200 長線多空硬濾網(僅多頭加碼)  `priority:high` `overfit:low`
- **改動**：loadStockSeries 仿 MA20 預算 MA200;DecideBuy 在 TodayPrice<MA200 直接不買,只在長線多頭逢低加碼。等同 Faber 200MA regime filter 硬版。
- **預期衝高**：完全避開長空頭接刀,通常大幅降 MaxDD(G2/G5),並抬升 Calmar。
- **回測驗證**：A/B 啟用 vs 不啟用,掃 maLongWindow∈{120,150,200,250},重點看 G1 仍過且 G2/G5 改善。

#### #7 災難停損出場(取代死抱)  `priority:high` `overfit:medium`
- **改動**：DecideSell 在 +100% take-profit 前加一條:loss_from_highest 超過 stop_loss_pct 時觸發賣出。用 HighestHeldPrice 計算。
- **預期衝高**：目前完全無下檔保護是 MaxDD/Calmar 最大缺口,硬停損砍掉長尾虧損改善 G2/G3/G5。
- **回測驗證**：掃 stop_loss_pct∈{0.30,0.35,0.40,0.45,0.50},每點看 Calmar/G2/G5,要求 G1 不崩取高原。

#### #18 權益曲線剎車(回撤過深暫停加碼)  `priority:high` `overfit:medium`
- **改動**：engine 維護全帳戶 equity peak,當前權益自峰值回撤>dd_brake_pct 時暫停所有新買入(或減半),回升至 recover 內才解除。
- **預期衝高**：回撤剎車直接針對 MaxDD:崩盤中止血不再加深虧損,強力改善 G2/G5 並縮短恢復期提升 Calmar。
- **回測驗證**：掃 dd_brake_pct∈{0.15,0.20,0.25,0.30},A/B 開關,看 G2/G5/最差視窗 MDD 與 G1 是否守住。

#### #22 現金下限保留(dry-powder reserve)  `priority:high` `overfit:low`
- **改動**：applyBuy 現金夾取改為可用現金=max(0,cash-reserve),reserve=cash_floor_frac*權益;低於下限不買 SkippedBuys++。
- **預期衝高**：保留乾火藥避免崩盤初期把現金用光、後段更深跌無力承接,降平均曝險壓低 MaxDD 提升 Calmar。
- **回測驗證**：掃 cash_floor_frac∈{0,0.1,0.2,0.3},看 Calmar/G2 改善是否伴隨 G4 維持(排除抱現金作弊)。

#### #23 單檔曝險上限(per-name position cap)  `priority:high` `overfit:low`
- **改動**：applyBuy 前算該檔持倉市值佔總權益比例,>=per_name_cap_frac 則跳過該檔買入,迫使現金流向另一檔。
- **預期衝高**：防高波動 00830 吃滿倉放大整體回撤,限制集中度平滑 equity 降 MaxDD 提升 Calmar/G5,通用風控不挑標的。
- **回測驗證**：掃 per_name_cap_frac∈{0.4,0.5,0.6,0.7,1.0},看 G2/G5 改善與 G1 取捨,兩檔一致套用。

#### #27 分批停利階梯(取代單一+100%)  `priority:high` `overfit:medium`
- **改動**：DecideSell 把單一門檻拆成多段:+50%/+100%/+150% 各賣一定比例(tiers 化),從成本最低 lot 開始賣。
- **預期衝高**：單一門檻讓大段獲利在門檻前完全暴露於回撤,分批落袋平滑 equity 降高點回吐,降 MaxDD 提升 Calmar 與 XIRR。
- **回測驗證**：A/B 單門檻 vs 三段階梯,掃階梯間距與比例少數組合,比 Calmar/XIRR/MDD 挑穩定者。

#### #41 總曝險上限(portfolio heat cap)  `priority:medium` `overfit:low`
- **改動**：ProcessDay 買入前算全組合持股佔總權益比例,超過 total_exposure_cap 即停止當日所有新買入。
- **預期衝高**：總曝險上限直接限制最大可能下跌幅度系統性壓低 MaxDD 改善 G2/G5,單旋鈕對所有標的一致過擬合風險低。
- **回測驗證**：掃 total_exposure_cap∈{0.6,0.7,0.8,0.9,1.0},畫 Calmar/MDD 對 cap 曲線找穩定高原。

#### #46 加碼間距硬下限(min price step for DCA)  `priority:medium` `overfit:low`
- **改動**：DecideBuy 增加:距上次買入價需再跌>=min_step_pct 才允許下一筆加碼(Snapshot 帶 lastBuyPrice),非僅看冷卻天數。
- **預期衝高**：現行只有時間冷卻沒價格間距易在小回檔密集加碼把現金提前耗盡,價格間距讓加碼留待更深跌改善平均成本降 MaxDD 抬 Calmar/XIRR。
- **回測驗證**：掃 min_step_pct∈{0.03,0.05,0.08,0.10},A/B 對純時間冷卻,觀察 SkippedBuys、Calmar、G2。

#### #53 保本停損(breakeven stop移動到成本)  `priority:medium` `overfit:medium`
- **改動**：當某 lot 曾達 +X%(per-lot 最高浮盈)後回落到成本價即出場該 lot 鎖定不虧,與 +100% 鎖利並列。
- **預期衝高**：把賺過又回吐成白工的部位在成本價止血避免獲利變虧損壓低個別部位 MAE,平滑 equity 降 MaxDD 改善 Calmar/G5。
- **回測驗證**：掃啟動門檻 X∈{0.3,0.5,0.7},A/B 開關,觀察 Calmar、G2、最差視窗 MDD。

#### #67 時間停損(time stop套牢部位減碼)  `priority:medium` `overfit:medium`
- **改動**：engine 記錄每檔最早未實現 lot 持有天數,DecideSell 增加:lot 持有超 time_stop_days 且仍虧損>loss_thresh 時部分出清釋放資金。
- **預期衝高**：長期套牢死部位拖累 equity 並佔用資金,時間停損釋放資金縮短回撤恢復期改善 Calmar/恢復因子並提升 deployed-capital XIRR。
- **回測驗證**：掃 time_stop_days∈{250,500,750} 與 loss_thresh∈{0.2,0.3},A/B 開關,看 Calmar/XIRR/MDD 恢復速度。

#### #83 ATR/波動率標準化停損(取代固定百分比)  `priority:medium` `overfit:medium`
- **改動**：Snapshot 加 ATR(或20日報酬std),災難停損門檻改為持倉最高價-k*ATR 動態值,對兩檔自動適配。
- **預期衝高**：固定百分比對高低波標的不公平,ATR 倍數對所有標的一致較不 overfit,高波動放寬低波動收緊降被洗出又降 MaxDD,助 G2/G5/Calmar。
- **回測驗證**：掃 atr_window∈{14,20}、atr_mult∈{2,2.5,3,3.5},看是否有寬廣 Calmar 高原並與固定%停損 A/B。

#### #90 回撤調節加碼批數(drawdown-scaled lot reduction)  `priority:medium` `overfit:medium`
- **改動**：依當前帳戶自峰值回撤幅度線性縮減 DecideBuy 目標金額乘數(回撤越深買越少),用 engine equityPeak 帶入 Snapshot。
- **預期衝高**：現行金字塔在深跌時加碼最多正是 MaxDD 放大器,改為回撤越深越保守鈍化下跌斜率降 MaxDD 提升 G5/Calmar,與崩盤剎車互補但更平滑。
- **回測驗證**：掃縮減斜率(回撤30%時乘0.5)少數設定,A/B 對原金字塔,看 G2/G5/Calmar 與 G1。

#### #99 崩盤保護出清覆蓋(drawdown circuit breaker)  `priority:low` `overfit:medium`
- **改動**：帳戶自峰值回撤超過 panic_dd_pct(極端如40%)時 ProcessDay 觸發全組合分批清倉至現金,待回升 recover 後重啟。
- **預期衝高**：最大回撤熔斷器是 G5 的直接保險,極端崩盤離場保命封住尾端風險顯著改善 G2/G5,門檻設高只在真崩盤觸發避免過度交易。
- **回測驗證**：掃 panic_dd_pct∈{0.35,0.40,0.45,0.50},A/B 開關,確認觸發次數稀少(防 overfit)且 G1 報酬參與未被嚴重犧牲。

### 資金部署（12）

#### #11 定期定額底倉+逢低加碼混合(拉高曝險攻G4)  `priority:high` `overfit:low`
- **改動**：engine 每隔 base_dca_every_days 對每檔投固定 base_dca_amount(等權整股夾取現金),疊加在 DecideBuy 上;底倉仍受 +100% 賣出管。
- **預期衝高**：策略低回撤多來自抱現金 G4 最難過;拉高 AvgExposure 讓 deployed 基數變大 XIRR 更可信,把閒置現金轉複利。
- **回測驗證**：掃 base_dca_amount 與 every_days(月/季),對照 amount=0,看 MedStratAvgExp、CAGR、G1/G4。

#### #15 回收現金即時再部署(賣出所得不閒置)  `priority:high` `overfit:low`
- **改動**：標記賣出/再平衡所得為可立即部署現金,同日或下個交易日優先投回最弱(或最低於MA20)標的,受 no-borrow 與 cooldown 約束。
- **預期衝高**：+100% 賣出後資金常閒置數月拖累 deployed-capital XIRR 與 AvgExposure(影響G4),即時再投入提高資金效率。
- **回測驗證**：A/B 開/關即時再部署,看 MedStratXIRR、AvgExp、平均現金閒置天數、Calmar 是否退化。

#### #20 處理順序去偏:依當日折扣深度排序配現金  `priority:high` `overfit:low`
- **改動**：ProcessDay 不再固定照 TrackStocks 順序配現金,改先算各檔低於 MA20 的乖離率,現金優先給最深(最超賣)標的。純機械排序。
- **預期衝高**：消除清單順序=資金優先權的隱性偏誤,把有限現金分配給當日最划算折扣提升 XIRR 與真擇時(G4)。
- **回測驗證**：A/B 固定序 vs 折扣深度序,看 XIRR、CAGR、SkippedBuys 是否下降、G4 BlendSkillRate 是否上升。

#### #26 exposure-matched底倉地板(直攻G4防抱現金)  `priority:high` `overfit:medium`
- **改動**：設 min_avg_exposure 目標,當近期平均持股低於地板時 engine 自動以底倉買入把曝險補到地板(整股夾取現金受 no-borrow)。
- **預期衝高**：把 AvgExposure 拉到有意義水準後低回撤不再只是抱現金,deployed 基數變大 XIRR 與 G4 BlendSkillRate 同時改善。
- **回測驗證**：掃 min_avg_exposure∈{0.3,0.4,0.5,0.6},看 BlendSkillRate、AvgExp、CAGR/MaxDD 取捨,A/B 對照無地板。

#### #44 閾值型再平衡(5/25規則)  `priority:medium` `overfit:low`
- **改動**：engine 每 M 交易日檢查兩檔市值權重,偏離目標(50/50)超 reb_band 時賣超重檔、把現金導向過輕檔下次加碼。對稱機械。
- **預期衝高**：再平衡均值回歸溢酬在不增曝險下提升風險調整報酬(Calmar→G3),賣出現金即時再部署提升 XIRR,對稱不易 overfit。
- **回測驗證**：掃 reb_band∈{0.1,0.15,0.2,0.25}、頻率(月/季),看 MedStratCalmar、CalmarWinRate、XIRR,A/B 對照無再平衡。

#### #45 相對強弱在兩檔間傾斜分配  `priority:medium` `overfit:medium`
- **改動**：Snapshot 加各檔 N 日報酬(60/120日 ROC);買入金額乘 rs_tilt,相對強的多配弱的少配但維持下限,用排名而非絕對閾值。
- **預期衝高**：資金導向相對強勢標的是穩健的橫截面動能效果,提升 deployed-capital XIRR 與中位 CAGR(G1),排名而非硬閾值較不 overfit。
- **回測驗證**：掃 lookback∈{60,120,200}、tilt 強度∈{1.0,1.3,1.6,2.0},看 XIRR/CAGR 是否有平台,A/B 對照等權。

#### #54 每檔總部位上限+池內資金配額  `priority:medium` `overfit:low`
- **改動**：engine 對每檔設 max_position_pct 上限,達上限停止對該檔加碼迫使現金流向另一檔,把單一池子拆成軟性配額。
- **預期衝高**：TrackStocks 順序讓 006208 系統性優先吃現金,上限均衡兩檔曝險降集中度風險(MaxDD→G2/G5)讓分配更公平提升 Calmar。
- **回測驗證**：掃 max_position_pct∈{0.4,0.5,0.6,0.7,1.0},看 MaxDD、CalmarWinRate、兩檔最終市值平衡度,A/B 對照無上限。

#### #58 深跌fallback改連續外推(移除3000硬上限)  `priority:medium` `overfit:low`
- **改動**：跌超過最後 tier 邊界時不用固定 fallback,延續金額曲線外推(amount=base*growth^lastIndex 後續 *growth),仍受現金緩衝夾取。
- **預期衝高**：固定 fallback 在最深最該重壓的價位反而封頂削弱金字塔尾段,連續外推讓最深跌部署更多資金抬升崩盤後回本斜率與 CAGR/XIRR(G1)。
- **回測驗證**：A/B 固定 fallback vs 外推,在含深跌視窗比中位 CAGR、最差視窗 CAGR 與 G5。

#### #70 波動率反比資金配重(低波多配,inverse-vol)  `priority:medium` `overfit:medium`
- **改動**：Snapshot 加各檔 N 日報酬std(或 ATR/價);兩檔間以 inverse-vol 權重分配加碼金額,可加 vol_target 夾取,公式對兩檔一致。
- **預期衝高**：00830 波動 37%+ 等額配會主導 MaxDD,inverse-vol/風險平價降組合回撤(G2/G5)穩定 Calmar(G3),經典穩健配置法。
- **回測驗證**：掃 vol_lookback∈{20,40,60}、配重強度,A/B 對照等權,看 MedStratMDD、CalmarWinRate、WorstStratMDD。

#### #72 deployed-capital XIRR目標化掃描排序  `priority:medium` `overfit:low`
- **改動**：cmd/evaluate 增彙整輸出:對任一參數網格回報 MedStratXIRR 與 AvgExposure 聯合分數,挑 XIRR 高且 G4 通過的高原,不只看 FinalTotal。
- **預期衝高**：FinalTotal 會被抱現金少賠灌水,以 deployed-capital XIRR+G4 當目標逼策略提升真實資金效率與擇時(G4),防 overfit 的目標函數設計。
- **回測驗證**：對任一方向網格輸出 (MedStratXIRR,BlendSkillRate,MedStratAvgExp) 表,挑穩定平台而非單點最高。

#### #87 現金水位自適應乘數(本金成長時等比放大)  `priority:medium` `overfit:medium`
- **改動**：buy_and_sell_multiplier 改 clamp(current_equity/initial_cash,lo,hi),權益成長時自動放大買賣金額使加碼與池子規模等比。
- **預期衝高**：固定乘數讓長視窗後期加碼相對池子越來越小 AvgExposure 偏低(傷G4),自適應維持曝險一致提升複利與 deployed-capital XIRR。
- **回測驗證**：掃 lo/hi 夾取範圍與基準,A/B 對照固定2.0,看 MedStratAvgExp、CAGR、MaxDD 是否同步且 Calmar 不崩。

#### #100 賣出階梯由start+step兩旋鈕生成(scale-out循環)  `priority:low` `overfit:medium`
- **改動**：把單一 +100% 全量賣改成多檔閾值階梯(如+80%/+120%/+160%各賣一部分),由 sell_threshold_start 與 sell_step 兩旋鈕生成,回收現金循環回部署池。
- **預期衝高**：一次性 +100% 賣常太早或太晚,階梯出場平滑賣出時點降 Calmar 變異(穩定G3),現金分批回流再部署提升 deployed-capital XIRR 與資金週轉。
- **回測驗證**：掃 sell_threshold_start∈{0.6,0.8,1.0}、step∈{0.2,0.4}、批數∈{2,3,4},看 Calmar勝率、XIRR、MaxDD,A/B 對照單閾值。

