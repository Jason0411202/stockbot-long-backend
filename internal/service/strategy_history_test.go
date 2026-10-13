// internal/service/strategy_history_test.go 驗證永久歷史、帳本重建、冪等重試及唯讀回補。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// fakeStrategyHistory 提供具有不可變日期語意的記憶體歷史儲存器。
type fakeStrategyHistory struct {
	records   map[string]entity.StrategyHistoryRecord
	insertErr error
	calls     int
}

// InsertMissing 僅新增未保存日期，並支援測試儲存失敗。
func (f *fakeStrategyHistory) InsertMissing(_ context.Context, v entity.StrategyHistoryRecord) error {
	f.calls++
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, ok := f.records[v.DecisionDate]; !ok {
		f.records[v.DecisionDate] = v
	}
	return nil
}

// GetDate 讀取指定日期。
func (f *fakeStrategyHistory) GetDate(_ context.Context, date string) (entity.StrategyHistoryRecord, bool, error) {
	v, ok := f.records[date]
	return v, ok, nil
}

// ListHistory 依日期降冪列出索引。
func (f *fakeStrategyHistory) ListHistory(context.Context) ([]entity.StrategyHistoryRecord, error) {
	out := []entity.StrategyHistoryRecord{}
	for _, r := range f.records {
		r.Snapshot = ""
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DecisionDate > out[j].DecisionDate })
	return out, nil
}

// historyReadState 記錄背景回補是否透過一致資料庫快照讀取。
type historyReadState struct {
	*fakeState
	reads int
}

// WithReadSnapshot 執行記憶體一致讀取並累計呼叫。
func (s *historyReadState) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	s.reads++
	return fn(ctx)
}

// historyFixture 建立分段賣出、未成交日與未知本金設定的歷史資料。
func historyFixture() (*StrategyMonitorService, *fakeStrategyHistory, *historyReadState, *fakeLedger, *fakeEquity) {
	cfg := tradingTestCfg("AAA")
	cfg.DecisionPriceBasis = "open"
	cfg.InitialCash = 999999
	cfg.BearBuyFrac = 0
	state := &historyReadState{fakeState: newFakeState()}
	state.values[stateKeyWatermark] = "2024-03-04"
	ledger := &fakeLedger{listU: []entity.UnrealizedGainsLoss{{StockID: "AAA", TransactionDate: "2024-03-01", Shares: 5, TransactionPrice: 100}}, listR: []entity.RealizedGainsLoss{{StockID: "AAA", BuyDate: "2024-03-01", SellDate: "2024-03-02", Shares: 3, PurchasePrice: 100, SellPrice: 120}, {StockID: "AAA", BuyDate: "2024-03-01", SellDate: "2024-03-03", Shares: 2, PurchasePrice: 100, SellPrice: 110}}}
	equity := &fakeEquity{list: []entity.EquitySnapshot{{Date: "2024-03-01", Cash: 0}, {Date: "2024-03-02", Cash: 360}, {Date: "2024-03-03", Cash: 580}, {Date: "2024-03-04", Cash: 580}, {Date: "2024-03-05", Cash: 580}}}
	rows := []entity.StockHistory{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 65; i++ {
		rows = append(rows, entity.StockHistory{Date: start.AddDate(0, 0, i).Format(dateLayout), OpenPrice: 100, ClosePrice: 100})
	}
	history := &fakeStrategyHistory{records: map[string]entity.StrategyHistoryRecord{}}
	svc := NewStrategyMonitorService(state).WithHistory(history, ledger, equity, &fakeSeriesLoader{data: map[string][]entity.StockHistory{"AAA": rows}}, cfg)
	return svc, history, state, ledger, equity
}

// TestStrategyHistoryBackfill_UsesLedgerAndPreservesAllSources 驗證現行策略不改歷史成交且既有資料永不覆寫。
func TestStrategyHistoryBackfill_UsesLedgerAndPreservesAllSources(t *testing.T) {
	svc, history, state, ledger, equity := historyFixture()
	ctx := context.Background()
	beforeState := copyValues(state.values)
	beforeLedger, _ := json.Marshal(ledger.listR)
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if state.reads != 1 || len(history.records) != 4 {
		t.Fatalf("history capture mismatch: reads=%d records=%d", state.reads, len(history.records))
	}
	first, err := svc.StrategyMonitorDate(ctx, "2024-03-01")
	if err != nil {
		t.Fatal(err)
	}
	buy := first.Stocks[0].Buy
	if buy.ExecutedShares != 10 || buy.IntendedShares != 0 || buy.CashBefore != 1000 || buy.CashAfter != 0 || first.Stocks[0].Sell.HeldSharesBefore != 10 {
		t.Fatalf("ledger fragments or actual prestate changed: %+v", first.Stocks[0])
	}
	if first.Source != "reconstructed" || first.ConfigHash == "" || len(first.StrategyConfig) == 0 || len(first.UnitBook) == 0 || len(first.Warnings) == 0 {
		t.Fatal("missing reconstruction provenance")
	}
	third, _ := svc.StrategyMonitorDate(ctx, "2024-03-03")
	if third.Stocks[0].Sell.HeldSharesBefore != 7 || third.Stocks[0].Sell.ExecutedShares != 2 || third.Stocks[0].Sell.HeldSharesAfter != 5 {
		t.Fatalf("partial-lot replay wrong: %+v", third.Stocks[0].Sell)
	}
	latest, err := svc.StrategyMonitor(ctx)
	if err != nil || latest.DecisionDate != "2024-03-04" || latest.Source != "reconstructed" {
		t.Fatalf("latest backfill unavailable: %+v %v", latest, err)
	}
	index, _ := svc.StrategyHistory(ctx)
	if index.Backfill.Status != "complete" || index.Backfill.Completed != 4 || index.NewestDate != "2024-03-04" || index.OldestDate != "2024-03-01" {
		t.Fatalf("index %+v", index)
	}
	beforeRecord := history.records["2024-03-01"].Snapshot
	calls := history.calls
	svc.cfg.BearBuyFrac = 1
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if history.calls != calls || history.records["2024-03-01"].Snapshot != beforeRecord {
		t.Fatal("existing reconstructed history was overwritten")
	}
	afterLedger, _ := json.Marshal(ledger.listR)
	if !reflect.DeepEqual(beforeState, state.values) || string(beforeLedger) != string(afterLedger) || len(state.setKeys) > 0 || len(ledger.deletes) > 0 || len(ledger.updates) > 0 || len(ledger.lots) > 0 || len(ledger.realized) > 0 || len(equity.recorded) > 0 {
		t.Fatal("backfill mutated live financial state")
	}
}

// TestStrategyHistoryBackfill_PreservesLegacyOriginal 驗證舊快照數值優先保留且新日期索引即時更新。
func TestStrategyHistoryBackfill_PreservesLegacyOriginal(t *testing.T) {
	svc, history, state, _, _ := historyFixture()
	original := storedMonitorFixture()
	state.values[stateKeyStrategyMonitor] = encodeStoredMonitor(t, original)
	if err := svc.BackfillHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := svc.StrategyMonitorDate(context.Background(), original.DecisionDate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "original" || got.StrategyVersion != "legacy-unknown" || !reflect.DeepEqual(got.Stocks, original.Stocks) {
		t.Fatalf("legacy snapshot changed: %+v", got)
	}
	newer := original
	newer.DecisionDate = "2024-03-06"
	newer.Source = "original"
	r, _ := historyRecord(newer)
	if err := history.InsertMissing(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	index, _ := svc.StrategyHistory(context.Background())
	if index.NewestDate != "2024-03-06" {
		t.Fatal("history index cached stale dates")
	}
}

// TestStrategyHistoryBackfill_RetriesFailuresAndDefersUnclosedDay 驗證儲存錯誤可重試且未結算當日不凍結缺值。
func TestStrategyHistoryBackfill_RetriesFailuresAndDefersUnclosedDay(t *testing.T) {
	svc, history, _, _, equity := historyFixture()
	history.insertErr = errFake
	if err := svc.BackfillHistory(context.Background()); !errors.Is(err, errFake) {
		t.Fatalf("error=%v", err)
	}
	index, _ := svc.StrategyHistory(context.Background())
	if index.Backfill.Status != "failed" || len(history.records) != 0 {
		t.Fatal("failed backfill published data")
	}
	history.insertErr = nil
	equity.list = equity.list[:3]
	if err := svc.BackfillHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(history.records) != 3 {
		t.Fatal("watermark alone manufactured decision history")
	}
	svc.state.(*historyReadState).values[stateKeyWatermark] = "2024-03-03"
	delete(history.records, "2024-03-03")
	equity.list = equity.list[:2]
	if err := svc.BackfillHistory(context.Background()); err == nil {
		t.Fatal("unclosed latest day froze incomplete data")
	}
	if _, ok := history.records["2024-03-03"]; ok {
		t.Fatal("unclosed day was stored")
	}
	equity.list = append(equity.list, entity.EquitySnapshot{Date: "2024-03-03", Cash: 580})
	if err := svc.BackfillHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if history.records["2024-03-03"].Availability != "available" {
		t.Fatal("retry failed to recover")
	}
}

// TestStrategyHistoryBackfill_MissingOldEquityAndInvalidLedger 驗證舊缺值可查閱而全域不一致留待重試。
func TestStrategyHistoryBackfill_MissingOldEquityAndInvalidLedger(t *testing.T) {
	svc, history, _, ledger, equity := historyFixture()
	equity.list = equity.list[1:]
	if err := svc.BackfillHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	missing, _ := svc.StrategyMonitorDate(context.Background(), "2024-03-01")
	if missing.Availability != "unavailable" || len(missing.Stocks) != 0 || len(missing.Warnings) == 0 {
		t.Fatal("missing historical cash was fabricated")
	}
	svc2, history2, _, _, _ := historyFixture()
	svc2.ledger = ledger
	ledger.listU[0].Shares = 0
	if err := svc2.BackfillHistory(context.Background()); err == nil || len(history2.records) != 0 {
		t.Fatal("invalid global ledger froze fabricated history")
	}
	if _, ok := history.records["2024-03-05"]; ok {
		t.Fatal("history exceeded committed watermark")
	}
}

// TestStrategyHistoryBackfill_UsesDecisionDateDisplayUnits 驗證歷史分割價格股數保留決策日單位。
func TestStrategyHistoryBackfill_UsesDecisionDateDisplayUnits(t *testing.T) {
	svc, _, _, _, _ := historyFixture()
	base := svc.series.(*fakeSeriesLoader)
	svc.series = unitSeriesLoader{base, marketunits.Book{Basis: marketunits.BasisDate, Actions: []marketunits.Action{{StockID: "AAA", Date: "2024-03-03", Ratio: 4}}}}
	if err := svc.BackfillHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, _ := svc.StrategyMonitorDate(context.Background(), "2024-03-01")
	if *first.Stocks[0].DecisionPrice != 400 || first.Stocks[0].Buy.ExecutedShares != 2.5 || first.Stocks[0].Buy.CashBefore != 1000 {
		t.Fatalf("history units changed economic value: %+v", first.Stocks[0])
	}
}

// atomicHistoryState 在交易失敗時一併還原測試中的歷史紀錄。
type atomicHistoryState struct {
	*fakeState
	history *fakeStrategyHistory
}

// WithTradingTransaction 模擬同筆交易對歷史儲存器的回滾。
func (s *atomicHistoryState) WithTradingTransaction(ctx context.Context, fn func(context.Context) error) error {
	before := map[string]entity.StrategyHistoryRecord{}
	for k, v := range s.history.records {
		before[k] = v
	}
	err := s.fakeState.WithTradingTransaction(ctx, fn)
	if err != nil {
		s.history.records = before
	}
	return err
}

// TestStrategyHistoryCommit_AtomicFailureAndLegacyArchive 驗證歷史寫入失敗回滾帳本與通知且重試不重複。
func TestStrategyHistoryCommit_AtomicFailureAndLegacyArchive(t *testing.T) {
	for _, failure := range []string{"history", "commit"} {
		t.Run(failure, func(t *testing.T) {
			cfg := tradingTestCfg("AAA")
			cfg.DecisionPriceBasis = "open"
			svc, _, state, notify, ledger, stock := newTradingFixture(cfg)
			stock.names["AAA"] = "AAA"
			day := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
			state.values[stateKeyWatermark] = "2024-02-29"
			legacy := storedMonitorFixture()
			legacy.DecisionDate = "2024-02-29"
			state.values[stateKeyStrategyMonitor] = encodeStoredMonitor(t, legacy)
			history := &fakeStrategyHistory{records: map[string]entity.StrategyHistoryRecord{}}
			svc.state = &atomicHistoryState{state, history}
			svc.WithStrategyHistory(history)
			if failure == "history" {
				history.insertErr = errFake
			} else {
				state.commitErr = errFake
			}
			series := map[string]*trading.StockSeries{"AAA": flatSeries(day.AddDate(0, 0, -60), 60, 100)}
			beforeCash := svc.engine.Cash()
			if err := svc.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, true); err == nil {
				t.Fatal("expected failure")
			}
			if len(history.records) != 0 || len(ledger.lots) != 0 || len(notify.tradeSent) != 0 || svc.engine.Cash() != beforeCash || state.values[stateKeyWatermark] != "2024-02-29" {
				t.Fatal("history failure leaked committed financial effects")
			}
			history.insertErr = nil
			state.commitErr = nil
			if err := svc.reloadIfNeeded(context.Background(), series); err != nil {
				t.Fatal(err)
			}
			if err := svc.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, true); err != nil {
				t.Fatal(err)
			}
			if len(history.records) != 2 || history.records["2024-02-29"].Source != "original" || history.records["2024-03-01"].Source != "original" || len(ledger.lots) != 1 || len(notify.tradeSent) != 1 {
				t.Fatal("retry lost archive or duplicated effects")
			}
			frozen := history.records["2024-03-01"].Snapshot
			if err := svc.commitDay(context.Background(), day, series, map[string]float64{"AAA": 90}, true); err != nil || history.records["2024-03-01"].Snapshot != frozen {
				t.Fatal("duplicate day changed history")
			}
		})
	}
}
