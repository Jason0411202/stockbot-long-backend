// internal/service/trading_service.go 負責線上交易模式的啟動、回放、每日 loop 與成交副作用。
package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// TradingService 是線上交易模式的命令式外殼（imperative shell）。
// 它將純交易引擎、投資組合／市場資料服務，以及 repository／notifier port 組合在一起，
// 負責從 DB 載入價格序列、於啟動時還原引擎狀態、持久化水位線與現金，
// 並將成交事件路由至 portfolio service 與 Discord。
// 純決策邏輯保留在 *trading.Engine 中，TradingService 本身只處理 I/O 協調。
type TradingService struct {
	engine    *trading.Engine
	portfolio *PortfolioService
	market    *MarketDataService
	series    SeriesLoader
	ledger    LedgerSeedStore
	state     StateStore
	notify    Notifier
	realtime  RealtimeFetcher
	cfg       *config.Config
	log       *logrus.Logger
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
		notify:    notify,
		realtime:  realtime,
		cfg:       cfg,
		log:       log,
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

	// 更新最新 TWSE 資料；失敗時記錄錯誤但繼續使用既有 DB 資料。
	if err := s.market.UpdateDatabase(ctx); err != nil {
		s.log.Error("UpdateDatabase 錯誤 (不致命,沿用既有 DB):", err)
	}

	// 載入所有追蹤股票的價格序列。
	series, err := s.loadSeries(ctx)
	if err != nil {
		return fmt.Errorf("loadSeries: %w", err)
	}
	if len(series) == 0 {
		return fmt.Errorf("無任何股票歷史資料")
	}

	// 從 DB 還原引擎的現金、持倉與冷卻錨點。
	if err := s.SeedFromDB(ctx); err != nil {
		return fmt.Errorf("SeedFromDB: %w", err)
	}

	// 靜默回放未處理的歷史日期。
	if err := s.CatchUp(ctx, series); err != nil {
		return fmt.Errorf("catch-up: %w", err)
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
			s.log.Warn("無歷史資料 stockID=", stockID)
		}
	}
	return series, nil
}

// SeedFromDB 從 DB 還原引擎的現金、持倉與各股冷卻錨點。
// 現金以 BotState 為準；無紀錄時退回 cfg.InitialCash（首次啟動）。
// lot 日期同時相容 DATE 與 DATETIME 兩種格式。
func (s *TradingService) SeedFromDB(ctx context.Context) error {
	// 讀取持久化的現金值；無紀錄時使用設定的起始現金。
	cash, hasCash, err := s.loadCash(ctx)
	if err != nil {
		return fmt.Errorf("loadCash: %w", err)
	}
	if hasCash {
		s.engine.SeedCash(cash)
		s.log.Infof("從 BotState 還原現金: %.2f", cash)
	} else {
		s.log.Infof("BotState 無現金紀錄,使用 cfg.InitialCash=%.2f", s.cfg.InitialCash)
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
				s.log.Warnf("跳過無法解析的 lot date=%q: %v", r.TransactionDate, perr)
				continue
			}
		}
		s.engine.SeedPosition(r.StockID, date, r.Shares, r.TransactionPrice)
	}
	s.log.Infof("從 UnrealizedGainsLosses 還原 %d 筆持倉", len(lots))

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
				s.log.Warnf("跳過無法解析的 last-buy date=%q for %s: %v", raw, stockID, perr)
				continue
			}
		}
		s.engine.SeedLastBuy(stockID, lb)
	}
	return nil
}

// CatchUp 以靜默 executor 回放 [水位線+1, 序列最新日] 區間的歷史決策，
// 寫入 DB 但不發送 Discord 通知，回放完成後更新水位線與現金。
func (s *TradingService) CatchUp(ctx context.Context, series map[string]*trading.StockSeries) error {
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
		s.log.Infof("首次啟動,從 common issuance %s catch-up", startFloor.Format(dateLayout))
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

	s.log.Infof("catch-up %d 天 (%s ~ %s),靜默回放中...",
		len(catchupDates),
		catchupDates[0].Format(dateLayout),
		catchupDates[len(catchupDates)-1].Format(dateLayout))

	// 使用靜默 executor 回放（寫入 DB 但不發 Discord 通知）;每月第一個交易日先注入定額資金。
	// 注資排程以 backtest.ContributionDue 為單一事實來源,prev 起始為水位線 (首次啟動為零值,故起始日不注資),
	// 與回測 ContributionAmounts 對同一段交易日序列逐日完全一致,使線上首跑帳本忠實重現回測情境。
	silent := &tradingExecutor{svc: s, ctx: ctx, notify: false}
	prev := watermark
	runContrib := 0.0
	for _, d := range catchupDates {
		if c := backtest.ContributionDue(prev, d, s.cfg.MonthlyContribution); c > 0 {
			s.engine.AddCash(c)
			runContrib += c
		}
		if err := s.engine.ProcessDay(d, series, silent); err != nil {
			return fmt.Errorf("ProcessDay(%s): %w", d.Format(dateLayout), err)
		}
		prev = d
	}

	// 持久化回放後的水位線、現金與本次新增的累計注資。
	newWatermark := catchupDates[len(catchupDates)-1]
	if err := s.saveWatermark(ctx, newWatermark); err != nil {
		s.log.Warn("saveWatermark 失敗 (不致命):", err)
	}
	if err := s.saveCash(ctx, s.engine.Cash()); err != nil {
		s.log.Warn("saveCash 失敗 (不致命):", err)
	}
	if runContrib > 0 {
		if err := s.addTotalContributed(ctx, runContrib); err != nil {
			s.log.Warn("addTotalContributed 失敗 (不致命):", err)
		}
	}
	stats := s.engine.Stats()
	s.log.Infof("catch-up 完成: cash=%.2f, buys=%d, sells=%d, skipped=%d",
		s.engine.Cash(), stats.TotalBuys, stats.TotalSells, stats.SkippedBuys)
	return nil
}

// 開盤決策時段 (台灣時間):09:00 集合競價後開盤價即穩定,故於 [09:10, 09:30) 內擇機決策一次。
// 採時段而非單一分鐘,可容忍 loop 偶爾錯過某一分鐘;當日處理後即以水位線去重不再重跑。
const (
	openDecisionHour     = 9
	openWindowStartMin   = 10 // 09:10 起 (集合競價後開盤價已穩定)
	openWindowEndMin     = 30 // 至 09:30 前
	openWindowForceFromM = 29 // 09:29 起即使未全部就緒也以現有開盤價決策 (逾時 fallback)
)

// inOpenDecisionWindow 回傳 now (台灣時間) 是否落在當日開盤決策時段 [09:10, 09:30)。
func inOpenDecisionWindow(now time.Time) bool {
	return now.Hour() == openDecisionHour && now.Minute() >= openWindowStartMin && now.Minute() < openWindowEndMin
}

// runDailyLoop 是線上模式的主迴圈：每天台灣時間開盤時段抓即時開盤價、即時決策並發送 Discord 通知。
// 每分鐘檢查一次;當日尚未處理且落在開盤時段才嘗試決策,成功後以水位線去重避免重跑。
func (s *TradingService) runDailyLoop(ctx context.Context) error {
	taiwanTimeZone, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return fmt.Errorf("LoadLocation Asia/Taipei: %w", err)
	}
	noisy := &tradingExecutor{svc: s, ctx: ctx, notify: true}

	// 每分鐘檢查一次是否到達開盤決策時段;到達且今日未處理時執行當日開盤決策。
	for {
		now := time.Now().In(taiwanTimeZone)
		if inOpenDecisionWindow(now) {
			today := taiwanDate(now)
			if s.processedToday(ctx, today) {
				time.Sleep(60 * time.Second)
				continue
			}
			s.log.Info("開盤決策時段,現在時間:", now)
			// 09:29 起即使未全部就緒也以現有開盤價決策 (逾時 fallback,避免無限等待)。
			force := now.Minute() >= openWindowForceFromM
			if err := s.runOneDayAtOpen(ctx, noisy, today, force); err != nil {
				s.log.Error("runOneDayAtOpen 錯誤:", err)
			}
		}
		time.Sleep(60 * time.Second)
	}
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
	return !wm.IsZero() && wm.Format(dateLayout) == today.Format(dateLayout)
}

// runOneDayAtOpen 執行「當日開盤即時決策」:回補 DB (確保收盤到 T-1)、載入序列、抓即時開盤價,
// 全部就緒 (或 force 逾時) 才以 ProcessOpenDecision 決策,並持久化水位線與現金。
// 未就緒時不前進水位線 → 下一分鐘於時段內重試;逾時仍無任何開盤價則記錄錯誤、今日略過。
func (s *TradingService) runOneDayAtOpen(ctx context.Context, exec trading.Executor, today time.Time, force bool) error {
	// 回補 TWSE 月資料,確保 DB 收盤序列補到前一交易日 (T-1)。
	if err := s.market.UpdateDatabase(ctx); err != nil {
		return fmt.Errorf("UpdateDatabase: %w", err)
	}
	series, err := s.loadSeries(ctx)
	if err != nil {
		return fmt.Errorf("loadSeries: %w", err)
	}

	// 抓取當日各追蹤股的即時開盤價 (僅回傳已就緒者)。
	opens, err := s.realtime.FetchOpens(ctx, s.cfg.TrackStocks)
	if err != nil {
		return fmt.Errorf("FetchOpens: %w", err)
	}

	// 計算「有歷史序列、應有開盤價」的股票數;全部就緒或逾時 force 才決策。
	needed := 0
	for _, id := range s.cfg.TrackStocks {
		if _, ok := series[id]; ok {
			needed++
		}
	}
	if len(opens) < needed && !force {
		s.log.Infof("開盤價尚未全部就緒 (%d/%d),時段內稍後重試", len(opens), needed)
		return nil // 不前進水位線,下一分鐘再試
	}
	// 開盤價完全未就緒:今日不前進水位線、不注資 → 下次成功處理時 prev 仍停在上月,注資自然遞延補上。
	// 切勿在此路徑前進水位線,否則會跳過該月注資。
	if len(opens) == 0 {
		s.log.Error("逾時仍無任何即時開盤價,今日略過開盤決策 (留待下次重啟由 catch-up 以 DB 開盤價回放)")
		return nil
	}
	if len(opens) < needed {
		s.log.Warnf("逾時僅 %d/%d 檔開盤價就緒,以現有開盤價決策 (缺漏股今日不交易)", len(opens), needed)
	}

	// 每月第一個交易日 (相對前一個已處理交易日跨月) 先注入定額資金,注資後當日即可動用;
	// 與回測 / catch-up 共用 backtest.ContributionDue 排程。水位線去重保證同一天只會注資一次。
	// loadWatermark 失敗時 prev 為零值 → 本日不注資 (遞延至下次),記錄警告以利察覺。
	prev, err := s.loadWatermark(ctx)
	if err != nil {
		s.log.Warn("loadWatermark (注資判定) 失敗 (不致命,本日不注資):", err)
	}
	contrib := backtest.ContributionDue(prev, today, s.cfg.MonthlyContribution)
	if contrib > 0 {
		s.engine.AddCash(contrib)
	}

	// 以即時開盤價執行當日決策並持久化狀態。
	if err := s.engine.ProcessOpenDecision(today, opens, series, exec); err != nil {
		return fmt.Errorf("ProcessOpenDecision: %w", err)
	}
	if err := s.saveWatermark(ctx, today); err != nil {
		s.log.Warn("saveWatermark 失敗 (不致命):", err)
	}
	if err := s.saveCash(ctx, s.engine.Cash()); err != nil {
		s.log.Warn("saveCash 失敗 (不致命):", err)
	}
	if contrib > 0 {
		if err := s.addTotalContributed(ctx, contrib); err != nil {
			s.log.Warn("addTotalContributed 失敗 (不致命):", err)
		}
	}
	return nil
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
// 並在 notify=true 時發送 Discord 嵌入訊息。notify=false 用於 catch-up 靜默回放。
// orchestration 的 context 保存於 executor 上，使 portfolio 寫入能參與取消機制。
type tradingExecutor struct {
	svc    *TradingService
	ctx    context.Context
	notify bool
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
	// 以結構化欄位記錄本筆買入及其決策理由 (每筆交易理由皆進 log)。
	e.logTrade("買入成交", stockID, dateStr, reason)
	// 通知模式下發送美化的 Discord 買入嵌入訊息 (附交易理由);失敗僅記錄,不影響成交。
	if e.notify {
		if err := e.svc.notify.SendTradeEmbed(buildTradeNotification("🟥 買入成交", buyColor, stockID, dateStr, reason)); err != nil {
			e.svc.log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}

// OnSellApplied 將引擎套用後的賣出成交寫入 portfolio，記錄交易理由 log，並視設定發送通知。
func (e *tradingExecutor) OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64, reason trading.TradeReason) error {
	dateStr := day.Format(dateLayout)
	// 將賣出成交以引擎成交價 (開盤價) 從未實現帳本轉為已實現損益。
	if err := e.svc.portfolio.SellShares(e.context(), stockID, dateStr, shares, price); err != nil {
		return fmt.Errorf("SellShares: %w", err)
	}
	// 以結構化欄位記錄本筆賣出及其決策理由 (每筆交易理由皆進 log)。
	e.logTrade("賣出成交", stockID, dateStr, reason)
	// 通知模式下發送美化的 Discord 賣出嵌入訊息 (附交易理由);失敗僅記錄,不影響成交。
	if e.notify {
		if err := e.svc.notify.SendTradeEmbed(buildTradeNotification("🟩 賣出成交", sellColor, stockID, dateStr, reason)); err != nil {
			e.svc.log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}

// logTrade 以結構化欄位 (logrus.Fields) 記錄一筆成交的方向、標的、價量與決策理由摘要。
// 確保 log 完整保留「每筆交易為什麼成交」,供日後稽核與重現。
func (e *tradingExecutor) logTrade(action, stockID, dateStr string, reason trading.TradeReason) {
	e.svc.log.WithFields(logrus.Fields{
		"stock_id":   stockID,
		"date":       dateStr,
		"trigger":    reason.Trigger,
		"regime":     reason.Regime,
		"shares":     reason.Shares,
		"price":      fmt.Sprintf("%.2f", reason.Price),
		"amount":     fmt.Sprintf("%.2f", reason.Amount),
		"cash_after": fmt.Sprintf("%.2f", reason.CashAfter),
		"reason":     reason.Summary(),
	}).Info(action)
}

// buildTradeNotification 由交易理由組裝一則多欄位、附理由的 Discord 成交通知。
func buildTradeNotification(title string, color int, stockID, dateStr string, reason trading.TradeReason) discord.TradeNotification {
	return discord.TradeNotification{
		Title: fmt.Sprintf("%s — %s", title, stockID),
		Color: color,
		Fields: []discord.TradeField{
			{Name: "股票", Value: stockID, Inline: true},
			{Name: "市況", Value: regimeText(reason.Regime), Inline: true},
			{Name: "成交價(開盤)", Value: fmt.Sprintf("%.2f", reason.Price), Inline: true},
			{Name: "股數", Value: fmt.Sprintf("%d 股", reason.Shares), Inline: true},
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
