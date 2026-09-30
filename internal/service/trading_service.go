// internal/service/trading_service.go 負責線上交易模式的啟動、回放、每日 loop 與成交副作用。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"

	"github.com/Jason0411202/stockbot-long-backend/internal/metrics"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// TradingService 是線上交易模式的命令式外殼（imperative shell）。
// 它將純交易引擎、投資組合／市場資料服務，以及 repository／notifier port 組合在一起，
// 負責從 DB 載入價格序列、於啟動時還原引擎狀態、持久化水位線與現金，
// 並將成交事件路由至 portfolio service 與通知管道 (Discord / LINE)。
// 純決策邏輯保留在 *trading.Engine 中，TradingService 本身只處理 I/O 協調。
type TradingService struct {
	calendar    TradingCalendar
	needsReload bool
	engineDate  time.Time
	seeded      bool
	engine      *trading.Engine
	portfolio   *PortfolioService
	market      *MarketDataService
	series      SeriesLoader
	ledger      LedgerSeedStore
	state       StateStore
	equity      EquityStore
	notify      Notifier
	realtime    RealtimeFetcher
	cfg         *config.Config
	log         *logrus.Entry
}

// BotState 鍵值常數，對應跨重啟持久化的水位線、現金與累計注資欄位。
const (
	stateKeyWatermark        = "last_processed_date"
	stateKeyCash             = "current_cash"
	stateKeyTotalContributed = "total_contributed"
)

// dateLayout / datetimeLayout 是 seed 路徑需相容的兩種日期字串格式
// （DATE 欄位格式與舊版 DATETIME 欄位格式）。
const (
	dateLayout     = "2006-01-02"
	datetimeLayout = "2006-01-02 15:04:05"
)

// NewTradingService 建立並回傳一個已完成依賴注入的 TradingService。
func NewTradingService(
	engine *trading.Engine,
	portfolio *PortfolioService,
	market *MarketDataService,
	series SeriesLoader,
	ledger LedgerSeedStore,
	state StateStore,
	equity EquityStore,
	notify Notifier,
	realtime RealtimeFetcher,
	cfg *config.Config,
	log *logrus.Logger,
) *TradingService {
	return &TradingService{
		engine:    engine,
		portfolio: portfolio,
		market:    market,
		series:    series,
		ledger:    ledger,
		state:     state,
		equity:    equity,
		notify:    notify,
		realtime:  realtime,
		cfg:       cfg,
		log:       log.WithField("component", "trading"),
	}
}

// DailyCheck 是伺服器啟動後的進入點，委派給 RunOnline 執行線上模式的完整啟動流程。
func (s *TradingService) DailyCheck(ctx context.Context) error {
	s.log.Info("DailyCheck 開始執行")
	return s.RunOnline(ctx)
}

// RunOnline 啟動線上模式，依序執行以下步驟：
//  1. 抓取最新 TWSE 資料（失敗為非致命，沿用既有 DB）。
//  2. 從 DB 載入價格序列。
//  3. 從 DB（BotState + UnrealizedGainsLosses）還原引擎狀態。
//  4. Catch-up：靜默回放 [水位線+1, 最新日] 區間（開盤價基準,用 DB 歷史開盤價），寫入 DB 但不發 Discord 通知。
//  5. 進入每日 loop：每天台灣時間開盤時段 (09:10~09:30) 抓即時開盤價、即時決策並發送通知。
func (s *TradingService) RunOnline(ctx context.Context) error {
	if s.cfg.ScalingStrategy != "Baseline" {
		return fmt.Errorf("目前僅支援 Scaling_Strategy=Baseline, got %s", s.cfg.ScalingStrategy)
	}

	if s.cfg.DecisionPriceBasis != "open" {
		return fmt.Errorf("online trading requires decision_price_basis=open")
	}
	if s.calendar == nil {
		return fmt.Errorf("online trading requires TWSE calendar")
	}
	// 更新最新 TWSE 資料；失敗時記錄錯誤與資料源失敗指標,但繼續使用既有 DB 資料。
	if err := s.market.UpdateDatabase(ctx); err != nil {
		metrics.IncMarketDataError()
		s.log.WithError(err).Error("UpdateDatabase 錯誤 (不致命,沿用既有 DB)")
	}

	// 載入所有追蹤股票的價格序列。
	series, err := s.loadSeries(ctx)
	if err != nil {
		return fmt.Errorf("loadSeries: %w", err)
	}
	if len(series) == 0 {
		return fmt.Errorf("無任何股票歷史資料")
	}

	// 從 DB 還原引擎的現金、持倉、冷卻錨點、停利出場日與持倉峰值。
	if err := s.SeedFromDB(ctx, series); err != nil {
		return fmt.Errorf("SeedFromDB: %w", err)
	}

	if err := s.finalizeLatest(ctx, series); err != nil {
		s.log.WithError(err).Warn("close valuation refresh pending")
	}

	// 靜默回放未處理的歷史日期。
	if err := s.CatchUp(ctx, series); err != nil {
		s.log.WithError(err).Error("catch-up paused; retry after market data recovery")
	}

	return s.runDailyLoop(ctx)
}

// loadSeries 從 DB 載入每檔追蹤股票的歷史資料，並建構 trading.StockSeries map。
// 對沒有任何歷史資料的股票記錄警告後略過。
func (s *TradingService) loadSeries(ctx context.Context) (map[string]*trading.StockSeries, error) {
	series, err := LoadTradingSeries(ctx, s.series, s.cfg.TrackStocks)
	if err != nil {
		return nil, err
	}
	// 對缺少歷史資料的追蹤股票記錄警告。
	for _, stockID := range s.cfg.TrackStocks {
		if _, ok := series[stockID]; !ok {
			s.log.WithField("stock_id", stockID).Warn("無歷史資料")
		}
	}
	return series, nil
}

// SeedFromDB 從 DB 還原引擎的全部決策狀態:現金、持倉、各股最後買入日 (冷卻錨點)、最後賣出日 (移動停利
// 再進場暫停錨點),並依持倉與價格序列重建持倉峰值 (截至水位線),使重啟後的決策與從頭連續回放完全一致。
// 現金以 BotState 為準；無紀錄時退回 cfg.InitialCash（首次啟動）。
// lot 日期同時相容 DATE 與 DATETIME 兩種格式。
func (s *TradingService) SeedFromDB(ctx context.Context, series map[string]*trading.StockSeries) error {
	s.engine = trading.NewEngine(s.cfg)
	// 讀取持久化的現金值；無紀錄時使用設定的起始現金。
	cash, hasCash, err := s.loadCash(ctx)
	if err != nil {
		return fmt.Errorf("loadCash: %w", err)
	}
	if hasCash {
		s.engine.SeedCash(cash)
		s.log.WithField("cash", cash).Info("從 BotState 還原現金")
	} else {
		s.log.WithField("initial_cash", s.cfg.InitialCash).Info("BotState 無現金紀錄,使用 cfg.InitialCash")
	}

	// 從 UnrealizedGainsLosses 讀取所有持倉，還原引擎持倉狀態。
	lots, err := s.ledger.LoadAllUnrealized(ctx)
	if err != nil {
		return fmt.Errorf("LoadAllUnrealized: %w", err)
	}
	for _, r := range lots {
		date, perr := time.Parse(dateLayout, r.TransactionDate)
		if perr != nil {
			date, perr = time.Parse(datetimeLayout, r.TransactionDate)
			if perr != nil {
				s.log.WithError(perr).WithField("date", r.TransactionDate).Warn("跳過無法解析的 lot date")
				continue
			}
		}
		s.engine.SeedPosition(r.StockID, date, r.Shares, r.TransactionPrice)
	}
	s.log.WithField("lots", len(lots)).Info("從 UnrealizedGainsLosses 還原持倉")

	// 還原各股最後買入日（冷卻計算的時間錨點）。
	for _, stockID := range s.cfg.TrackStocks {
		raw, has, err := s.ledger.LastBuyDateRaw(ctx, stockID)
		if err != nil {
			return fmt.Errorf("LastBuyDateRaw(%s): %w", stockID, err)
		}
		if !has {
			continue
		}
		lb, perr := time.Parse(dateLayout, raw)
		if perr != nil {
			lb, perr = time.Parse(datetimeLayout, raw)
			if perr != nil {
				s.log.WithError(perr).WithFields(logrus.Fields{"date": raw, "stock_id": stockID}).Warn("跳過無法解析的 last-buy date")
				continue
			}
		}
		s.engine.SeedLastBuy(stockID, lb)
	}

	// 還原各股最後賣出日 (移動停利為唯一賣出路徑 → 即最後停利出場日,供再進場暫停閘)。
	for _, stockID := range s.cfg.TrackStocks {
		raw, has, err := s.ledger.LastSellDateRaw(ctx, stockID)
		if err != nil {
			return fmt.Errorf("LastSellDateRaw(%s): %w", stockID, err)
		}
		if !has {
			continue
		}
		ls, perr := parseLedgerDate(raw)
		if perr != nil {
			s.log.WithError(perr).WithFields(logrus.Fields{"date": raw, "stock_id": stockID}).Warn("跳過無法解析的 last-sell date")
			continue
		}
		s.engine.SeedLastTrailSell(stockID, ls)
	}

	// 依已還原的持倉與價格序列重建「持倉期間最高決策價」(截至水位線);水位線之後的日期由 catch-up 逐日更新。
	watermark, err := s.loadWatermark(ctx)
	if err != nil {
		return fmt.Errorf("loadWatermark: %w", err)
	}
	if !watermark.IsZero() {
		s.engine.RebuildPeakSinceHold(series, watermark)
	}
	for _, id := range s.cfg.TrackStocks {
		raw, ok, err := s.state.Get(ctx, "risk_"+id)
		if err != nil {
			return err
		}
		if ok {
			var risk trading.RiskState
			if err = json.Unmarshal([]byte(raw), &risk); err != nil {
				return err
			}
			s.engine.SeedRiskState(id, risk)
		}
	}
	// Publish restored metrics even when catch-up has no work.
	if !watermark.IsZero() {
		holding := s.engine.HoldingValueForDisplay(series, watermark)
		metrics.SetPortfolioSnapshot(s.engine.Cash(), holding, s.engine.Cash()+holding, s.engine.CostBasis())
		metrics.SetLastProcessedDate(watermark)
	}

	s.engineDate = watermark
	s.seeded = true
	return nil
}

// parseLedgerDate 解析帳本日期字串,同時相容 DATE 與 DATETIME 兩種格式。
func parseLedgerDate(raw string) (time.Time, error) {
	if d, err := time.Parse(dateLayout, raw); err == nil {
		return d, nil
	}
	return time.Parse(datetimeLayout, raw)
}

// CatchUp 以靜默 executor 回放 [水位線+1, 序列最新日] 區間的歷史決策，
// 寫入 DB 但不發送通知，回放完成後更新水位線與現金。
func (s *TradingService) CatchUp(ctx context.Context, series map[string]*trading.StockSeries) error {
	if err := s.reloadIfNeeded(ctx, series); err != nil {
		return err
	}
	watermark, err := s.loadWatermark(ctx)
	if err != nil {
		return fmt.Errorf("loadWatermark: %w", err)
	}

	// 收集所有股票的日期聯集並確認不為空。
	allDates := trading.CollectDateUnion(series)
	if len(allDates) == 0 {
		s.log.Warn("series 為空,跳過 catch-up")
		return nil
	}

	// 依水位線決定 catch-up 起始日期。
	var catchupDates []time.Time
	if watermark.IsZero() {
		// 首次啟動:從「所有追蹤股票都已發行」的那一天起 catch-up (不在某檔尚未上市的空窗期做決策)。
		startFloor := allDates[0]
		if ci, ok := backtest.CommonIssuanceStart(s.cfg, series); ok && ci.After(startFloor) {
			startFloor = ci
		}
		lo := sort.Search(len(allDates), func(i int) bool { return !allDates[i].Before(startFloor) })
		catchupDates = allDates[lo:]
		s.log.WithField("start", startFloor.Format(dateLayout)).Info("首次啟動,從 common issuance catch-up")
	} else {
		idx := sort.Search(len(allDates), func(i int) bool {
			return allDates[i].After(watermark)
		})
		catchupDates = allDates[idx:]
	}
	if len(catchupDates) == 0 {
		s.log.Info("無需要 catch-up 的日期,直接進入每日 loop")
		return nil
	}

	s.log.WithFields(logrus.Fields{
		"days": len(catchupDates),
		"from": catchupDates[0].Format(dateLayout),
		"to":   catchupDates[len(catchupDates)-1].Format(dateLayout),
	}).Info("catch-up 靜默回放中...")

	prev := watermark
	for _, d := range catchupDates {
		if err := s.validateDay(ctx, d, prev, series); err != nil {
			return err
		}
		if err := s.commitDay(ctx, d, series, nil, false); err != nil {
			return fmt.Errorf("commit %s: %w", d.Format(dateLayout), err)
		}
		prev = d
	}
	s.log.WithField("cash", s.engine.Cash()).Info("catch-up complete")
	return nil
}

// 開盤決策時段 (台灣時間):09:00 集合競價後開盤價即穩定,故於 [09:10, 09:30) 內擇機決策一次。
// 採時段而非單一分鐘,可容忍 loop 偶爾錯過某一分鐘;當日處理後即以水位線去重不再重跑。
const (
	openDecisionHour   = 9
	openWindowStartMin = 10 // 09:10 起 (集合競價後開盤價已穩定)
	openWindowEndMin   = 30 // 至 09:30 前
)

// inOpenDecisionWindow 回傳 now (台灣時間) 是否落在當日開盤決策時段 [09:10, 09:30)。
func inOpenDecisionWindow(now time.Time) bool {
	return now.Hour() == openDecisionHour && now.Minute() >= openWindowStartMin && now.Minute() < openWindowEndMin
}

// runDailyLoop 是線上模式的主迴圈：每天台灣時間開盤時段抓即時開盤價、即時決策並發送通知 (Discord / LINE)。
// 每分鐘檢查一次;當日尚未處理且落在開盤時段才嘗試決策,成功後以水位線去重避免重跑。
func (s *TradingService) runDailyLoop(ctx context.Context) error {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return err
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastRefresh time.Time
	for {
		now := time.Now().In(loc)
		today := taiwanDate(now)
		// Refresh after publication, and retry overnight every 15 minutes. The
		// next opening also finalizes the previous close before any new trades.
		if inOpenDecisionWindow(now) && !s.processedToday(ctx, today) {
			exec := &tradingExecutor{svc: s, ctx: ctx, notify: true}
			if err = s.runOneDayAtOpen(ctx, exec, today, false); err != nil {
				s.log.WithError(err).Error("opening decision paused")
			}
		} else if (now.Hour() >= 15 || now.Hour() < 9) && time.Since(lastRefresh) >= 15*time.Minute {
			lastRefresh = time.Now()
			if err = s.refreshCompletedDays(ctx, today, now.Hour() >= 15); err != nil {
				s.log.WithError(err).Error("daily close refresh paused")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *TradingService) refreshCompletedDays(ctx context.Context, today time.Time, includeToday bool) error {
	watermark, err := s.loadWatermark(ctx)
	if err != nil {
		return err
	}
	if err := s.market.UpdateDatabaseSince(ctx, watermark, today); err != nil {
		metrics.IncMarketDataError()
		return err
	}
	series, err := s.loadSeries(ctx)
	if err != nil {
		return err
	}
	if !includeToday {
		series = seriesBefore(series, today)
	}
	if err = s.reloadIfNeeded(ctx, series); err != nil {
		return err
	}
	if err = s.finalizeLatest(ctx, series); err != nil {
		return err
	}
	return s.CatchUp(ctx, series)
}

// taiwanDate 由台灣時間的 now 取出該日的 UTC 午夜日期 (與水位線 / 引擎日期格式一致)。
func taiwanDate(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// processedToday 回傳水位線是否已等於 today (今日已決策過,避免時段內重複下單)。
func (s *TradingService) processedToday(ctx context.Context, today time.Time) bool {
	wm, err := s.loadWatermark(ctx)
	if err != nil {
		return false
	}
	return !wm.IsZero() && wm.Format(dateLayout) >= today.Format(dateLayout)
}

// runOneDayAtOpen requires complete prior-session bars and all current opening
// prices, replays missed days in order, then commits the opening day atomically.
func (s *TradingService) runOneDayAtOpen(ctx context.Context, exec trading.Executor, today time.Time, _ bool) error {
	if s.calendar != nil {
		open, err := s.calendar.IsTradingDay(ctx, today)
		if err != nil {
			return err
		}
		if !open {
			return nil
		}
	}
	prev, err := s.loadWatermark(ctx)
	if err != nil {
		return err
	}
	if !prev.IsZero() && !today.After(prev) {
		return nil
	}
	if err = s.market.UpdateDatabaseSince(ctx, prev, today); err != nil {
		metrics.IncMarketDataError()
		return fmt.Errorf("UpdateDatabase: %w", err)
	}
	series, err := s.loadSeries(ctx)
	if err != nil {
		return err
	}
	// Exclude today's and future closes even if they are already present in DB.
	series = seriesBefore(series, today)
	if err = s.reloadIfNeeded(ctx, series); err != nil {
		return err
	}
	expected := today.AddDate(0, 0, -1)
	if s.calendar != nil {
		expected, err = s.calendar.PreviousTradingDay(ctx, today)
		if err != nil {
			return err
		}
	}
	var active []string
	for _, id := range s.cfg.TrackStocks {
		ss := series[id]
		if ss != nil && ss.Suspended(today) {
			continue
		}
		active = append(active, id)
		required := expected
		for ss != nil && ss.Suspended(required) {
			if s.calendar != nil {
				required, err = s.calendar.PreviousTradingDay(ctx, required)
				if err != nil {
					return err
				}
			} else {
				required = required.AddDate(0, 0, -1)
			}
		}
		if ss == nil || len(ss.Dates) == 0 || !ss.Dates[len(ss.Dates)-1].Equal(required) {
			return fmt.Errorf("stale or missing history %s: require %s", id, required.Format(dateLayout))
		}
	}
	opens := map[string]float64{}
	if len(active) > 0 {
		opens, err = s.realtime.FetchOpens(ctx, active)
		if err != nil {
			return err
		}
	}
	book := bookFor(s.series)
	normalized := make(map[string]float64, len(active))
	for _, id := range active {
		px := opens[id] * book.Factor(id, today.Format(dateLayout))
		if px <= 0 || math.IsNaN(px) || math.IsInf(px, 0) {
			s.log.WithField("stock_id", id).Info("awaiting complete opening prices; no watermark advance")
			return nil
		}
		last := series[id].ClosePrices[len(series[id].ClosePrices)-1]
		if px/last < 0.5 || px/last > 2 {
			return fmt.Errorf("inconsistent opening quote %s after official unit conversion; retry market data", id)
		}
		normalized[id] = px
	}
	opens = normalized
	if err = s.finalizeLatest(ctx, series); err != nil {
		return err
	}
	if err = s.CatchUp(ctx, series); err != nil {
		return err
	}
	noisy := false
	if te, ok := exec.(*tradingExecutor); ok {
		noisy = te.notify
	}
	return s.commitDay(ctx, today, series, opens, noisy)
}

func seriesBefore(src map[string]*trading.StockSeries, day time.Time) map[string]*trading.StockSeries {
	out := make(map[string]*trading.StockSeries, len(src))
	for id, ss := range src {
		n := sort.Search(len(ss.Dates), func(i int) bool { return !ss.Dates[i].Before(day) })
		var opens []float64
		if len(ss.OpenPrices) >= n {
			opens = ss.OpenPrices[:n]
		}
		out[id] = trading.NewStockSeries(ss.Dates[:n], opens, ss.ClosePrices[:n], nil, nil, nil)
		out[id].Suspensions = ss.Suspensions
	}
	return out
}

// loadWatermark 讀取 BotState 的 last_processed_date；
// 無紀錄時回傳零值時間，呼叫端將此視為「從最早資料開始 catch-up」。
func (s *TradingService) loadWatermark(ctx context.Context) (time.Time, error) {
	v, ok, err := s.state.Get(ctx, stateKeyWatermark)
	if err != nil || !ok {
		return time.Time{}, err
	}
	t, err := time.Parse(dateLayout, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse watermark %q: %w", v, err)
	}
	return t, nil
}

// saveWatermark 持久化最後處理日至 BotState。
func (s *TradingService) saveWatermark(ctx context.Context, t time.Time) error {
	return s.state.Set(ctx, stateKeyWatermark, t.Format(dateLayout))
}

// loadCash 讀取 BotState 的 current_cash；無紀錄時 bool 為 false，呼叫端退回 cfg.InitialCash。
func (s *TradingService) loadCash(ctx context.Context) (float64, bool, error) {
	v, ok, err := s.state.Get(ctx, stateKeyCash)
	if err != nil || !ok {
		return 0, false, err
	}
	c, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse cash %q: %w", v, err)
	}
	return c, true, nil
}

// saveCash 持久化引擎當前現金至 BotState。
func (s *TradingService) saveCash(ctx context.Context, cash float64) error {
	return s.state.Set(ctx, stateKeyCash, strconv.FormatFloat(cash, 'f', -1, 64))
}

// loadTotalContributed 讀取 BotState 的 total_contributed (除期初現金外、累計從外部注入的定額資金);
// 無紀錄時回傳 0 (尚未注資過,或關閉每月注資)。
func (s *TradingService) loadTotalContributed(ctx context.Context) (float64, error) {
	v, ok, err := s.state.Get(ctx, stateKeyTotalContributed)
	if err != nil || !ok {
		return 0, err
	}
	c, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("parse total_contributed %q: %w", v, err)
	}
	return c, nil
}

// saveTotalContributed 持久化累計注資總額至 BotState。
func (s *TradingService) saveTotalContributed(ctx context.Context, total float64) error {
	return s.state.Set(ctx, stateKeyTotalContributed, strconv.FormatFloat(total, 'f', -1, 64))
}

// addTotalContributed 把本次新增的注資額累加到 BotState 既有的 total_contributed 上後寫回。
func (s *TradingService) addTotalContributed(ctx context.Context, amount float64) error {
	cur, err := s.loadTotalContributed(ctx)
	if err != nil {
		return fmt.Errorf("loadTotalContributed: %w", err)
	}
	return s.saveTotalContributed(ctx, cur+amount)
}

// tradingExecutor 是線上模式的 trading.Executor 實作：
// 將引擎套用後的買進／賣出成交路由至 PortfolioService（寫入 UnrealizedGainsLosses / RealizedGainsLosses），
// 並在 notify=true 時經 Notifier 發送通知 (Discord embed / LINE 文字)。notify=false 用於 catch-up 靜默回放。
// orchestration 的 context 保存於 executor 上，使 portfolio 寫入能參與取消機制。
type tradingExecutor struct {
	svc      *TradingService
	ctx      context.Context
	notify   bool
	deferred bool
	events   []tradeEvent
}

// context 回傳 executor 要用於 portfolio 寫入的 context。
func (e *tradingExecutor) context() context.Context {
	if e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

// buyColor / sellColor 為買賣 embed 的左側色條 (紅買 / 綠賣)。
const (
	buyColor  = 0xD50000
	sellColor = 0x00C853
)

// OnBuyApplied 將引擎套用後的買進成交寫入 portfolio，記錄交易理由 log，並視設定發送通知。
func (e *tradingExecutor) OnBuyApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64, reason trading.TradeReason) error {
	dateStr := day.Format(dateLayout)
	// 將買進成交以引擎成交價 (開盤價) 寫入未實現帳本。
	if err := e.svc.portfolio.BuyShares(e.context(), stockID, dateStr, shares, price); err != nil {
		return fmt.Errorf("BuyShares: %w", err)
	}
	e.emit(tradeEvent{stockID: stockID, date: dateStr, reason: reason})
	return nil
}

// OnSellApplied 將引擎套用後的賣出成交寫入 portfolio，記錄交易理由 log，並視設定發送通知。
func (e *tradingExecutor) OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64, reason trading.TradeReason) error {
	dateStr := day.Format(dateLayout)
	// 將賣出成交以引擎成交價 (開盤價) 從未實現帳本轉為已實現損益。
	if err := e.svc.portfolio.SellShares(e.context(), stockID, dateStr, shares, price); err != nil {
		return fmt.Errorf("SellShares: %w", err)
	}
	e.emit(tradeEvent{stockID: stockID, date: dateStr, reason: reason})
	return nil
}

// logTrade 以結構化欄位 (logrus.Fields) 記錄一筆成交的方向、標的、價量與決策理由摘要。
// mode 欄位標示這筆是開盤即時決策 (live) 或重啟後的靜默回放補寫 (catchup)。
// 確保 log 完整保留「每筆交易為什麼成交」,供日後稽核與重現。
func (e *tradingExecutor) logTrade(action, stockID, dateStr string, reason trading.TradeReason) {
	// 依 executor 的通知旗標判別即時決策或回放補寫。
	mode := "catchup"
	if e.notify {
		mode = "live"
	}
	e.svc.log.WithFields(logrus.Fields{
		"mode":       mode,
		"stock_id":   stockID,
		"date":       dateStr,
		"trigger":    reason.Trigger,
		"regime":     reason.Regime,
		"shares":     reason.ShareQuantity(),
		"price":      fmt.Sprintf("%.2f", reason.Price),
		"amount":     fmt.Sprintf("%.2f", reason.Amount),
		"cash_after": fmt.Sprintf("%.2f", reason.CashAfter),
		"reason":     reason.Summary(),
	}).Info(action)
}

// buildTradeNotification 由交易理由組裝一則多欄位、附理由的成交通知 (Discord 渲染為 embed、LINE 渲染為多行文字)。
func buildTradeNotification(title string, color int, stockID, dateStr string, reason trading.TradeReason) discord.TradeNotification {
	return discord.TradeNotification{
		Title: fmt.Sprintf("%s — %s", title, stockID),
		Color: color,
		Fields: []discord.TradeField{
			{Name: "股票", Value: stockID, Inline: true},
			{Name: "市況", Value: regimeText(reason.Regime), Inline: true},
			{Name: "成交價(開盤)", Value: fmt.Sprintf("%.2f", reason.Price), Inline: true},
			{Name: "股數", Value: fmt.Sprintf("%s 股", strconv.FormatFloat(reason.ShareQuantity(), 'f', -1, 64)), Inline: true},
			{Name: "金額", Value: fmt.Sprintf("$%.0f", reason.Amount), Inline: true},
			{Name: "剩餘現金", Value: fmt.Sprintf("$%.0f", reason.CashAfter), Inline: true},
			{Name: "📋 交易理由", Value: reason.Summary(), Inline: false},
		},
		Footer: fmt.Sprintf("成交日 %s｜開盤價即時決策", dateStr),
	}
}

// regimeText 將 regime 代碼轉為帶 emoji 的繁中標籤。
func regimeText(regime string) string {
	if regime == "bull" {
		return "🐂 牛市"
	}
	return "🐻 熊市"
}
