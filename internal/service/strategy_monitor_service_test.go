// internal/service/strategy_monitor_service_test.go 驗證監控快照讀取、格式驗證與跨重啟資料一致性。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
)

// storedMonitorFixture 建立包含冷卻、回落門檻與交易前後數值的固定歷史快照。
func storedMonitorFixture() dto.StrategyMonitor {
	price, drawdown, threshold := 97.0, 3.0, 10.0
	remaining := 7
	return dto.StrategyMonitor{
		SchemaVersion:      1,
		Availability:       "available",
		DecisionDate:       "2024-03-01",
		RecordedAt:         "2024-03-01T09:05:00+08:00",
		DecisionPriceBasis: "open",
		ExecutionMode:      "live",
		IndicatorRule:      "prior_session",
		Stocks: []dto.StrategyMonitorStock{{
			StockID:       "AAA",
			Status:        "evaluated",
			DecisionPrice: &price,
			IndicatorDate: "2024-02-29",
			Buy: dto.StrategyDecision{
				Status:  "blocked",
				Reasons: []string{"cooldown"},
				Conditions: []dto.StrategyCondition{{
					ID: "buy_cooldown", Status: "blocked", RemainingDays: &remaining,
					ReleaseDate: "2024-03-08", Unit: "days", Operator: "gte",
				}},
				CashBefore: 1234, CashAfter: 1234,
			},
			Sell: dto.StrategyDecision{
				Status: "blocked",
				Conditions: []dto.StrategyCondition{{
					ID: "trailing_drawdown", Status: "blocked", Value: &drawdown,
					Threshold: &threshold, Unit: "percent", Operator: "gte",
				}},
				HeldSharesBefore: 250, HeldSharesAfter: 250,
			},
		}},
	}
}

// encodeStoredMonitor 將測試快照編碼成 BotState 使用的 JSON 字串，編碼失敗即中止測試。
func encodeStoredMonitor(t *testing.T, snapshot dto.StrategyMonitor) string {
	t.Helper()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return string(encoded)
}

// TestStrategyMonitorService_MissingSnapshotIsUnavailable 驗證舊部署缺少快照時回傳不可用狀態及空陣列。
func TestStrategyMonitorService_MissingSnapshotIsUnavailable(t *testing.T) {
	state := newFakeState()
	got, err := NewStrategyMonitorService(state).StrategyMonitor(context.Background())
	if err != nil {
		t.Fatalf("missing snapshot: %v", err)
	}
	if got.SchemaVersion != 1 || got.Availability != "unavailable" || got.Message == "" {
		t.Fatalf("missing snapshot presented as valid: %#v", got)
	}
	if got.Stocks == nil || len(got.Stocks) != 0 || got.DecisionDate != "" {
		t.Fatalf("missing snapshot must contain an empty array and no decision date: %#v", got)
	}
	if len(state.setKeys) != 0 || len(state.values) != 0 {
		t.Fatal("reading an absent snapshot wrote state")
	}
}

// TestStrategyMonitorService_RejectsCorruptAndUnsupportedSnapshots 驗證損毀、未知版本及缺損快照不會被當成有效決策。
func TestStrategyMonitorService_RejectsCorruptAndUnsupportedSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*dto.StrategyMonitor)
		raw    string
	}{
		{name: "malformed JSON", raw: "{"},
		{name: "JSON null", raw: "null"},
		{name: "unsupported version", mutate: func(s *dto.StrategyMonitor) { s.SchemaVersion = 2 }},
		{name: "missing version", mutate: func(s *dto.StrategyMonitor) { s.SchemaVersion = 0 }},
		{name: "unavailable persisted snapshot", mutate: func(s *dto.StrategyMonitor) { s.Availability = "unavailable" }},
		{name: "missing availability", mutate: func(s *dto.StrategyMonitor) { s.Availability = "" }},
		{name: "missing decision date", mutate: func(s *dto.StrategyMonitor) { s.DecisionDate = "" }},
		{name: "invalid decision date", mutate: func(s *dto.StrategyMonitor) { s.DecisionDate = "2024-02-30" }},
		{name: "wrong date format", mutate: func(s *dto.StrategyMonitor) { s.DecisionDate = "03/01/2024" }},
		{name: "zero decision date", mutate: func(s *dto.StrategyMonitor) { s.DecisionDate = "0001-01-01" }},
		{name: "null stocks", mutate: func(s *dto.StrategyMonitor) { s.Stocks = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := newFakeState()
			raw := tc.raw
			if tc.mutate != nil {
				snapshot := storedMonitorFixture()
				tc.mutate(&snapshot)
				raw = encodeStoredMonitor(t, snapshot)
			}
			state.values[stateKeyStrategyMonitor] = raw
			if _, err := NewStrategyMonitorService(state).StrategyMonitor(context.Background()); err == nil {
				t.Fatal("invalid stored snapshot accepted")
			}
			if len(state.setKeys) != 0 || state.values[stateKeyStrategyMonitor] != raw {
				t.Fatal("reading an invalid snapshot rewrote stored state")
			}
		})
	}
}

// TestStrategyMonitorService_PropagatesStoreError 驗證儲存讀取錯誤保留原始錯誤鏈。
func TestStrategyMonitorService_PropagatesStoreError(t *testing.T) {
	state := newFakeState()
	state.getErr = errFake
	_, err := NewStrategyMonitorService(state).StrategyMonitor(context.Background())
	if !errors.Is(err, errFake) {
		t.Fatalf("storage error = %v, want %v", err, errFake)
	}
}

// TestStrategyMonitorService_PreservesStoredDecisionAcrossReadsAndRestart 驗證歷史數值不重算且重讀、重啟均無寫入副作用。
func TestStrategyMonitorService_PreservesStoredDecisionAcrossReadsAndRestart(t *testing.T) {
	want := storedMonitorFixture()
	state := newFakeState()
	state.values[stateKeyStrategyMonitor] = encodeStoredMonitor(t, want)
	state.values[stateKeyCash] = "999999"
	before := copyValues(state.values)
	service := NewStrategyMonitorService(state)
	got, err := service.StrategyMonitor(context.Background())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("stored decision was recomputed or changed: got %#v, err %v", got, err)
	}
	// 呼叫者修改回傳 DTO 不得影響後續讀取。
	*got.Stocks[0].Buy.Conditions[0].RemainingDays = 0
	got.Stocks[0].Sell.Conditions[0].Threshold = nil
	again, err := service.StrategyMonitor(context.Background())
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("subsequent read changed persisted decision: got %#v, err %v", again, err)
	}
	// 以新的服務及儲存實例模擬程序重啟；歷史冷卻天數與門檻必須維持不變。
	restoredState := newFakeState()
	restoredState.values = copyValues(state.values)
	restarted, err := NewStrategyMonitorService(restoredState).StrategyMonitor(context.Background())
	if err != nil || !reflect.DeepEqual(restarted, want) {
		t.Fatalf("restart lost decision snapshot: got %#v, err %v", restarted, err)
	}
	if len(state.setKeys) != 0 || len(restoredState.setKeys) != 0 || !reflect.DeepEqual(state.values, before) {
		t.Fatal("monitor read mutated trading state")
	}
}
