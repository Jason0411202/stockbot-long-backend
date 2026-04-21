# Research Plans

本檔案會依時累積本專案每一輪的實驗計劃。舊版本不修改;新版本追加於末尾。

---

## Plan v1: 以「趨勢跟隨加碼分支」解決 Pyramid 在牛市中買量被 anchor 鎖死的問題 — 2026-04-21 22:01

- **Mode**: A(新想法)
- **Codebase commit**: 666a1791c4ec72ee80835996f831ad8827a94f35
- **Hamming 三問**: 對的問題 ✓(牛市錯失與現金過剩為可量化缺陷) / 對的時機 ✓(已有 Pyramid baseline、RunBacktest 與 10 年日線可重現) / 對的方法 ✓(一次一個變因、與 baseline 對照)

### 0. 前輪檢討
不適用(Mode A)。本輪是在使用者回饋「既有 Pyramid 策略於牛市過度保守、現金永遠用不完、無止損」後的第一份新計劃。後續 Plan v2+ 會依下方 §6 Fallback 處理另一條主線(加碼金額全面放大 / 止損)。

### 1. Hypothesis
**單句可證偽**:
> 在追蹤股票集 $D = \{006208, 00830\}$、近 3600 交易日區間上,若在現行 Pyramid 策略外新增一條「趨勢跟隨加碼分支」(Trend-Following Branch,TF),使得當市場被判定為**多頭趨勢**時,買入金額從 Pyramid 原本相對「歷史最高買入價」的級距切換成固定(或趨勢強度線性)的較大金額 $A_{\text{TF}}$,則最終權益 `FinalTotal` 將比 codebase 現行的 `Pyramid-Baseline` 高出至少 $\Delta \geq 0.05 \cdot C_0$($C_0$ 為起始現金),且最大回撤(MaxDD) 不惡化超過 5 個百分點。

命題的可證偽部分:若在所有被試 TF 觸發閾值下,`FinalTotal` 皆 $\leq$ Baseline,或 MaxDD 惡化超過 5pp,視為被證偽。

### 2. Preliminaries & Notation

- $P_t$: 第 $t$ 個交易日的收盤價。
- $\mathrm{MA}_w(t) = \frac{1}{w}\sum_{i=0}^{w-1}P_{t-i}$: 第 $t$ 日的 $w$ 日均價。
- $H_t$: 至第 $t$ 日前,該股票所有**歷史買入單價**的最大值(若尚無持倉,$H_t = \perp$)。
- $L_t$: 至第 $t$ 日前所有歷史買入單價的最小值。
- $r_t = (P_t - H_t)/H_t$: 現價相對「最高買入價」的比例(負代表已回檔)。
- $\mathcal{T}$: Pyramid 金額級距函數 $\mathcal{T}(r) \to$ 金額,形式為分段常數,從深回檔到淺回檔金額遞減。
- $B_t \in \{0,1\}$: 多頭趨勢判定旗標(§3.2)。
- $A_{\text{TF}}$: TF 分支在 $B_t = 1$ 時採用的買入金額。

### 3. Method

#### 3.1 Overview
現行策略由兩個判斷組成:(i)**買點訊號**:$P_t < \mathrm{MA}_{20}(t)$ 時視為可買日;(ii)**加碼金額**:以 $r_t$ 落入哪個 tier 決定要下多少錢。問題出在 (ii):當市場連續上漲,$H_t$ 會隨著每次買入被不斷往上推,即使偶有回檔觸發 (i),$r_t$ 通常只是小幅負數,恆落入最淺的 tier,金額恆等於最小值,於是形成「牛市每次買都買最少」的病態。

本方法**不動**買點訊號 (i),改動 (ii) 的**金額決策**:在 Pyramid 之外新增一條並行的 TF 分支。流程如下:

1. 每個可買日,先判斷 $B_t$(多頭趨勢是否成立)。
2. 若 $B_t = 1$(趨勢向上),使用 TF 金額 $A_{\text{TF}}$ 取代 Pyramid tier 的輸出。
3. 否則沿用 Pyramid tier。
4. 其他一切(MA20 觸發條件、冷卻期、賣出邏輯、股數四捨五入、初始現金)**完全不變**。

關鍵設計原則:TF 只接管**金額決策**這一個動作,不接管是否買、也不接管賣出。這使得新方法與 baseline 之間的差異剛好等於「牛市買多少」這單一變因。

#### 3.2 多頭趨勢判定 $B_t$
- 動機:在不引入未來資訊的前提下,辨識「當前是否處於連續上漲環境」,以避免在盤整或下行段誤觸 TF 而過度買入。
- 直覺:若短期均線已穩定站上長期均線,且兩線都向上,即視為多頭。
- 形式化(用最簡單、最常見、最少 hparam 的版本):
  $$
  B_t = \mathbb{1}\Big[\mathrm{MA}_{s}(t) > (1+\tau)\cdot \mathrm{MA}_{\ell}(t)\Big],
  \quad s=20,\; \ell=60,\; \tau \in \{0,\ 0.02,\ 0.05\}.
  $$
- 解讀:當 20 日均價比 60 日均價高出至少 $\tau$ 的比例時,判定為多頭。$\tau$ 為 scientific hparam(§4.4)。

#### 3.3 TF 買入金額 $A_{\text{TF}}$
- 動機:需要一個在牛市下「比 Pyramid 最淺 tier 還大」但又「不至於把現金一次性打光」的金額。
- 形式化(兩種備選形式,擇一為 scientific hparam):
  1. **常數型**:$A_{\text{TF}} = \alpha \cdot \max_r \mathcal{T}(r)$,$\alpha \in \{1,\ 2,\ 3\}$——即 Pyramid 最大 tier 的整數倍。
  2. **比例型**:$A_{\text{TF}}(t) = \beta \cdot \mathrm{Cash}_t$,$\beta \in \{0.01,\ 0.02,\ 0.05\}$——即當下現金的固定比例。
- 解讀:$\alpha$ 讓 TF 的金額與 Pyramid 規模「同量級但可調倍率」,便於直接對照;$\beta$ 讓 TF 自動隨本金擴縮,避免長期累積現金時 TF 變得相對太小。兩種形式在 §4.5 會各取代表點進行對比。

#### 3.4 Algorithm
```
INPUT : 歷史收盤序列 {P_t}、追蹤股票清單 D、起始現金 C0、
        Pyramid tier 函數 T、TF 觸發閾值 τ、TF 金額參數 θ∈{α,β}、
        冷卻天數 K、賣出門檻 g*、賣出金額 S
OUTPUT: FinalTotal、MaxDD、TotalBuys、TotalSells


1. 初始化 cash ← C0,每檔股票的 lots 為空,lastBuy 為空
2. for each trading day t in 時序:
3.     for each stock ∈ D:
4.         若 P_t ≥ MA_20(t):                    // 買點訊號不變
5.             跳到賣出判斷
6.         若 t - lastBuy[stock] < K:            // 冷卻期不變
7.             跳到賣出判斷
8.         計算 B_t = 1[ MA_20(t) > (1+τ)·MA_60(t) ]    // 新增:趨勢判定
9.         若 B_t = 1:
10.            amount ← A_TF(θ)                   // 新增:TF 分支
11.        else:
12.            amount ← T(r_t)                    // 原 Pyramid
13.        shares ← round(amount / P_t);執行買入
14.        賣出邏輯:與現行完全相同(最低買入價獲利超過 g* 即賣出 S 元)
15. 以各股票最後收盤價結算未實現部位,回傳指標
```

具體例子(一維):假設 $P_t$ 10 天內從 100 連續漲到 118,其中第 5、7 天各有小幅回檔至 MA20 下方 1%。Baseline 因 $H_t$ 被每日買入往上推,$r_t \approx -0.01$,兩次都落到最淺 tier,各下單小額。本方法在 $\tau = 0.02$ 下,這 10 天內 $\mathrm{MA}_{20} > 1.02\cdot\mathrm{MA}_{60}$ 成立,$B_t = 1$,兩次買入改以 $A_{\text{TF}} = \alpha \cdot \max_r \mathcal{T}(r)$ 下單——在 $\alpha = 2$ 時相當於原本最大 tier 的兩倍。

### 4. Experimental Setup

#### 4.1 Baselines(來自當前 codebase)
- `Pyramid-Baseline`:以 codebase 在當前 commit 下設定的 Pyramid 參數組合跑完整段 3600 日回測。訓練/評估協議:按日線序貫模擬、以股池共用日期聯集為時間軸、以日收盤價為價格源、以(期末現金 + 以最末可得收盤價結算的未實現持股市值)為最終權益指標。
- `BuyHold-5050`(對照用非 Pyramid 參考點):將 $C_0$ 平均分配至 $D$ 中各股票、首日全買、之後不動作。用於確認新方法至少勝過「完全不擇時」。

新方法沿用 `Pyramid-Baseline` 的**所有**訓練與評估協議(日序、價格源、期末結算方式、冷卻期、賣出邏輯、股池、初始現金),**只替換買入金額決策這一個組件**。

#### 4.2 Datasets
- $D = \{006208, 00830\}$:沿用 codebase 現有股池。
- 期間:最近 3600 交易日(≈14.7 年),沿用 baseline 的股池日期聯集規則。
- 前處理:使用歷史日收盤價序列,MA20 與 MA60 在序列讀入後一次性計算,視窗不足日期以缺值表示並於當日直接跳過。

#### 4.3 Metrics
- **主要**:$\mathrm{FinalTotal} = \mathrm{FinalCash} + \mathrm{FinalHoldingValue}$。
- **輔助**:
  - $\mathrm{MaxDD}$:模擬期間每日總權益(cash + 即時持股市值)序列的最大回撤,以百分比表示。
  - $\mathrm{CashUtil}$:全期間「平均持股市值 / 起始現金」——量化現金是否真的被使用。
  - $\mathrm{TotalBuys}$、$\mathrm{TotalSells}$:交易頻次,用於偵測退化成 over-trading。

#### 4.4 Hyperparameters
| 名稱 | 類型 | 值 / 範圍 | 理由 |
|---|---|---|---|
| TF 趨勢閾值 $\tau$ | **scientific** | $\{0,\ 0.02,\ 0.05\}$ | 本輪要回答「牛市判定鬆緊」是否影響新方法有效性 |
| TF 金額形式 $\theta$ | **scientific** | $\{\alpha=2\text{(常數型)},\ \beta=0.02\text{(比例型)}\}$ | 本輪要回答常數型還是本金比例型更合適 |
| 趨勢判定均線組 $(s, \ell)$ | nuisance | 固定 $(20, 60)$ | 常用且已有 MA20 程式碼可直接擴充 MA60;其他組合留到 Plan v2+ |
| 冷卻天數 $K$ | fixed | 14 | 沿用 baseline |
| 賣出門檻 $g^*$ | fixed | 1.0 | 沿用 baseline,本輪不研究賣出 |
| 賣出金額 $S$ | fixed | baseline 值 | 沿用 baseline |
| 起始現金 $C_0$ | fixed | baseline 值 | 沿用 baseline |
| 追蹤股池 $D$ | fixed | baseline 值 | 沿用 baseline |

#### 4.5 Ablations(Karpathy recipe:一次加一個要素)
| Variant | 新增趨勢判定 $B_t$ | 新增 TF 金額 $A_{\text{TF}}$ | 預期 |
|---|---|---|---|
| `L0_BH5050` | — | — | 參考下界,驗證至少該贏 |
| `L1_Baseline` | ✗ | ✗ | 當前 codebase 的 Pyramid,主對照 |
| `L2_TF_only` | ✓ | ✓(固定 $\alpha=2$、$\tau=0.02$ 一組) | 驗 TF 分支是否有獨立正貢獻 |
| `L3_TF_tauSweep` | ✓ | ✓(固定 $\alpha=2$,掃 $\tau \in \{0,0.02,0.05\}$) | 驗 scientific hparam $\tau$ 的敏感度 |
| `L4_TF_form` | ✓ | ✓(固定最佳 $\tau$,切換 $\theta$ 兩形式) | 驗金額形式是常數型優還是比例型優 |
| `Full` | ✓ | ✓(最佳 $\tau$ + 最佳 $\theta$) | 主結果 |

每一個 variant 相對於上一列只多一個變因;因此任一變異產生的表現差距可以明確歸因於該變因。

### 5. Expected Outcomes

#### 5.1 Success Criteria
- $\mathrm{FinalTotal}(\texttt{Full}) \geq \mathrm{FinalTotal}(\texttt{L1\_Baseline}) + 0.05 \cdot C_0$。
- $\mathrm{MaxDD}(\texttt{Full})$ 比 baseline 惡化不超過 5 個百分點。
- Ablation 單調性(軟性期望):$\texttt{L1\_Baseline} \lesssim \texttt{L2\_TF\_only} \leq \texttt{Full}$;$\texttt{L0\_BH5050}$ 不必被超越,但若被大幅超越可加強信心。
- $\mathrm{CashUtil}(\texttt{Full}) > \mathrm{CashUtil}(\texttt{L1\_Baseline})$——驗證現金確實比較有被用到。

#### 5.2 Falsification Criteria
- 所有被試的 $\tau$ 與 $\theta$ 組合下,$\mathrm{FinalTotal}(\texttt{Full}) < \mathrm{FinalTotal}(\texttt{L1\_Baseline})$;或
- $\mathrm{MaxDD}(\texttt{Full}) - \mathrm{MaxDD}(\texttt{L1\_Baseline}) > 10$ 個百分點。

若被證偽,視為「多頭趨勢判定 + 金額切換」這條路徑不成立,Plan v2 直接跳到 §6 的第一條 Fallback。

### 6. Risks & Fallbacks
- **Risk 1**:盤整市被誤判為多頭 → 高檔追價 → MaxDD 惡化。緩解:$\tau$ 取較保守的 0.05 作主變異之一,並看 MaxDD 是否突破上限。
- **Risk 2**:3600 日區間本身包含強勁大多頭,任何「買更多」都會贏 baseline → 結果可能只是幸運。緩解:另外做 split 檢驗(前半、後半各一次),若兩段都贏,信心較高;若只靠單一強多頭撐起 PnL,結論下修為「在此期間有效」。
- **Risk 3**:Fallback1——若 §5.2 被觸發,Plan v2 改走 issue #2(現金儲備 / 止損):全面放大 $\mathcal{T}$ 的金額並加入固定比例止損,不再動趨勢判定。
- **Risk 4**:Fallback2——若 Fallback1 亦無效,則回到更基本的問題:在現有買點訊號(「$P_t < \mathrm{MA}_{20}$」)下,策略本身的可改進空間可能已接近耗盡,下一輪改走「換買點訊號」(e.g., RSI、布林通道下軌)。

### 7. Compute & Dependencies
- 主實驗:單 variant 在 3600 日 × 2 檔股池的日序模擬上預估 < 30 秒 CPU-time;全部 6 個 variant 合計 < 5 分鐘。
- 硬體:任何一台可跑 Go + MariaDB 的開發機即可;無需 GPU。
- 非標準軟體依賴:無——baseline 既有的 DB driver、YAML 讀取、dotenv 載入均已於 codebase 依賴清單內鎖版,本輪不引入新套件。
