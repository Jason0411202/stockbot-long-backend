// internal/service/strategy_history_backfill.go 以一致帳本及歷史行情自動補齊缺少的決策圖表。
package service

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// historyReadSnapshot 在不鎖定交易寫入者的情況下讀取一致資料。
type historyReadSnapshot interface {
	WithReadSnapshot(context.Context, func(context.Context) error) error
}

// frozenHistorySeries 固定一次回補採用的正規化行情及單位簿。
type frozenHistorySeries struct {
	rows map[string][]entity.StockHistory
	book marketunits.Book
}

// LoadSeries 回傳同次回補已讀取的正規化行情。
func (f *frozenHistorySeries) LoadSeries(context.Context, []string) (map[string][]entity.StockHistory, error) {
	return f.rows, nil
}

// UnitBook 回傳同次回補凍結的公司行動單位簿。
func (f *frozenHistorySeries) UnitBook() marketunits.Book { return f.book }

// historyInputs 保存短暫唯讀交易內取得的帳本、權益及價格輸入。
type historyInputs struct {
	watermark string
	lots      []entity.UnrealizedGainsLoss
	realized  []entity.RealizedGainsLoss
	equity    []entity.EquitySnapshot
	series    *frozenHistorySeries
}

// historicalLot 保存同一買單尚未出售或某次已出售的股數片段。
type historicalLot struct {
	stock     string
	buy, sell time.Time
	shares    int
	price     float64
}

// archiveLegacyMonitor 保留尚存在於舊單筆欄位的原始快照及原有條件。
func archiveLegacyMonitor(ctx context.Context, state StateStore, history StrategyHistoryStore) error {
	legacy, err := NewStrategyMonitorService(state).legacyMonitor(ctx)
	if err != nil {
		return err
	}
	if legacy.DecisionDate == "" {
		return nil
	}
	// 舊快照沒有版本證據，保留未知而不套用今天的策略版本。
	if legacy.Source == "" {
		legacy.Source = "original"
	}
	if legacy.StrategyVersion == "" {
		legacy.StrategyVersion = "legacy-unknown"
	}
	if legacy.Warnings == nil {
		legacy.Warnings = []string{"此原始快照未保存當時的程式版本及完整策略設定。"}
	}
	record, err := historyRecord(legacy)
	if err != nil {
		return err
	}
	return history.InsertMissing(ctx, record)
}

// setBackfill 以互斥鎖發佈進度，HTTP 查詢不需等待回補計算。
func (s *StrategyMonitorService) setBackfill(status string, completed, total int, message string) {
	s.mu.Lock()
	s.backfill = dto.StrategyBackfillStatus{Status: status, Completed: completed, Total: total, Message: message}
	s.mu.Unlock()
}

// StartBackfill 啟動可取消的背景回補，每五分鐘檢查新缺口，錯誤後三十秒自動重試。
func (s *StrategyMonitorService) StartBackfill(ctx context.Context) {
	s.startOnce.Do(func() {
		go func() {
			for {
				err := s.BackfillHistory(ctx)
				delay := 5 * time.Minute
				if err != nil {
					if s.reportBackfillError != nil && ctx.Err() == nil {
						s.reportBackfillError(err)
					}
					delay = 30 * time.Second
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	})
}

// readHistoryInputs 在可重複讀取交易中擷取資料，計算及保存均在交易外執行。
func (s *StrategyMonitorService) readHistoryInputs(ctx context.Context) (historyInputs, error) {
	out := historyInputs{series: &frozenHistorySeries{book: bookFor(s.series)}}
	read := func(ctx context.Context) error {
		var err error
		out.watermark, _, err = s.state.Get(ctx, stateKeyWatermark)
		if err != nil {
			return err
		}
		if out.lots, err = s.ledger.ListUnrealized(ctx); err != nil {
			return err
		}
		if out.realized, err = s.ledger.ListRealized(ctx); err != nil {
			return err
		}
		if out.equity, err = s.equity.ListEquityAsc(ctx); err != nil {
			return err
		}
		out.series.rows, err = s.series.LoadSeries(ctx, s.cfg.TrackStocks)
		if err == nil && !reflect.DeepEqual(out.series.book, bookFor(s.series)) {
			return fmt.Errorf("market unit book changed during history read; retry")
		}
		return err
	}
	// 正式 repository 使用一致的唯讀交易；記憶體資料源直接讀取。
	if reader, ok := s.state.(historyReadSnapshot); ok {
		err := reader.WithReadSnapshot(ctx, read)
		return out, err
	}
	err := read(ctx)
	return out, err
}

// evidenceDates 僅收集有每日權益或交易帳本證據且不超過決策水位線的日期。
func evidenceDates(in historyInputs) ([]string, error) {
	if in.watermark == "" {
		return []string{}, nil
	}
	wm, err := parseLedgerDate(in.watermark)
	if err != nil {
		return nil, fmt.Errorf("invalid history watermark: %w", err)
	}
	seen := map[string]bool{}
	add := func(raw string) {
		if day, err := parseLedgerDate(raw); err == nil && !day.After(wm) {
			seen[day.Format(dateLayout)] = true
		}
	}
	for _, r := range in.equity {
		add(r.Date)
	}
	for _, r := range in.lots {
		add(r.TransactionDate)
	}
	for _, r := range in.realized {
		add(r.BuyDate)
		add(r.SellDate)
	}
	out := make([]string, 0, len(seen))
	for date := range seen {
		out = append(out, date)
	}
	sort.Strings(out)
	return out, nil
}

// BackfillHistory 僅新增缺少日期；所有實際交易與歷史原始資料保持不變。
func (s *StrategyMonitorService) BackfillHistory(ctx context.Context) (err error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	completed, total := 0, 0
	defer func() {
		if err != nil {
			s.setBackfill("failed", completed, total, "回補暫時失敗，系統將自動重試；既有紀錄可繼續查看。")
		}
	}()
	if s.history == nil || s.ledger == nil || s.equity == nil || s.series == nil || s.cfg == nil {
		return fmt.Errorf("history backfill dependencies unavailable")
	}
	s.setBackfill("running", 0, 0, "正在讀取歷史帳本及決策日期。")
	if err = archiveLegacyMonitor(ctx, s.state, s.history); err != nil {
		return err
	}
	inputs, err := s.readHistoryInputs(ctx)
	if err != nil {
		return fmt.Errorf("read history inputs: %w", err)
	}
	dates, err := evidenceDates(inputs)
	if err != nil {
		return err
	}
	total = len(dates)
	rows, err := s.history.ListHistory(ctx)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, row := range rows {
		existing[row.DecisionDate] = true
	}
	revisions, err := s.prepareCashflowRevisions(ctx, rows, inputs)
	if err != nil {
		return err
	}
	// 缺少行情或帳本細節的日期仍保留可查看的不可用紀錄。
	series, seriesErr := LoadTradingSeries(ctx, inputs.series, s.cfg.TrackStocks)
	lots, trades, ledgerErr := historyLedger(inputs)
	if seriesErr != nil {
		return fmt.Errorf("load reconstruction series: %w", seriesErr)
	}
	if ledgerErr != nil {
		return fmt.Errorf("validate reconstruction ledger: %w", ledgerErr)
	}
	unavailable := 0
	for _, date := range dates {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !existing[date] {
			// 最新交易日的盤後權益仍可稍後寫入，不把暫時未結算資料凍結為永久缺值。
			if date == inputs.watermark {
				closed := false
				for _, snap := range inputs.equity {
					if d, parseErr := parseLedgerDate(snap.Date); parseErr == nil && d.Format(dateLayout) == date {
						closed = true
						break
					}
				}
				if !closed {
					return fmt.Errorf("latest decision awaits closed equity history")
				}
			}
			v := s.reconstructHistory(date, inputs, series, lots, trades, seriesErr, ledgerErr)
			if v.Availability == "unavailable" {
				unavailable++
			}
			record, encodeErr := historyRecord(v)
			if encodeErr != nil {
				return encodeErr
			}
			if err = s.history.InsertMissing(ctx, record); err != nil {
				return err
			}
		}
		if revision, ok := revisions[date]; ok {
			if err = s.appendCashflowRevision(ctx, date, inputs, lots, trades, revision); err != nil {
				return err
			}
		}
		completed++
		s.setBackfill("running", completed, total, "依帳本補齊缺少日期與現金精度修訂，初版紀錄保持不變。")
	}
	message := "已補齊有帳本或每日權益證據的決策日期；新決策將持續自動保存。"
	if unavailable > 0 {
		message = fmt.Sprintf("已保存所有可確認日期，其中 %d 日資料不足，可選取日期查看原因。", unavailable)
	}
	s.setBackfill("complete", completed, total, message)
	return nil
}

// historyLedger 聚合同日買入片段並保留每個出售片段的原始買入日期。
func historyLedger(in historyInputs) ([]historicalLot, map[string][]trading.HistoricalTrade, error) {
	lots := []historicalLot{}
	trades := map[string][]trading.HistoricalTrade{}
	buys := map[string]trading.HistoricalTrade{}
	add := func(id, buyRaw, sellRaw string, shares int, price, sellPrice, buyAmount, sellAmount float64) error {
		buy, err := parseLedgerDate(buyRaw)
		if err != nil {
			return fmt.Errorf("帳本買入日期無法解析")
		}
		if shares <= 0 || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return fmt.Errorf("帳本缺少有效股數或成本，無法可靠還原持倉")
		}
		if buyAmount < 0 || sellAmount < 0 || math.IsNaN(buyAmount) || math.IsInf(buyAmount, 0) || math.IsNaN(sellAmount) || math.IsInf(sellAmount, 0) {
			return fmt.Errorf("帳本成交金額不是有效非負數")
		}
		if buyAmount == 0 {
			buyAmount = float64(shares) * price
		}
		lot := historicalLot{stock: id, buy: buy, shares: shares, price: price}
		if sellRaw != "" {
			sell, err := parseLedgerDate(sellRaw)
			if err != nil || sell.Before(buy) || sellPrice <= 0 || math.IsNaN(sellPrice) || math.IsInf(sellPrice, 0) {
				return fmt.Errorf("帳本賣出資料不完整")
			}
			lot.sell = sell
			date := sell.Format(dateLayout)
			if sellAmount == 0 {
				sellAmount = float64(shares) * sellPrice
			}
			trades[date] = append(trades[date], trading.HistoricalTrade{StockID: id, Buy: false, Shares: shares, Price: sellPrice, BuyDate: buy, Amount: sellAmount})
		}
		key := id + "/" + buy.Format(dateLayout)
		v := buys[key]
		if v.Shares > 0 && math.Abs(v.Price-price) > 1e-7*math.Max(1, price) {
			return fmt.Errorf("同日買入片段的成本不一致")
		}
		v.StockID = id
		v.Buy = true
		v.Shares += shares
		v.Amount += buyAmount
		v.Price = price
		v.BuyDate = buy
		buys[key] = v
		lots = append(lots, lot)
		return nil
	}
	// 帳本以永久會計單位儲存，此處不再次做股票分割換算。
	for _, r := range in.lots {
		if err := add(r.StockID, r.TransactionDate, "", r.Shares, r.TransactionPrice, 0, r.InvestmentCost, 0); err != nil {
			return nil, nil, err
		}
	}
	for _, r := range in.realized {
		if err := add(r.StockID, r.BuyDate, r.SellDate, r.Shares, r.PurchasePrice, r.SellPrice, r.InvestmentCost, r.Revenue); err != nil {
			return nil, nil, err
		}
	}
	keys := make([]string, 0, len(buys))
	for key := range buys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := buys[key]
		date := v.BuyDate.Format(dateLayout)
		trades[date] = append(trades[date], v)
	}
	return lots, trades, nil
}

// reconstructHistory 由真實帳本建立每日起始部位，僅使用目前規則觀察歷史條件。
func (s *StrategyMonitorService) reconstructHistory(date string, in historyInputs, series map[string]*trading.StockSeries, lots []historicalLot, trades map[string][]trading.HistoricalTrade, seriesErr, ledgerErr error) dto.StrategyMonitor {
	day, _ := time.Parse(dateLayout, date)
	temporary := &TradingService{cfg: s.cfg, series: in.series}
	out := temporary.monitorSnapshot(day, nil, false)
	out.Source = "reconstructed"
	out.ExecutionMode = "catchup"
	out.Message = "依真實帳本與歷史行情，使用回補當下策略重建的決策圖表。"
	out.Warnings = []string{"歷史策略設定及無成交日的即時開盤報價未完整保存；條件採用回補時的目前規則與歷史行情，並非原始決策理由。", "成交股數與持倉沿用帳本；目前規則的預期與歷史成交可能不同。歷史賣出後冷卻以帳本最後賣出日推估。", "歷史現金由當日權益紀錄與成交流水還原，受權益紀錄小數精度限制；股票處理順序採目前設定。", "持倉峰值依完整帳本推導最近一次全數出場後的持倉期間，並使用歷史行情與已知成交價重建。"}
	unavailable := func(reason string) dto.StrategyMonitor {
		out.Availability = "unavailable"
		out.Message = reason
		out.Warnings = append(out.Warnings, reason)
		out.Stocks = []dto.StrategyMonitorStock{}
		return out
	}
	if seriesErr != nil {
		return unavailable("歷史行情不足，無法還原此日決策條件。")
	}
	if ledgerErr != nil {
		return unavailable(ledgerErr.Error())
	}
	// 日終現金加回買額並扣除賣額，得到已含當日注資的決策前現金。
	cash, found := 0.0, false
	for _, v := range in.equity {
		if d, err := parseLedgerDate(v.Date); err == nil && d.Equal(day) {
			cash = v.Cash
			found = true
			break
		}
	}
	if !found {
		return unavailable("此日有交易或水位線紀錄，但缺少每日權益現金，無法可靠還原買賣階段。")
	}
	expectedCash := cash
	for _, v := range trades[date] {
		amount := v.Amount
		if amount == 0 {
			amount = float64(v.Shares) * v.Price
		}
		if v.Buy {
			cash += amount
		} else {
			cash -= amount
		}
	}
	if cash < -0.011 || math.IsNaN(cash) || math.IsInf(cash, 0) {
		return unavailable("當日現金與帳本無法對齊，無法可靠還原決策。")
	}
	cash = math.Max(0, cash)
	engine := trading.NewEngine(s.cfg)
	engine.SeedCash(cash)
	lastBuy, lastSell := map[string]time.Time{}, map[string]time.Time{}
	for _, lot := range lots {
		if lot.buy.Before(day) {
			if lot.buy.After(lastBuy[lot.stock]) {
				lastBuy[lot.stock] = lot.buy
			}
			if lot.sell.IsZero() || !lot.sell.Before(day) {
				engine.SeedPosition(lot.stock, lot.buy, lot.shares, lot.price)
			}
		}
		if !lot.sell.IsZero() && lot.sell.Before(day) && lot.sell.After(lastSell[lot.stock]) {
			lastSell[lot.stock] = lot.sell
		}
	}
	for id, d := range lastBuy {
		engine.SeedLastBuy(id, d)
	}
	for id, d := range lastSell {
		engine.SeedLastTrailSell(id, d)
	}
	for id, peak := range historicalPeaks(lots, trades, series, day, s.cfg.DecisionPriceBasis == "open") {
		engine.SeedRiskState(id, trading.RiskState{LastBuy: lastBuy[id], LastTrailSell: lastSell[id], Peak: peak})
	}
	observations, err := engine.ObserveHistoricalDay(day, series, trades[date])
	if err != nil {
		return unavailable("帳本與歷史行情無法一致還原此日買賣階段：" + err.Error())
	}
	if math.Abs(engine.Cash()-expectedCash) > 0.011 {
		return unavailable("重建日終現金與每日權益紀錄不一致，無法可靠還原決策。")
	}
	// 顯示單位固定於歷史決策日，之後查閱不隨新公司行動重新換算。
	observed := temporary.monitorSnapshot(day, observations, false)
	out.Stocks = observed.Stocks
	for _, stock := range out.Stocks {
		if stock.Status == "unavailable" {
			out.Warnings = append(out.Warnings, stock.StockID+" 的歷史行情或可見指標不足，此股票不顯示推測訊號。")
		}
	}
	return out
}
