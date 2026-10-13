// internal/service/strategy_history_revision_test.go 驗證帳本金額修訂、初版永久保留與版本選取。
package service

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
)

// fakeStrategyRevisions 以獨立 map 保存不可變修訂，初版仍由既有儲存器提供。
type fakeStrategyRevisions struct {
	*fakeStrategyHistory
	revisions     map[string]entity.StrategyHistoryRecord
	revisionCalls int
	revisionErr   error
}

// TestStrategyCashflowRevision_ProductionPrecisionGap 驗證87.71元截位差距不再破壞歷史現金。
func TestStrategyCashflowRevision_ProductionPrecisionGap(t *testing.T) {
	ctx := context.Background()
	cfg := tradingTestCfg("AAA", "BBB")
	cfg.DecisionPriceBasis = "open"
	cfg.BearBuyFrac = 0
	state := newFakeState()
	state.values[stateKeyWatermark] = "2023-08-14"
	ledger := &fakeLedger{listR: []entity.RealizedGainsLoss{{StockID: "AAA", BuyDate: "2023-06-01", SellDate: "2023-08-14", Shares: 40469, PurchasePrice: 5, InvestmentCost: 202345, SellPrice: 5.65, Revenue: 228562.14}}, listU: []entity.UnrealizedGainsLoss{{StockID: "BBB", TransactionDate: "2023-08-14", Shares: 1389, TransactionPrice: 32.92, InvestmentCost: 45725.88}}}
	equity := &fakeEquity{list: []entity.EquitySnapshot{{Date: "2023-08-11", Cash: 28.97}, {Date: "2023-08-14", Cash: 182865.23}}}
	rows := map[string][]entity.StockHistory{}
	day := time.Date(2023, 8, 14, 0, 0, 0, 0, time.UTC)
	for id, price := range map[string]float64{"AAA": 5.65, "BBB": 32.92} {
		for i := 90; i >= 0; i-- {
			rows[id] = append(rows[id], entity.StockHistory{Date: day.AddDate(0, 0, -i).Format(dateLayout), OpenPrice: price, ClosePrice: price})
		}
	}
	loader := &fakeSeriesLoader{data: rows}
	base := &fakeStrategyHistory{records: map[string]entity.StrategyHistoryRecord{}}
	store := &fakeStrategyRevisions{fakeStrategyHistory: base, revisions: map[string]entity.StrategyHistoryRecord{}}
	t.Setenv("APP_COMMIT", strings.TrimSuffix(cashflowInitialBuild, ":"))
	initial := (&TradingService{cfg: cfg, series: loader}).monitorSnapshot(day, nil, false)
	initial.Source = "reconstructed"
	initial.Availability = "unavailable"
	initial.Message = "當日現金與帳本無法對齊，無法可靠還原決策。"
	record, _ := historyRecord(initial)
	base.records[initial.DecisionDate] = record
	svc := NewStrategyMonitorService(state).WithHistory(store, ledger, equity, loader, cfg)
	t.Setenv("APP_COMMIT", "fixed-deployment")
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := svc.StrategyMonitorDate(ctx, initial.DecisionDate)
	if err != nil || got.Availability != "available" || len(got.Stocks) != 2 {
		t.Fatalf("precision day unavailable %+v %v", got, err)
	}
	if math.Abs(got.Stocks[0].Buy.CashBefore-28.97) > 1e-7 || math.Abs(got.Stocks[1].Sell.CashAfter-182865.23) > 1e-7 || *got.Stocks[0].DecisionPrice != 5.65 {
		t.Fatalf("cash flow / price conflated %+v", got.Stocks)
	}
	if base.records[initial.DecisionDate].Snapshot != record.Snapshot {
		t.Fatal("production correction rewrote initial bytes")
	}
}

// InsertRevision 模擬只追加修訂並保留既有版次。
func (f *fakeStrategyRevisions) InsertRevision(_ context.Context, r entity.StrategyHistoryRecord) error {
	f.revisionCalls++
	if f.revisionErr != nil {
		return f.revisionErr
	}
	key := r.DecisionDate + "/" + r.Revision
	if _, ok := f.revisions[key]; !ok {
		f.revisions[key] = r
	}
	return nil
}

// GetRevision 讀取指定版次。
func (f *fakeStrategyRevisions) GetRevision(_ context.Context, date, revision string) (entity.StrategyHistoryRecord, bool, error) {
	r, ok := f.revisions[date+"/"+revision]
	return r, ok, nil
}

// ListRevisions 列出指定版次摘要，不回傳本文。
func (f *fakeStrategyRevisions) ListRevisions(_ context.Context, revision string) ([]entity.StrategyHistoryRecord, error) {
	out := []entity.StrategyHistoryRecord{}
	for _, r := range f.revisions {
		if r.Revision == revision {
			r.Snapshot = ""
			out = append(out, r)
		}
	}
	return out, nil
}

// TestStrategyCashflowRevision_PreservesInitialAndFrozenRules 驗證只修指定部署重建資料且永不覆寫初版。
func TestStrategyCashflowRevision_PreservesInitialAndFrozenRules(t *testing.T) {
	svc, base, state, ledger, _ := historyFixture()
	ctx := context.Background()
	t.Setenv("APP_COMMIT", strings.TrimSuffix(cashflowInitialBuild, ":"))
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	// 初版第二日保留精度錯誤的不可用結果，其他來源／部署不在修訂範圍。
	var initial dto.StrategyMonitor
	if err := json.Unmarshal([]byte(base.records["2024-03-02"].Snapshot), &initial); err != nil {
		t.Fatal(err)
	}
	initial.Availability = "unavailable"
	initial.Stocks = []dto.StrategyMonitorStock{}
	initial.Message = "當日現金與帳本無法對齊，無法可靠還原決策。"
	r, _ := historyRecord(initial)
	base.records[r.DecisionDate] = r
	for date, source := range map[string]string{"2024-03-03": "original", "2024-03-04": "reconstructed"} {
		var v dto.StrategyMonitor
		_ = json.Unmarshal([]byte(base.records[date].Snapshot), &v)
		v.Source = source
		if date == "2024-03-04" {
			v.StrategyVersion = "other-deployment:hash"
		}
		r, _ := historyRecord(v)
		base.records[date] = r
	}
	before := map[string]entity.StrategyHistoryRecord{}
	for k, v := range base.records {
		before[k] = v
	}
	beforeState := copyValues(state.values)
	store := &fakeStrategyRevisions{fakeStrategyHistory: base, revisions: map[string]entity.StrategyHistoryRecord{}}
	svc.history = store
	// 截位售價的名目金額為363，帳本收入360才是現金流。現行設定另外改動以驗證凍結。
	ledger.listR[0].SellPrice = 121
	ledger.listR[0].Revenue = 360
	svc.cfg.BearBuyFrac = 1
	changedRows := map[string][]entity.StockHistory{}
	for id, rows := range svc.series.(*fakeSeriesLoader).data {
		for _, row := range rows {
			if row.Date < "2024-03-03" {
				row.OpenPrice /= 4
				row.ClosePrice /= 4
			}
			changedRows[id] = append(changedRows[id], row)
		}
	}
	svc.series = unitSeriesLoader{&fakeSeriesLoader{data: changedRows}, marketunits.Book{Basis: marketunits.BasisDate, Actions: []marketunits.Action{{StockID: "AAA", Date: "2024-03-03", Ratio: 4}}}}
	t.Setenv("APP_COMMIT", "fixed-deployment")
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.revisions) != 2 || !reflect.DeepEqual(base.records, before) || !reflect.DeepEqual(state.values, beforeState) {
		t.Fatal("revision changed initial snapshots or expanded eligibility")
	}
	got, err := svc.StrategyMonitorDate(ctx, "2024-03-02")
	if err != nil {
		t.Fatal(err)
	}
	if got.Availability != "available" || got.Revision != StrategyCashflowRevision || got.SupersedesRevision != StrategyInitialRevision || got.ReconstructionBaseVersion != initial.StrategyVersion {
		t.Fatalf("invalid revision metadata %+v", got)
	}
	if got.Stocks[0].Buy.CashBefore != 0 || got.Stocks[0].Sell.CashAfter != 360 || got.Stocks[0].Buy.IntendedShares != 0 {
		t.Fatalf("ledger amount or frozen rules ignored %+v", got.Stocks[0])
	}
	if *got.Stocks[0].Buy.Metrics[0].Value != 100 {
		t.Fatal("revision used current unit book instead of initial prices")
	}
	if !reflect.DeepEqual(got.StrategyConfig, initial.StrategyConfig) || got.ConfigHash != initial.ConfigHash || !reflect.DeepEqual(got.UnitBook, initial.UnitBook) || !strings.HasPrefix(got.StrategyVersion, "fixed-deployment:") {
		t.Fatal("initial config or unit basis changed")
	}
	old, err := svc.StrategyMonitorRevision(ctx, "2024-03-02", StrategyInitialRevision)
	if err != nil || old.Availability != "unavailable" || old.Revision != StrategyInitialRevision {
		t.Fatal("initial revision no longer available")
	}
	if _, err := svc.StrategyMonitorRevision(ctx, "2024-03-03", StrategyCashflowRevision); err != ErrStrategyHistoryNotFound {
		t.Fatalf("original unexpectedly revised: %v", err)
	}
	index, _ := svc.StrategyHistory(ctx)
	for _, entry := range index.Entries {
		revised := entry.DecisionDate < "2024-03-03"
		if entry.HasInitialRevision != revised || (revised && entry.Revision != StrategyCashflowRevision) {
			t.Fatalf("index revision mismatch %+v", entry)
		}
	}
	calls := store.revisionCalls
	if err := svc.BackfillHistory(ctx); err != nil || store.revisionCalls != calls || !reflect.DeepEqual(base.records, before) {
		t.Fatal("repeat backfill rewrote an existing revision")
	}
}

// TestHistoryLedger_UsesAmountsAcrossPartialFragments 驗證買單片段累計成本而不是截位價格乘股數。
func TestHistoryLedger_UsesAmountsAcrossPartialFragments(t *testing.T) {
	in := historyInputs{lots: []entity.UnrealizedGainsLoss{{StockID: "A", TransactionDate: "2023-01-01", Shares: 2, TransactionPrice: 5.65, InvestmentCost: 11.29}}, realized: []entity.RealizedGainsLoss{{StockID: "A", BuyDate: "2023-01-01", SellDate: "2023-08-14", Shares: 3, PurchasePrice: 5.65, InvestmentCost: 16.94, SellPrice: 6.12, Revenue: 18.35}}}
	_, trades, err := historyLedger(in)
	if err != nil {
		t.Fatal(err)
	}
	buy := trades["2023-01-01"][0]
	sell := trades["2023-08-14"][0]
	if buy.Shares != 5 || buy.Amount != 28.23 || sell.Amount != 18.35 {
		t.Fatalf("cash flow changed: buy=%+v sell=%+v", buy, sell)
	}
	in.realized[0].Revenue = -1
	if _, _, err := historyLedger(in); err == nil {
		t.Fatal("invalid cash flow accepted")
	}
}

// TestStrategyCashflowRevision_FailureRetriesWithoutChangingInitial 驗證追加失敗可重試且初版始終可查。
func TestStrategyCashflowRevision_FailureRetriesWithoutChangingInitial(t *testing.T) {
	svc, base, _, _, _ := historyFixture()
	t.Setenv("APP_COMMIT", strings.TrimSuffix(cashflowInitialBuild, ":"))
	ctx := context.Background()
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	original := base.records["2024-03-01"].Snapshot
	store := &fakeStrategyRevisions{fakeStrategyHistory: base, revisions: map[string]entity.StrategyHistoryRecord{}, revisionErr: errFake}
	svc.history = store
	if err := svc.BackfillHistory(ctx); err == nil || len(store.revisions) != 0 || base.records["2024-03-01"].Snapshot != original {
		t.Fatal("failed correction lost initial")
	}
	store.revisionErr = nil
	if err := svc.BackfillHistory(ctx); err != nil || len(store.revisions) != 4 {
		t.Fatalf("retry failed %v", err)
	}
}

// TestStrategyCashflowRevision_DoesNotDowngradeAvailableInitial 驗證暫時資料缺損不永久降低初版可用性。
func TestStrategyCashflowRevision_DoesNotDowngradeAvailableInitial(t *testing.T) {
	svc, base, _, _, equity := historyFixture()
	t.Setenv("APP_COMMIT", strings.TrimSuffix(cashflowInitialBuild, ":"))
	ctx := context.Background()
	if err := svc.BackfillHistory(ctx); err != nil {
		t.Fatal(err)
	}
	store := &fakeStrategyRevisions{fakeStrategyHistory: base, revisions: map[string]entity.StrategyHistoryRecord{}}
	svc.history = store
	first := equity.list[0]
	equity.list = equity.list[1:]
	if err := svc.BackfillHistory(ctx); err == nil || len(store.revisions) != 0 {
		t.Fatal("temporary missing equity downgraded available initial")
	}
	got, err := svc.StrategyMonitorDate(ctx, "2024-03-01")
	if err != nil || got.Availability != "available" || got.Revision != StrategyInitialRevision {
		t.Fatal("available initial became inaccessible")
	}
	equity.list = append([]entity.EquitySnapshot{first}, equity.list...)
	if err := svc.BackfillHistory(ctx); err != nil || len(store.revisions) != 4 {
		t.Fatalf("retry did not recover revisions: %v", err)
	}
}
