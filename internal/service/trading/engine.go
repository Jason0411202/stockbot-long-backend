// internal/service/trading/engine.go 實作上線與回測共用的純記憶體交易引擎。
package trading

import (
	"fmt"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"math"
	"sort"
	"time"
)

// engine.go 為上線與回測共用的純 in-memory 模擬器。零 I/O 依賴 (無 DB / Discord / app_context);
// 價格來源一律走呼叫端傳入的 StockSeries。資料載入 (DB / CSV) 由外層套件負責。

// DayRecorder 為「選用」的觀測回呼,讓回測/評估可在不改變引擎決策的前提下,
// 收集每日權益曲線與對外現金流。上線模式不掛 recorder (rec == nil),故行為完全不變。
//
// 設計成「struct of callbacks 的可空指標 field」而非擴充 Executor 介面 —— 因為 Executor
// 有兩個既有實作 (NoopExecutor / dbExecutor) 且只在成交時被呼叫,無法觀測「無成交日」的權益點。
type DayRecorder struct {
	// OnCashflow 在每次實際成交後觸發。買入為負、賣出為正 (金額為夾取後的真實成交額)。
	OnCashflow func(day time.Time, amount float64)
	// OnEquity 在每個處理日結束時觸發一次 (即使當日無成交),回報帳戶總權益 / 現金 / 持股市值。
	OnEquity func(day time.Time, equity, cash, holdings float64)
}

// Executor 是引擎將 Intent 套用後通知副作用的回呼介面。
//   - 回測模式:使用 NoopExecutor,不產生任何 DB / Discord 副作用。
//   - 上線模式:寫入 UnrealizedGainsLosses / RealizedGainsLosses,並可選擇是否發 Discord。
//
// 引擎內部的「決策 + 現金 / 持倉變動」對兩種模式完全相同;Executor 只負責持久化與通知。
type Executor interface {
	OnBuyApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64, reason TradeReason) error
	OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64, reason TradeReason) error
}

// NoopExecutor 是回測用,什麼副作用都不做。
type NoopExecutor struct{}

// OnBuyApplied 接收買進成交但不執行任何副作用。
func (NoopExecutor) OnBuyApplied(string, time.Time, int, float64, float64, TradeReason) error {
	return nil
}

// OnSellApplied 接收賣出成交但不執行任何副作用。
func (NoopExecutor) OnSellApplied(string, time.Time, int, float64, float64, TradeReason) error {
	return nil
}

// Engine 是上線與回測共用的 in-memory 模擬器。
// 它持有「策略觀點下的真實狀態」:現金、未實現持倉、每檔股票最後買入日。
// 上線模式啟動時會從 DB 還原這些狀態,使引擎與真實 DB 內容一致。
type Engine struct {
	cfg           *config.Config
	cash          float64
	positions     map[string][]lot
	lastBuy       map[string]time.Time
	peakSinceHold map[string]float64     // 持倉期間最高收盤 (移動停利用);全出後歸零
	breakDates    map[string][]time.Time // 每檔歷次「動用打破冷卻額度」的日期 (滾動視窗計數用)
	lastTrailSell map[string]time.Time   // 每檔最後一次移動停利出場日 (出場後暫停買入用);未持久化,重啟靠 catch-up 回放重建

	totalBuys   int
	totalSells  int
	skippedBuys int

	trailSells  int // 移動停利觸發的賣出次數
	profitSells int // 獲利了結觸發的賣出次數

	rec *DayRecorder // 選用觀測者;nil 表示不收集 (上線模式)。
}

// EngineStats 為引擎自啟動以來的累計事件數。
type EngineStats struct {
	TotalBuys   int
	TotalSells  int
	SkippedBuys int // 想買但可動用現金連 1 股都不夠 → 完全沒買成
	TrailSells  int // 移動停利觸發次數
	ProfitSells int // 獲利了結觸發次數
}

// NewEngine 建立空狀態的引擎,起始現金為 cfg.InitialCash。
func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg:           cfg,
		cash:          cfg.InitialCash,
		positions:     make(map[string][]lot, len(cfg.TrackStocks)),
		lastBuy:       make(map[string]time.Time, len(cfg.TrackStocks)),
		peakSinceHold: make(map[string]float64, len(cfg.TrackStocks)),
		breakDates:    make(map[string][]time.Time, len(cfg.TrackStocks)),
		lastTrailSell: make(map[string]time.Time, len(cfg.TrackStocks)),
	}
}

// SetRecorder 掛上選用觀測者 (回測/評估用)。傳 nil 可卸除。上線模式不呼叫此方法。
func (e *Engine) SetRecorder(r *DayRecorder) { e.rec = r }

// SeedCash 由外部 (上線啟動) 指定起始現金,覆蓋預設的 cfg.InitialCash。
func (e *Engine) SeedCash(cash float64) { e.cash = cash }

// AddCash 把一筆外部注資 (定期定額) 加進現金池,當日即可動用。amount <= 0 為 no-op。
// 用於「每月解鎖新資金」問題設定;回測由 backtest 逐視窗注入,上線由 TradingService 在 catch-up
// 回放與每日 loop 依同一排程 (backtest.ContributionDue) 注入,兩邊現金軌跡一致。
func (e *Engine) AddCash(amount float64) {
	if amount > 0 {
		e.cash += amount
	}
}

// SeedPosition 餵入既有 lot (上線啟動從 UnrealizedGainsLosses 還原狀態)。
func (e *Engine) SeedPosition(stockID string, date time.Time, shares int, price float64) {
	if shares <= 0 || price <= 0 {
		return
	}
	e.positions[stockID] = append(e.positions[stockID], lot{date: date, shares: shares, price: price})
}

// SeedLastBuy 餵入既有最後買入日 (上線啟動從 UnrealizedGainsLosses ∪ RealizedGainsLosses 還原)。
func (e *Engine) SeedLastBuy(stockID string, date time.Time) {
	e.lastBuy[stockID] = date
}

// Cash 回傳當前現金。
func (e *Engine) Cash() float64 { return e.cash }

// PositionCount 回傳某檔目前持有的 lot 筆數 (供跨套件測試在不暴露內部結構下檢視持倉狀態)。
func (e *Engine) PositionCount(stockID string) int { return len(e.positions[stockID]) }

// LastBuy 回傳某檔最後買入日 (供跨套件測試檢視冷卻基準);無紀錄時 ok=false。
func (e *Engine) LastBuy(stockID string) (time.Time, bool) {
	d, ok := e.lastBuy[stockID]
	return d, ok
}

// Stats 回傳累計統計。
func (e *Engine) Stats() EngineStats {
	return EngineStats{
		TotalBuys:   e.totalBuys,
		TotalSells:  e.totalSells,
		SkippedBuys: e.skippedBuys,
		TrailSells:  e.trailSells,
		ProfitSells: e.profitSells,
	}
}

// HoldingValueAsOf 以「day 當天或之前最近收盤價」結算所有持股市值 (as-of 估值)。
// 用於建立每日權益曲線,以及替「結束日 < 全序列最後日」的視窗正確收尾。
// 尚未上市 (CloseAsOf 回傳 false) 的股票貢獻 0,符合事實。
func (e *Engine) HoldingValueAsOf(series map[string]*StockSeries, day time.Time) float64 {
	total := 0.0
	for stockID, pos := range e.positions {
		if len(pos) == 0 {
			continue
		}
		s, ok := series[stockID]
		if !ok {
			continue
		}
		price, ok := s.CloseAsOf(day)
		if !ok {
			continue
		}
		for _, l := range pos {
			total += float64(l.shares) * price
		}
	}
	return total
}

// CostBasis 回傳目前所有持倉的總投入成本 (Σ 各 lot 股數 × 買入價);無持倉回傳 0。
// 純讀取記憶體持倉,零 I/O、不參與任何決策 (供「未實現損益 = 持股市值 − 成本基礎」計算)。
func (e *Engine) CostBasis() float64 {
	total := 0.0
	for _, lots := range e.positions {
		for _, l := range lots {
			total += float64(l.shares) * l.price
		}
	}
	return total
}

// ProcessDay 處理單一日期下所有追蹤股票的買賣決策。
// 依 cfg.DecisionPriceBasis 決定成交價基準:
//   - "close"(預設):用當日收盤價成交,指標看到當日收盤 (asOfIdx = idx)。
//   - "open"        :用當日開盤價成交,指標只看到前一交易日收盤 (asOfIdx = idx-1;idx==0 跳過)。
//
// 單檔流程委派給 processStock (買→更新峰值→賣→通知 Executor),使 close / open / 線上三條路徑共用同一決策核心。
func (e *Engine) ProcessDay(today time.Time, series map[string]*StockSeries, exec Executor) error {
	todayStr := today.Format("2006-01-02")
	openBasis := e.cfg.DecisionPriceBasis == "open"
	// BuyFracBasis=="equity" 時先算當日總權益;open 基準以前一交易日估值 (不偷看當日收盤);cash 基準零成本略過。
	needEquity := e.cfg.BuyFracBasis == "equity"
	eqToday := 0.0
	if needEquity {
		eqAsOfDay := today
		if openBasis {
			eqAsOfDay = today.AddDate(0, 0, -1)
		}
		eqToday = e.cash + e.HoldingValueAsOf(series, eqAsOfDay)
	}
	for _, stockID := range e.cfg.TrackStocks {
		s, ok := series[stockID]
		if !ok {
			continue
		}
		idx, ok := s.DateIndex[todayStr]
		if !ok {
			continue
		}

		// 依決策基準決定成交價與「指標可見的最後一筆收盤索引」。
		decisionPrice := s.ClosePrices[idx]
		asOfIdx := idx
		if openBasis {
			if idx == 0 {
				continue // 無前一交易日收盤,無法在不偷看當日收盤下決策
			}
			decisionPrice = s.OpenAt(idx)
			asOfIdx = idx - 1
		}
		if err := e.processStock(stockID, today, decisionPrice, asOfIdx, s, exec, eqToday, needEquity); err != nil {
			return err
		}
	}
	if e.cash < 0 {
		return fmt.Errorf("internal invariant violated: cash went negative (%.6f) on %s", e.cash, todayStr)
	}
	// 每個處理日結束 (含無成交日) 記錄一次權益點;以 as-of 估值,故跨上市日空窗也正確。
	// rec == nil (上線模式) 時整段略過,僅多一次 nil 判斷,行為與原本一致。
	if e.rec != nil && e.rec.OnEquity != nil {
		holdings := e.HoldingValueAsOf(series, today)
		e.rec.OnEquity(today, e.cash+holdings, e.cash, holdings)
	}
	return nil
}

// ProcessOpenDecision 為線上「開盤價基準」決策進入點。
// series 僅含到前一交易日 (T-1) 的收盤;opens 提供當日 (T) 各股即時開盤價。
// 每檔以 asOfIdx = 最新收盤索引 (= T-1) 計算指標、用即時開盤價成交,與回測 open 基準共用 processStock,
// 確保線上 / 回測決策邏輯完全一致。未提供開盤價或 <=0 的股票直接略過 (呼叫端負責重試 / fallback)。
func (e *Engine) ProcessOpenDecision(today time.Time, opens map[string]float64, series map[string]*StockSeries, exec Executor) error {
	needEquity := e.cfg.BuyFracBasis == "equity"
	eqToday := 0.0
	if needEquity {
		// series 最末筆即 T-1,as-of(today) 估值自然落在前一交易日收盤,無未來資訊。
		eqToday = e.cash + e.HoldingValueAsOf(series, today)
	}
	for _, stockID := range e.cfg.TrackStocks {
		s, ok := series[stockID]
		if !ok || len(s.Dates) == 0 {
			continue
		}
		openPx, ok := opens[stockID]
		if !ok || openPx <= 0 {
			continue
		}
		asOfIdx := len(s.Dates) - 1
		if err := e.processStock(stockID, today, openPx, asOfIdx, s, exec, eqToday, needEquity); err != nil {
			return err
		}
	}
	if e.cash < 0 {
		return fmt.Errorf("internal invariant violated: cash went negative (%.6f) on %s", e.cash, today.Format("2006-01-02"))
	}
	if e.rec != nil && e.rec.OnEquity != nil {
		holdings := e.HoldingValueAsOf(series, today)
		e.rec.OnEquity(today, e.cash+holdings, e.cash, holdings)
	}
	return nil
}

// processStock 對單檔股票執行「買入 → 更新持倉峰值 → 賣出」並通知 Executor。
// decisionPrice 為當日成交價 (close 基準=當日收盤;open 基準=當日開盤);
// asOfIdx 為「指標可見到的最後一筆收盤索引」(close 基準=當日;open 基準=前一交易日),
// 進場均線 / 牛熊判定 / 近期高點皆截至 asOfIdx,確保 open 基準在開盤決策時不使用尚未發生的當日收盤。
func (e *Engine) processStock(stockID string, today time.Time, decisionPrice float64, asOfIdx int, s *StockSeries, exec Executor, eqToday float64, needEquity bool) error {
	// 成交價無效或無可見收盤 (尚未上市 / 第一天) 直接略過。
	if decisionPrice <= 0 || asOfIdx < 0 || asOfIdx >= len(s.ClosePrices) {
		return nil
	}

	// 套用該股 per-stock override (無 override 時 == 共用 cfg,零成本)。其後該股決策一律用 eff。
	eff := e.cfg.ForStock(stockID)

	// 進場均線:預設用預先算好的 20MA;若 eff.MAWindow 指定其他長度則用 PrefixClose O(1) 重算。皆截至 asOfIdx。
	entryMA := s.MA20[asOfIdx]
	if eff.MAWindow > 0 && eff.MAWindow != 20 {
		entryMA = s.maAt(asOfIdx, eff.MAWindow)
	}

	// 牛熊判定 (一天一次);套到買賣兩個 snapshot。
	isBullToday := false
	if eff.RegimeMethod != "" {
		isBullToday = regimeBull(eff, s, asOfIdx)
	}

	// === 買入 ===
	snap := e.buildSnapshot(stockID, today, decisionPrice, entryMA)
	e.applyGateInputs(&snap, s, asOfIdx)
	snap.IsBull = isBullToday
	snap.Cash = e.cash
	if needEquity {
		snap.Equity = eqToday
	}
	if eff.CooldownBreakBudget > 0 {
		snap.CooldownBreaksLeft = eff.CooldownBreakBudget - e.breaksInWindow(eff, stockID, today)
	}
	// 移動停利出場後的「暫停買入」閘:避免空頭中「停損→隔日又逢低買→再停損」的 whipsaw 循環 (zero-value 不暫停)。
	reentryBlocked := false
	if eff.TrailReentryCooldownDays > 0 {
		if ts, ok := e.lastTrailSell[stockID]; ok &&
			today.Sub(ts) < time.Duration(eff.TrailReentryCooldownDays)*24*time.Hour {
			reentryBlocked = true
		}
	}
	if !reentryBlocked {
		if buy := DecideBuy(eff, snap); buy.Should {
			if err := e.applyBuy(stockID, today, buy, exec); err != nil {
				return err
			}
		}
	}

	// 更新持倉峰值 (移動停利用):有持倉才追蹤,含今日成交價與剛買進的部位。
	if len(e.positions[stockID]) > 0 && decisionPrice > e.peakSinceHold[stockID] {
		e.peakSinceHold[stockID] = decisionPrice
	}

	// === 賣出 (重新組 Snapshot:剛買的 lot 也可能影響 lowest / highest) ===
	snap = e.buildSnapshot(stockID, today, decisionPrice, entryMA)
	e.applyGateInputs(&snap, s, asOfIdx)
	snap.IsBull = isBullToday
	if sell := DecideSell(eff, snap); sell.Should {
		if err := e.applySell(stockID, today, sell, exec); err != nil {
			return err
		}
	}
	return nil
}

// ProcessDates 對升冪日期序列連續呼叫 ProcessDay。
func (e *Engine) ProcessDates(dates []time.Time, series map[string]*StockSeries, exec Executor) error {
	for _, d := range dates {
		if err := e.ProcessDay(d, series, exec); err != nil {
			return err
		}
	}
	return nil
}

// buildSnapshot 依目前現金、持倉與價格建立單檔股票的決策輸入。
func (e *Engine) buildSnapshot(stockID string, today time.Time, todayPrice, ma20 float64) Snapshot {
	highest := -1.0
	lowest := math.MaxFloat64
	heldShares := 0
	hasLot := false
	for _, l := range e.positions[stockID] {
		if l.shares <= 0 {
			continue
		}
		hasLot = true
		heldShares += l.shares
		if l.price > highest {
			highest = l.price
		}
		if l.price < lowest {
			lowest = l.price
		}
	}
	if !hasLot {
		lowest = -1
	}
	lb, hasLB := e.lastBuy[stockID]
	return Snapshot{
		StockID:          stockID,
		Today:            today,
		TodayPrice:       todayPrice,
		MA20:             ma20,
		HighestHeldPrice: highest,
		LowestHeldPrice:  lowest,
		HasLastBuy:       hasLB,
		LastBuyDate:      lb,
		HeldShares:       heldShares,
		PeakSinceHold:    e.peakSinceHold[stockID],
	}
}

// breaksInWindow 回傳近 cfg.CooldownBreakWindowDays 日曆日內 (不含界外) 同一檔已動用的「打破冷卻」次數。
// 收 cfg 參數以支援 per-stock override (窗長可能各股不同)。
func (e *Engine) breaksInWindow(cfg *config.Config, stockID string, today time.Time) int {
	w := cfg.CooldownBreakWindowDays
	if w <= 0 {
		w = 365 // ≈252 交易日≈1 年
	}
	cutoff := today.Add(-time.Duration(w) * 24 * time.Hour)
	n := 0
	for _, d := range e.breakDates[stockID] {
		if d.After(cutoff) {
			n++
		}
	}
	return n
}

// applyGateInputs 依 cfg 旗標「按需」把選用指標填入 snapshot (近期高點 RecentPeak,供 peak 深度基準 /
// 熊市現金比例的深度權重使用)。近期高點截至 asOfIdx (open 基準時為前一交易日,不含當日收盤)。
// IsBull / Cash / Equity / CooldownBreaksLeft 由 processStock 設定。
func (e *Engine) applyGateInputs(snap *Snapshot, s *StockSeries, asOfIdx int) {
	if e.cfg.BuyDepthBasis == "peak" {
		lb := e.cfg.BuyPeakLookback
		if lb <= 0 {
			lb = 252
		}
		snap.RecentPeak = s.peakAt(asOfIdx, lb)
	}
}

// regimeBull 依 c.RegimeMethod 判定 (stock, idx) 當日是否為多頭。free function 以支援 per-stock override。
// 資料不足 (NaN / 回看越界) 一律回 false (= bear/中性,維持嚴格逢低買的保守行為)。
func regimeBull(c *config.Config, s *StockSeries, idx int) bool {
	w := c.RegimeMAWindow
	if w <= 0 {
		w = 200
	}
	lb := c.RegimeLookback
	switch c.RegimeMethod {
	case "ma_pos":
		ma := s.maAt(idx, w)
		return !math.IsNaN(ma) && s.ClosePrices[idx] > ma
	case "ma_slope":
		if lb <= 0 {
			lb = 200
		}
		ma := s.maAt(idx, w)
		prev := s.maAt(idx-lb, w)
		return !math.IsNaN(ma) && !math.IsNaN(prev) && ma > prev
	case "mom":
		if lb <= 0 {
			lb = 252
		}
		if idx-lb < 0 {
			return false
		}
		return s.ClosePrices[idx] > s.ClosePrices[idx-lb]
	}
	return false
}

// applyBuy 將買進意圖套用到現金與持倉，並通知 executor 寫入副作用。
func (e *Engine) applyBuy(stockID string, today time.Time, intent BuyIntent, exec Executor) error {
	shares := intent.Shares
	// === 防作弊現金夾取:可利用資金僅為當前持有現金,不得借錢 ===
	maxAffordable := 0
	if intent.Price > 0 {
		maxAffordable = int(math.Floor(e.cash / intent.Price))
	}
	if shares > maxAffordable {
		shares = maxAffordable
	}
	if shares <= 0 {
		e.skippedBuys++
		return nil
	}
	cost := float64(shares) * intent.Price
	e.cash -= cost
	if e.cash < 0 {
		return fmt.Errorf("internal invariant violated: cash went negative (%.6f) after buy of %s on %s",
			e.cash, stockID, today.Format("2006-01-02"))
	}
	e.positions[stockID] = append(e.positions[stockID], lot{
		date:   today,
		shares: shares,
		price:  intent.Price,
	})
	e.lastBuy[stockID] = today
	if intent.BrokeCooldown {
		e.breakDates[stockID] = append(e.breakDates[stockID], today) // 記錄一次「打破冷卻」(滾動視窗計數)
	}
	e.totalBuys++
	// 觀測者記錄夾取後的真實成交額 (買入為負現金流)。被夾取到 0 股的情況已在上面提前 return。
	if e.rec != nil && e.rec.OnCashflow != nil {
		e.rec.OnCashflow(today, -cost)
	}
	// 補上成交端理由欄位 (夾取後的實際股數 / 金額 / 餘額),供執行器寫 log 與發通知。
	reason := intent.TradeReason
	reason.Shares = shares
	reason.Price = intent.Price
	reason.Amount = cost
	reason.CashAfter = e.cash
	return exec.OnBuyApplied(stockID, today, shares, intent.Price, e.cash, reason)
}

// applySell 將賣出意圖套用到現金與持倉，並通知 executor 寫入副作用。
func (e *Engine) applySell(stockID string, today time.Time, intent SellIntent, exec Executor) error {
	pos := e.positions[stockID]
	if len(pos) == 0 {
		return nil
	}
	// 從成本最低 (相同價格時取較早) 的 lot 開始賣 — 與 DB 端
	// GetLowestUnrealizedGainsLossesRecord 的 ORDER BY 一致,確保兩邊行為等價。
	sort.SliceStable(pos, func(i, j int) bool {
		if pos[i].price != pos[j].price {
			return pos[i].price < pos[j].price
		}
		return pos[i].date.Before(pos[j].date)
	})
	remaining := intent.TargetShares
	soldShares := 0
	newPos := make([]lot, 0, len(pos))
	for _, l := range pos {
		if remaining <= 0 {
			newPos = append(newPos, l)
			continue
		}
		if l.shares <= remaining {
			e.cash += float64(l.shares) * intent.Price
			remaining -= l.shares
			soldShares += l.shares
			e.totalSells++
		} else {
			e.cash += float64(remaining) * intent.Price
			l.shares -= remaining
			soldShares += remaining
			remaining = 0
			e.totalSells++
			newPos = append(newPos, l)
		}
	}
	e.positions[stockID] = newPos
	if len(newPos) == 0 {
		e.peakSinceHold[stockID] = 0 // 全數出場 → 峰值歸零,下次建倉重新追蹤
	}
	if soldShares > 0 {
		if intent.Reason == "trail" {
			e.trailSells++
			e.lastTrailSell[stockID] = today // 記錄出場日,供「暫停買入」閘判定
		} else {
			e.profitSells++
		}
		if e.rec != nil && e.rec.OnCashflow != nil {
			e.rec.OnCashflow(today, float64(soldShares)*intent.Price)
		}
		// 補上成交端理由欄位 (實際賣出股數 / 金額 / 餘額),供執行器寫 log 與發通知。
		reason := intent.TradeReason
		reason.Shares = soldShares
		reason.Price = intent.Price
		reason.Amount = float64(soldShares) * intent.Price
		reason.CashAfter = e.cash
		return exec.OnSellApplied(stockID, today, soldShares, intent.Price, e.cash, reason)
	}
	return nil
}
