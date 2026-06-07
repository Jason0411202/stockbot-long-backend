// internal/service/trading_service.go 負責線上交易模式的啟動、回放、每日 loop 與成交副作用。
package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

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
	cfg       *config.Config
	log       *logrus.Logger
}

// BotState 鍵值常數，對應跨重啟持久化的水位線與現金欄位。
const (
	stateKeyWatermark = "last_processed_date"
	stateKeyCash      = "current_cash"
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
//  4. Catch-up：靜默回放 [水位線+1, 最新日] 區間，寫入 DB 但不發 Discord 通知。
//  5. 進入每日 loop：每天 14:00 台灣時間抓取資料、執行當日決策並發送通知。
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

	// 使用靜默 executor 回放（寫入 DB 但不發 Discord 通知）。
	silent := &tradingExecutor{svc: s, ctx: ctx, notify: false}
	if err := s.engine.ProcessDates(catchupDates, series, silent); err != nil {
		return fmt.Errorf("ProcessDates: %w", err)
	}

	// 持久化回放後的水位線與現金。
	newWatermark := catchupDates[len(catchupDates)-1]
	if err := s.saveWatermark(ctx, newWatermark); err != nil {
		s.log.Warn("saveWatermark 失敗 (不致命):", err)
	}
	if err := s.saveCash(ctx, s.engine.Cash()); err != nil {
		s.log.Warn("saveCash 失敗 (不致命):", err)
	}
	stats := s.engine.Stats()
	s.log.Infof("catch-up 完成: cash=%.2f, buys=%d, sells=%d, skipped=%d",
		s.engine.Cash(), stats.TotalBuys, stats.TotalSells, stats.SkippedBuys)
	return nil
}

// runDailyLoop 是線上模式的主迴圈：每天台灣時間 14:00 執行一次當日決策，並發送 Discord 通知。
func (s *TradingService) runDailyLoop(ctx context.Context) error {
	taiwanTimeZone, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return fmt.Errorf("LoadLocation Asia/Taipei: %w", err)
	}
	noisy := &tradingExecutor{svc: s, ctx: ctx, notify: true}

	// 每分鐘檢查一次是否到達 14:00，到達時執行當日決策。
	for {
		now := time.Now().In(taiwanTimeZone)
		if now.Hour() == 14 && now.Minute() == 0 {
			s.log.Info("現在時間:", now)
			if err := s.runOneDay(ctx, noisy, now); err != nil {
				s.log.Error("runOneDay 錯誤:", err)
			}
		}
		time.Sleep(60 * time.Second)
	}
}

// runOneDay 抓取當日 TWSE 資料、重新載入序列、執行引擎單日決策，並持久化水位線與現金。
func (s *TradingService) runOneDay(ctx context.Context, exec trading.Executor, now time.Time) error {
	// 更新今日 TWSE 資料。
	if err := s.market.UpdateDatabase(ctx); err != nil {
		return fmt.Errorf("UpdateDatabase: %w", err)
	}
	series, err := s.loadSeries(ctx)
	if err != nil {
		return fmt.Errorf("loadSeries: %w", err)
	}
	// 執行引擎單日決策並持久化狀態。
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := s.engine.ProcessDay(today, series, exec); err != nil {
		return fmt.Errorf("ProcessDay: %w", err)
	}
	if err := s.saveWatermark(ctx, today); err != nil {
		s.log.Warn("saveWatermark 失敗 (不致命):", err)
	}
	if err := s.saveCash(ctx, s.engine.Cash()); err != nil {
		s.log.Warn("saveCash 失敗 (不致命):", err)
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

// OnBuyApplied 將引擎套用後的買進成交寫入 portfolio，並視設定發送通知。
func (e *tradingExecutor) OnBuyApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error {
	dateStr := day.Format(dateLayout)
	// 將買進成交寫入未實現帳本。
	if err := e.svc.portfolio.BuyShares(e.context(), stockID, dateStr, shares); err != nil {
		return fmt.Errorf("BuyShares: %w", err)
	}
	cost := float64(shares) * price
	e.svc.log.Infof("%s 買入: shares=%d, price=%.2f, cost=%.2f, cash=%.2f",
		stockID, shares, price, cost, cashAfter)
	// 通知模式下發送 Discord 買入嵌入訊息。
	if e.notify {
		if err := e.svc.notify.SendEmbed("🔴 買入通知",
			fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f\n剩餘現金: %.2f",
				stockID, shares, price, cost, cashAfter), 0xD50000); err != nil {
			e.svc.log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}

// OnSellApplied 將引擎套用後的賣出成交寫入 portfolio，並視設定發送通知。
func (e *tradingExecutor) OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error {
	dateStr := day.Format(dateLayout)
	// 將賣出成交從未實現帳本轉為已實現損益。
	if err := e.svc.portfolio.SellShares(e.context(), stockID, dateStr, shares); err != nil {
		return fmt.Errorf("SellShares: %w", err)
	}
	revenue := float64(shares) * price
	e.svc.log.Infof("%s 賣出: shares=%d, price=%.2f, revenue=%.2f, cash=%.2f",
		stockID, shares, price, revenue, cashAfter)
	// 通知模式下發送 Discord 賣出嵌入訊息。
	if e.notify {
		if err := e.svc.notify.SendEmbed("🟢 賣出通知",
			fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f\n剩餘現金: %.2f",
				stockID, shares, price, revenue, cashAfter), 0x00C853); err != nil {
			e.svc.log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}
