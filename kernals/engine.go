package kernals

import (
	"fmt"
	"main/app_context"
	"main/config"
	"math"
	"sort"
	"time"
)

// lot 為記憶體中的單筆未實現持倉。
type lot struct {
	date   time.Time
	shares int
	price  float64
}

// stockSeries 為單一股票經整理後的歷史資料,供引擎查價與 MA20。
type stockSeries struct {
	dates       []time.Time // asc
	dateIndex   map[string]int
	closePrices []float64
	ma20        []float64 // ma20[i] = 截至 dates[i] 的 20 日均價;不足 20 日以 NaN 表示
}

// closeAsOf 回傳「在 day 當天或之前最近一個交易日」的收盤價 (as-of 查價)。
// 用於在「聯集日期」上替某檔沒交易的股票估值 (例如某檔放假、或尚未上市)。
//   - day 早於該股第一筆資料 (尚未上市) -> (0, false)。
//   - 其餘 -> 最近一個 <= day 的收盤價, true。
//
// 只看過去資料,絕無未來資訊洩漏;O(log n) 走既有已排序的 dates。
// 注意:不可用 dateIndex (只含精確交易日),也不可用 closePrices[len-1] (那是全序列最後價)。
func (s *stockSeries) closeAsOf(day time.Time) (float64, bool) {
	i := sort.Search(len(s.dates), func(i int) bool { return s.dates[i].After(day) })
	if i == 0 {
		return 0, false
	}
	return s.closePrices[i-1], true
}

// DayRecorder 為「選用」的觀測回呼,讓回測/評估可在不改變引擎決策的前提下,
// 收集每日權益曲線與對外現金流。上線模式不掛 recorder (rec == nil),故行為完全不變。
//
// 設計成「struct of callbacks 的可空指標 field」而非擴充 Executor 介面 —— 因為 Executor
// 有兩個既有實作 (noopExecutor / dbExecutor) 且只在成交時被呼叫,無法觀測「無成交日」的權益點。
type DayRecorder struct {
	// OnCashflow 在每次實際成交後觸發。買入為負、賣出為正 (金額為夾取後的真實成交額)。
	OnCashflow func(day time.Time, amount float64)
	// OnEquity 在每個處理日結束時觸發一次 (即使當日無成交),回報帳戶總權益 / 現金 / 持股市值。
	OnEquity func(day time.Time, equity, cash, holdings float64)
}

// Executor 是引擎將 Intent 套用後通知副作用的回呼介面。
//   - 回測模式:使用 noopExecutor,不產生任何 DB / Discord 副作用。
//   - 上線模式:寫入 UnrealizedGainsLosses / RealizedGainsLosses,並可選擇是否發 Discord。
//
// 引擎內部的「決策 + 現金 / 持倉變動」對兩種模式完全相同;Executor 只負責持久化與通知。
type Executor interface {
	OnBuyApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error
	OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error
}

// noopExecutor 是回測用,什麼副作用都不做。
type noopExecutor struct{}

func (noopExecutor) OnBuyApplied(string, time.Time, int, float64, float64) error  { return nil }
func (noopExecutor) OnSellApplied(string, time.Time, int, float64, float64) error { return nil }

// Engine 是上線與回測共用的 in-memory 模擬器。
// 它持有「策略觀點下的真實狀態」:現金、未實現持倉、每檔股票最後買入日。
// 上線模式啟動時會從 DB 還原這些狀態,使引擎與真實 DB 內容一致。
type Engine struct {
	cfg       *config.Config
	cash      float64
	positions map[string][]lot
	lastBuy   map[string]time.Time

	totalBuys   int
	totalSells  int
	skippedBuys int

	rec *DayRecorder // 選用觀測者;nil 表示不收集 (上線模式)。
}

// EngineStats 為引擎自啟動以來的累計事件數。
type EngineStats struct {
	TotalBuys   int
	TotalSells  int
	SkippedBuys int
}

// NewEngine 建立空狀態的引擎,起始現金為 cfg.InitialCash。
func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg:       cfg,
		cash:      cfg.InitialCash,
		positions: make(map[string][]lot, len(cfg.TrackStocks)),
		lastBuy:   make(map[string]time.Time, len(cfg.TrackStocks)),
	}
}

// SetRecorder 掛上選用觀測者 (回測/評估用)。傳 nil 可卸除。上線模式不呼叫此方法。
func (e *Engine) SetRecorder(r *DayRecorder) { e.rec = r }

// SeedCash 由外部 (上線啟動) 指定起始現金,覆蓋預設的 cfg.InitialCash。
func (e *Engine) SeedCash(cash float64) { e.cash = cash }

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

// Stats 回傳累計統計。
func (e *Engine) Stats() EngineStats {
	return EngineStats{
		TotalBuys:   e.totalBuys,
		TotalSells:  e.totalSells,
		SkippedBuys: e.skippedBuys,
	}
}

// HoldingValueAsOf 以「day 當天或之前最近收盤價」結算所有持股市值 (as-of 估值)。
// 用於建立每日權益曲線,以及替「結束日 < 全序列最後日」的視窗正確收尾。
// 尚未上市 (closeAsOf 回傳 false) 的股票貢獻 0,符合事實。
func (e *Engine) HoldingValueAsOf(series map[string]*stockSeries, day time.Time) float64 {
	total := 0.0
	for stockID, pos := range e.positions {
		if len(pos) == 0 {
			continue
		}
		s, ok := series[stockID]
		if !ok {
			continue
		}
		price, ok := s.closeAsOf(day)
		if !ok {
			continue
		}
		for _, l := range pos {
			total += float64(l.shares) * price
		}
	}
	return total
}

// FinalHoldingValue 以每檔股票最後可得收盤價結算持股市值。
func (e *Engine) FinalHoldingValue(series map[string]*stockSeries) float64 {
	total := 0.0
	for stockID, pos := range e.positions {
		if len(pos) == 0 {
			continue
		}
		s, ok := series[stockID]
		if !ok || len(s.closePrices) == 0 {
			continue
		}
		lastPrice := s.closePrices[len(s.closePrices)-1]
		for _, l := range pos {
			total += float64(l.shares) * lastPrice
		}
	}
	return total
}

// ProcessDay 處理單一日期下所有追蹤股票的買賣決策。
// 流程 (對每檔股票):
//  1. 用記憶體狀態組 Snapshot
//  2. DecideBuy → 套用 (帶現金夾取)
//  3. 重新組 Snapshot → DecideSell → 套用
//  4. 通知 Executor (上線:寫 DB / 發 Discord;回測:no-op)
func (e *Engine) ProcessDay(today time.Time, series map[string]*stockSeries, exec Executor) error {
	todayStr := today.Format("2006-01-02")
	for _, stockID := range e.cfg.TrackStocks {
		s, ok := series[stockID]
		if !ok {
			continue
		}
		idx, ok := s.dateIndex[todayStr]
		if !ok {
			continue
		}
		todayPrice := s.closePrices[idx]
		if todayPrice <= 0 {
			continue
		}

		// === 買入 ===
		snap := e.buildSnapshot(stockID, today, todayPrice, s.ma20[idx])
		if buy := DecideBuy(e.cfg, snap); buy.Should {
			if err := e.applyBuy(stockID, today, buy, exec); err != nil {
				return err
			}
		}

		// === 賣出 (重新組 Snapshot:剛買的 lot 也可能影響 lowest / highest) ===
		snap = e.buildSnapshot(stockID, today, todayPrice, s.ma20[idx])
		if sell := DecideSell(e.cfg, snap); sell.Should {
			if err := e.applySell(stockID, today, sell, exec); err != nil {
				return err
			}
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

// ProcessDates 對升冪日期序列連續呼叫 ProcessDay。
func (e *Engine) ProcessDates(dates []time.Time, series map[string]*stockSeries, exec Executor) error {
	for _, d := range dates {
		if err := e.ProcessDay(d, series, exec); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) buildSnapshot(stockID string, today time.Time, todayPrice, ma20 float64) Snapshot {
	highest := -1.0
	lowest := math.MaxFloat64
	hasLot := false
	for _, l := range e.positions[stockID] {
		if l.shares <= 0 {
			continue
		}
		hasLot = true
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
	}
}

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
	e.totalBuys++
	// 觀測者記錄夾取後的真實成交額 (買入為負現金流)。被夾取到 0 股的情況已在上面提前 return。
	if e.rec != nil && e.rec.OnCashflow != nil {
		e.rec.OnCashflow(today, -cost)
	}
	return exec.OnBuyApplied(stockID, today, shares, intent.Price, e.cash)
}

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
	if soldShares > 0 {
		if e.rec != nil && e.rec.OnCashflow != nil {
			e.rec.OnCashflow(today, float64(soldShares)*intent.Price)
		}
		return exec.OnSellApplied(stockID, today, soldShares, intent.Price, e.cash)
	}
	return nil
}

// loadStockSeries 從 DB 一次讀入所有追蹤股票的歷史資料並預先計算 20MA。
// 上線與回測都使用同一份 series — 引擎處理單日決策時,所有讀價皆走 series,而非額外 DB query,
// 因此兩種模式的「策略觀點」完全一致。
func loadStockSeries(appCtx *app_context.AppContext) (map[string]*stockSeries, error) {
	series := make(map[string]*stockSeries, len(appCtx.Cfg.TrackStocks))

	for _, stockID := range appCtx.Cfg.TrackStocks {
		rows, err := appCtx.Db.Query("SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;", stockID)
		if err != nil {
			return nil, fmt.Errorf("load %s history: %w", stockID, err)
		}

		dates := make([]time.Time, 0, 2048)
		prices := make([]float64, 0, 2048)
		for rows.Next() {
			var dateStr string
			var price float64
			if err := rows.Scan(&dateStr, &price); err != nil {
				rows.Close()
				return nil, err
			}
			t, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				t, err = time.Parse("2006-01-02 15:04:05", dateStr)
				if err != nil {
					continue
				}
			}
			dates = append(dates, t)
			prices = append(prices, price)
		}
		rows.Close()

		if len(dates) == 0 {
			appCtx.Log.Warn("無歷史資料 stockID=", stockID)
			continue
		}

		idx := make(map[string]int, len(dates))
		for i, d := range dates {
			idx[d.Format("2006-01-02")] = i
		}

		ma20 := make([]float64, len(dates))
		const window = 20
		sum := 0.0
		for i, p := range prices {
			sum += p
			if i >= window {
				sum -= prices[i-window]
			}
			if i >= window-1 {
				ma20[i] = sum / float64(window)
			} else {
				ma20[i] = math.NaN()
			}
		}

		series[stockID] = &stockSeries{
			dates:       dates,
			dateIndex:   idx,
			closePrices: prices,
			ma20:        ma20,
		}
	}

	return series, nil
}

// collectDateUnion 回傳所有股票日期的聯集,升冪排序。
func collectDateUnion(series map[string]*stockSeries) []time.Time {
	seen := make(map[string]struct{}, 2048)
	for _, s := range series {
		for _, d := range s.dates {
			seen[d.Format("2006-01-02")] = struct{}{}
		}
	}
	out := make([]time.Time, 0, len(seen))
	for k := range seen {
		t, err := time.Parse("2006-01-02", k)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
