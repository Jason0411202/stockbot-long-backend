package kernals

import (
	"fmt"
	"github.com/Jason0411202/stockbot-long-backend/app_context"
	"github.com/Jason0411202/stockbot-long-backend/discord"
	"github.com/Jason0411202/stockbot-long-backend/sqls"
	"sort"
	"time"
)

// DailyCheck 是 main 啟動後的進入點。
// 行為:
//   - back_testing_months > 0:跑一次回測,結束並回報結果 (不進入每日 loop)。
//   - 否則:進入上線模式 — 啟動時 catch-up 回放歷史交易,接著每日 14:00 觸發決策。
func DailyCheck(appCtx *app_context.AppContext) {
	appCtx.Log.Info("DailyCheck 開始執行")

	if appCtx.Cfg.BackTestingMonths > 0 {
		runBacktestMode(appCtx)
		return
	}
	if err := runOnlineMode(appCtx); err != nil {
		appCtx.Log.Fatal("上線模式啟動失敗:", err)
	}
}

// runBacktestMode 跑一次 in-memory 回測並把結果發到 Discord。
// 與上線唯一差別:沒有副作用 (noopExecutor) 且跑完即停。
func runBacktestMode(appCtx *app_context.AppContext) {
	appCtx.Log.Info("進入回測模式 (in-memory), months=", appCtx.Cfg.BackTestingMonths)
	if err := sqls.UpdataDatebase(appCtx); err != nil {
		appCtx.Log.Error("回測模式出錯,UpdataDatebase 錯誤:", err)
		return
	}
	result, err := RunBacktest(appCtx)
	if err != nil {
		appCtx.Log.Error("回測執行錯誤:", err)
		return
	}
	appCtx.Log.Infof("=== 回測結果 === 起始現金: %.2f, 每月注資合計: %.2f, 期末現金: %.2f, 期末持股市值: %.2f, 合計: %.2f",
		result.InitialCash, result.TotalContributed, result.FinalCash, result.FinalHoldingValue, result.FinalTotal)
	_ = discord.SendEmbedDiscordMessage(appCtx, "📊 回測結果",
		fmt.Sprintf("起始現金: %.2f\n每月注資合計: %.2f\n期末現金: %.2f\n期末持股市值: %.2f\n合計: %.2f",
			result.InitialCash, result.TotalContributed, result.FinalCash, result.FinalHoldingValue, result.FinalTotal),
		0x2196F3)
}

// runOnlineMode 啟動上線模式。
// 步驟:
//  1. 抓 TWSE 最新資料 (失敗不致命,沿用既有 DB)。
//  2. 載入價格 series。
//  3. 從 DB (BotState + UnrealizedGainsLosses + RealizedGainsLosses) 還原 engine 狀態。
//  4. catch-up:把 [watermark+1, 最新日] 透過引擎跑一次,寫入 DB 但不發 Discord per-trade。
//  5. 進入每日 loop:每天 14:00 Taipei 抓資料 + 跑當日決策 + 發 Discord。
//
// 上線與回測的唯一語意差別,就只在「step 4 之後是否進入 step 5」這一點。
func runOnlineMode(appCtx *app_context.AppContext) error {
	if appCtx.Cfg.ScalingStrategy != "Baseline" {
		return fmt.Errorf("目前僅支援 Scaling_Strategy=Baseline, got %s", appCtx.Cfg.ScalingStrategy)
	}

	if err := sqls.UpdataDatebase(appCtx); err != nil {
		appCtx.Log.Error("UpdataDatebase 錯誤 (不致命,沿用既有 DB):", err)
	}

	series, err := loadStockSeries(appCtx)
	if err != nil {
		return fmt.Errorf("loadStockSeries: %w", err)
	}
	if len(series) == 0 {
		return fmt.Errorf("無任何股票歷史資料")
	}

	engine := NewEngine(appCtx.Cfg)
	if err := seedEngineFromDB(appCtx, engine); err != nil {
		return fmt.Errorf("seedEngineFromDB: %w", err)
	}

	if err := runCatchUp(appCtx, engine, series); err != nil {
		return fmt.Errorf("catch-up: %w", err)
	}

	return runDailyLoop(appCtx, engine)
}

// seedEngineFromDB 從 DB 還原 engine 的現金、持倉、冷卻基準。
// 順序很重要:cash 優先用 BotState (持久化過的真實餘額);如果沒有 (第一次啟動),
// 則維持 NewEngine 預設的 cfg.InitialCash。
func seedEngineFromDB(appCtx *app_context.AppContext, engine *Engine) error {
	cash, hasCash, err := sqls.LoadCash(appCtx)
	if err != nil {
		return fmt.Errorf("LoadCash: %w", err)
	}
	if hasCash {
		engine.SeedCash(cash)
		appCtx.Log.Infof("從 BotState 還原現金: %.2f", cash)
	} else {
		appCtx.Log.Infof("BotState 無現金紀錄,使用 cfg.InitialCash=%.2f", appCtx.Cfg.InitialCash)
	}

	lots, err := sqls.LoadAllUnrealizedLots(appCtx)
	if err != nil {
		return fmt.Errorf("LoadAllUnrealizedLots: %w", err)
	}
	for _, r := range lots {
		date, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			date, err = time.Parse("2006-01-02 15:04:05", r.Date)
			if err != nil {
				appCtx.Log.Warnf("跳過無法解析的 lot date=%q: %v", r.Date, err)
				continue
			}
		}
		engine.SeedPosition(r.StockID, date, r.Shares, r.Price)
	}
	appCtx.Log.Infof("從 UnrealizedGainsLosses 還原 %d 筆持倉", len(lots))

	for _, stockID := range appCtx.Cfg.TrackStocks {
		lb, has, err := sqls.LoadLastBuyDate(appCtx, stockID)
		if err != nil {
			return fmt.Errorf("LoadLastBuyDate(%s): %w", stockID, err)
		}
		if has {
			engine.SeedLastBuy(stockID, lb)
		}
	}
	return nil
}

// runCatchUp 把 [watermark+1, latest series date] 透過引擎跑一遍。
// 使用 silent executor:寫 DB 但不發 Discord (避免歷史回放灌爆通知)。
func runCatchUp(appCtx *app_context.AppContext, engine *Engine, series map[string]*stockSeries) error {
	watermark, err := sqls.LoadWatermark(appCtx)
	if err != nil {
		return fmt.Errorf("LoadWatermark: %w", err)
	}

	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		appCtx.Log.Warn("series 為空,跳過 catch-up")
		return nil
	}

	var catchupDates []time.Time
	if watermark.IsZero() {
		// 首次啟動:從「所有追蹤股票都已發行」的那一天起 catch-up (不在某檔尚未上市的空窗期做決策)。
		startFloor := allDates[0]
		if ci, ok := commonIssuanceStart(appCtx.Cfg, series); ok && ci.After(startFloor) {
			startFloor = ci
		}
		lo := sort.Search(len(allDates), func(i int) bool { return !allDates[i].Before(startFloor) })
		catchupDates = allDates[lo:]
		appCtx.Log.Infof("首次啟動,從 common issuance %s catch-up", startFloor.Format("2006-01-02"))
	} else {
		idx := sort.Search(len(allDates), func(i int) bool {
			return allDates[i].After(watermark)
		})
		catchupDates = allDates[idx:]
	}
	if len(catchupDates) == 0 {
		appCtx.Log.Info("無需要 catch-up 的日期,直接進入每日 loop")
		return nil
	}

	appCtx.Log.Infof("catch-up %d 天 (%s ~ %s),靜默回放中...",
		len(catchupDates),
		catchupDates[0].Format("2006-01-02"),
		catchupDates[len(catchupDates)-1].Format("2006-01-02"))

	silent := &dbExecutor{appCtx: appCtx, notify: false}
	if err := engine.ProcessDates(catchupDates, series, silent); err != nil {
		return fmt.Errorf("ProcessDates: %w", err)
	}

	newWatermark := catchupDates[len(catchupDates)-1]
	if err := sqls.SaveWatermark(appCtx, newWatermark); err != nil {
		appCtx.Log.Warn("SaveWatermark 失敗 (不致命):", err)
	}
	if err := sqls.SaveCash(appCtx, engine.Cash()); err != nil {
		appCtx.Log.Warn("SaveCash 失敗 (不致命):", err)
	}
	stats := engine.Stats()
	appCtx.Log.Infof("catch-up 完成: cash=%.2f, buys=%d, sells=%d, skipped=%d",
		engine.Cash(), stats.TotalBuys, stats.TotalSells, stats.SkippedBuys)
	return nil
}

// runDailyLoop 是上線模式的本體:每天 Taipei 時間 14:00 觸發一次當日決策。
// 處理流程與 catch-up 相同,只差在會發 Discord per-trade。
func runDailyLoop(appCtx *app_context.AppContext, engine *Engine) error {
	taiwanTimeZone, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return fmt.Errorf("LoadLocation Asia/Taipei: %w", err)
	}
	noisy := &dbExecutor{appCtx: appCtx, notify: true}

	for {
		now := time.Now().In(taiwanTimeZone)
		if now.Hour() == 14 && now.Minute() == 0 {
			appCtx.Log.Info("現在時間:", now)
			if err := runOneDay(appCtx, engine, noisy, now); err != nil {
				appCtx.Log.Error("runOneDay 錯誤:", err)
			}
		}
		time.Sleep(60 * time.Second)
	}
}

// runOneDay 抓今日 TWSE → reload series → 引擎處理一日 → 持久化 watermark/cash。
func runOneDay(appCtx *app_context.AppContext, engine *Engine, exec Executor, now time.Time) error {
	if err := sqls.UpdataDatebase(appCtx); err != nil {
		return fmt.Errorf("UpdataDatebase: %w", err)
	}
	series, err := loadStockSeries(appCtx)
	if err != nil {
		return fmt.Errorf("loadStockSeries: %w", err)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := engine.ProcessDay(today, series, exec); err != nil {
		return fmt.Errorf("ProcessDay: %w", err)
	}
	if err := sqls.SaveWatermark(appCtx, today); err != nil {
		appCtx.Log.Warn("SaveWatermark 失敗 (不致命):", err)
	}
	if err := sqls.SaveCash(appCtx, engine.Cash()); err != nil {
		appCtx.Log.Warn("SaveCash 失敗 (不致命):", err)
	}
	return nil
}

// dbExecutor 是上線模式的 Executor 實作:寫入 UnrealizedGainsLosses /
// RealizedGainsLosses,並可選擇是否發 Discord (notify=false 用於 catch-up 回放)。
type dbExecutor struct {
	appCtx *app_context.AppContext
	notify bool
}

func (e *dbExecutor) OnBuyApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error {
	dateStr := day.Format("2006-01-02")
	if err := sqls.SQLBuyStock(e.appCtx, stockID, dateStr, shares); err != nil {
		return fmt.Errorf("SQLBuyStock: %w", err)
	}
	cost := float64(shares) * price
	e.appCtx.Log.Infof("%s 買入: shares=%d, price=%.2f, cost=%.2f, cash=%.2f",
		stockID, shares, price, cost, cashAfter)
	if e.notify {
		if err := discord.SendEmbedDiscordMessage(e.appCtx, "🔴 買入通知",
			fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f\n剩餘現金: %.2f",
				stockID, shares, price, cost, cashAfter), 0xD50000); err != nil {
			e.appCtx.Log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}

func (e *dbExecutor) OnSellApplied(stockID string, day time.Time, shares int, price float64, cashAfter float64) error {
	dateStr := day.Format("2006-01-02")
	if err := sqls.SQLSellStock(e.appCtx, stockID, dateStr, shares); err != nil {
		return fmt.Errorf("SQLSellStock: %w", err)
	}
	revenue := float64(shares) * price
	e.appCtx.Log.Infof("%s 賣出: shares=%d, price=%.2f, revenue=%.2f, cash=%.2f",
		stockID, shares, price, revenue, cashAfter)
	if e.notify {
		if err := discord.SendEmbedDiscordMessage(e.appCtx, "🟢 賣出通知",
			fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f\n剩餘現金: %.2f",
				stockID, shares, price, revenue, cashAfter), 0x00C853); err != nil {
			e.appCtx.Log.Error("發送 Discord 訊息失敗:", err)
		}
	}
	return nil
}
