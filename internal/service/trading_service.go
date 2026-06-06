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

// TradingService is the online-trading orchestration (the imperative shell). It
// ports the online logic that previously lived in the kernals shim
// (DailyCheck / runOnlineMode / seedEngineFromDB / runCatchUp / runDailyLoop /
// runOneDay / dbExecutor) onto the layered dependencies: the pure trading engine,
// the portfolio/market services, and the repository/notifier ports.
//
// The pure decision logic stays in *trading.Engine; this shell only loads price
// series from the DB, restores engine state at startup, persists the
// watermark/cash, and routes applied buys/sells to the portfolio service and
// Discord.
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

// BotState keys persisted across restarts (mirror sqls.go Load/SaveWatermark and
// Load/SaveCash).
const (
	stateKeyWatermark = "last_processed_date"
	stateKeyCash      = "current_cash"
)

// dateLayout / datetimeLayout are the two stored date formats the seed path must
// tolerate (DATE columns vs legacy DATETIME), preserved from seedEngineFromDB.
const (
	dateLayout     = "2006-01-02"
	datetimeLayout = "2006-01-02 15:04:05"
)

// NewTradingService wires a TradingService to its engine, services, ports,
// config and logger.
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

// DailyCheck is the entrypoint after server boot: it enters online mode (startup
// catch-up replay of historical trades, then the daily 14:00 decision loop).
//
// The BackTestingMonths backtest branch that the old kernals.DailyCheck carried
// was already removed in phase 7, so DailyCheck simply delegates to RunOnline.
func (s *TradingService) DailyCheck(ctx context.Context) error {
	s.log.Info("DailyCheck 開始執行")
	return s.RunOnline(ctx)
}

// RunOnline starts online mode (ports runOnlineMode):
//  1. fetch latest TWSE data (a failure is non-fatal — keep using the existing DB).
//  2. load the price series.
//  3. restore engine state from the DB (BotState + UnrealizedGainsLosses).
//  4. catch-up: replay [watermark+1, latest] through the engine, writing the DB
//     but NOT sending per-trade Discord embeds.
//  5. enter the daily loop: every day at 14:00 Taipei, fetch + run one day + notify.
//
// The only semantic difference between online and backtest is whether step 5 runs
// after step 4.
func (s *TradingService) RunOnline(ctx context.Context) error {
	if s.cfg.ScalingStrategy != "Baseline" {
		return fmt.Errorf("目前僅支援 Scaling_Strategy=Baseline, got %s", s.cfg.ScalingStrategy)
	}

	if err := s.market.UpdateDatabase(ctx); err != nil {
		s.log.Error("UpdateDatabase 錯誤 (不致命,沿用既有 DB):", err)
	}

	series, err := s.loadSeries(ctx)
	if err != nil {
		return fmt.Errorf("loadSeries: %w", err)
	}
	if len(series) == 0 {
		return fmt.Errorf("無任何股票歷史資料")
	}

	if err := s.SeedFromDB(ctx); err != nil {
		return fmt.Errorf("SeedFromDB: %w", err)
	}

	if err := s.CatchUp(ctx, series); err != nil {
		return fmt.Errorf("catch-up: %w", err)
	}

	return s.runDailyLoop(ctx)
}

// loadSeries loads every tracked stock's history from the DB and builds a
// trading.StockSeries per stock. It delegates the DB-rows→StockSeries
// construction to the shared LoadTradingSeries helper (no behavior change), then
// warns about any tracked stock that produced no series (preserving the OLD
// loadStockSeries logging).
func (s *TradingService) loadSeries(ctx context.Context) (map[string]*trading.StockSeries, error) {
	series, err := LoadTradingSeries(ctx, s.series, s.cfg.TrackStocks)
	if err != nil {
		return nil, err
	}
	for _, stockID := range s.cfg.TrackStocks {
		if _, ok := series[stockID]; !ok {
			s.log.Warn("無歷史資料 stockID=", stockID)
		}
	}
	return series, nil
}

// SeedFromDB restores the engine's cash, positions and per-stock cooldown anchor
// from the DB (ports seedEngineFromDB). Cash falls back to cfg.InitialCash when
// BotState has no record (first start). Lot dates tolerate both DATE and DATETIME
// layouts.
func (s *TradingService) SeedFromDB(ctx context.Context) error {
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

// CatchUp replays [watermark+1, latest series date] through the engine (ports
// runCatchUp). It uses a SILENT executor (writes DB but no per-trade Discord, to
// avoid flooding notifications on historical replay), then persists the new
// watermark and cash.
func (s *TradingService) CatchUp(ctx context.Context, series map[string]*trading.StockSeries) error {
	watermark, err := s.loadWatermark(ctx)
	if err != nil {
		return fmt.Errorf("loadWatermark: %w", err)
	}

	allDates := trading.CollectDateUnion(series)
	if len(allDates) == 0 {
		s.log.Warn("series 為空,跳過 catch-up")
		return nil
	}

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

	silent := &tradingExecutor{svc: s, ctx: ctx, notify: false}
	if err := s.engine.ProcessDates(catchupDates, series, silent); err != nil {
		return fmt.Errorf("ProcessDates: %w", err)
	}

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

// runDailyLoop is the body of online mode (ports runDailyLoop): every day at
// 14:00 Taipei it triggers one day's decisions. The flow is identical to
// catch-up except it sends per-trade Discord embeds.
func (s *TradingService) runDailyLoop(ctx context.Context) error {
	taiwanTimeZone, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return fmt.Errorf("LoadLocation Asia/Taipei: %w", err)
	}
	noisy := &tradingExecutor{svc: s, ctx: ctx, notify: true}

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

// runOneDay fetches today's TWSE data, reloads the series, runs the engine for
// one day, then persists the watermark/cash (ports runOneDay).
func (s *TradingService) runOneDay(ctx context.Context, exec trading.Executor, now time.Time) error {
	if err := s.market.UpdateDatabase(ctx); err != nil {
		return fmt.Errorf("UpdateDatabase: %w", err)
	}
	series, err := s.loadSeries(ctx)
	if err != nil {
		return fmt.Errorf("loadSeries: %w", err)
	}
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

// loadWatermark reads BotState.last_processed_date. A missing record yields the
// zero time, which the caller treats as "catch-up from the earliest data"
// (ports sqls.LoadWatermark).
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

// saveWatermark persists the last processed day (ports sqls.SaveWatermark).
func (s *TradingService) saveWatermark(ctx context.Context, t time.Time) error {
	return s.state.Set(ctx, stateKeyWatermark, t.Format(dateLayout))
}

// loadCash reads BotState.current_cash. The bool is false when no record exists,
// so the caller falls back to cfg.InitialCash (ports sqls.LoadCash).
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

// saveCash persists the engine's current cash (ports sqls.SaveCash).
func (s *TradingService) saveCash(ctx context.Context, cash float64) error {
	return s.state.Set(ctx, stateKeyCash, strconv.FormatFloat(cash, 'f', -1, 64))
}

// tradingExecutor is the online-mode trading.Executor implementation (ports the
// kernals dbExecutor): it routes applied buys/sells to the PortfolioService
// (which writes UnrealizedGainsLosses / RealizedGainsLosses) and, when notify is
// true, sends a Discord embed. notify=false is used during catch-up replay.
//
// The old dbExecutor had no context; here the orchestration's context is carried
// on the executor so the portfolio writes participate in cancellation.
type tradingExecutor struct {
	svc    *TradingService
	ctx    context.Context
	notify bool
}

func (e *tradingExecutor) context() context.Context {
	if e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

func (e *tradingExecutor) OnBuyApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error {
	dateStr := day.Format(dateLayout)
	if err := e.svc.portfolio.BuyShares(e.context(), stockID, dateStr, shares); err != nil {
		return fmt.Errorf("BuyShares: %w", err)
	}
	cost := float64(shares) * price
	e.svc.log.Infof("%s 買入: shares=%d, price=%.2f, cost=%.2f, cash=%.2f",
		stockID, shares, price, cost, cashAfter)
	if e.notify {
		if err := e.svc.notify.SendEmbed("🔴 買入通知",
			fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f\n剩餘現金: %.2f",
				stockID, shares, price, cost, cashAfter), 0xD50000); err != nil {
			e.svc.log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}

func (e *tradingExecutor) OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error {
	dateStr := day.Format(dateLayout)
	if err := e.svc.portfolio.SellShares(e.context(), stockID, dateStr, shares); err != nil {
		return fmt.Errorf("SellShares: %w", err)
	}
	revenue := float64(shares) * price
	e.svc.log.Infof("%s 賣出: shares=%d, price=%.2f, revenue=%.2f, cash=%.2f",
		stockID, shares, price, revenue, cashAfter)
	if e.notify {
		if err := e.svc.notify.SendEmbed("🟢 賣出通知",
			fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f\n剩餘現金: %.2f",
				stockID, shares, price, revenue, cashAfter), 0x00C853); err != nil {
			e.svc.log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}
