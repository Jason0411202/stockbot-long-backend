// internal/controller/strategy_history_test.go 驗證決策日期參數、查無資料與歷史索引傳輸。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/service"
)

// historyReporterFake 提供可記錄指定日期的歷史查詢服務。
type historyReporterFake struct {
	date     string
	err      error
	revision string
}

// StrategyMonitorRevision 記錄要求的永久版次。
func (f *historyReporterFake) StrategyMonitorRevision(ctx context.Context, date, revision string) (dto.StrategyMonitor, error) {
	f.revision = revision
	v, err := f.StrategyMonitorDate(ctx, date)
	v.Revision = revision
	return v, err
}

// TestStrategyHistoryController_RevisionSelection 驗證版號與日期搭配及未知版次400。
func TestStrategyHistoryController_RevisionSelection(t *testing.T) {
	for _, tc := range []struct {
		query, revision string
		status          int
	}{{"?date=2023-08-14&revision=initial", "initial", 200}, {"?date=2023-08-14&revision=ledger-cashflow-v2", "ledger-cashflow-v2", 200}, {"?revision=initial", "", 400}, {"?date=2023-08-14&revision=unknown", "", 400}, {"?date=2023-08-14&revision=", "", 400}} {
		f := &historyReporterFake{}
		ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}).WithStrategyMonitor(f)
		rec := invoke(t, ctl.StrategyMonitor, "/api/get_strategy_monitor"+tc.query)
		if rec.Code != tc.status || f.revision != tc.revision {
			t.Fatalf("%s status=%d revision=%s", tc.query, rec.Code, f.revision)
		}
	}
}

// StrategyMonitor 回傳最新決策。
func (f *historyReporterFake) StrategyMonitor(context.Context) (dto.StrategyMonitor, error) {
	return dto.StrategyMonitor{DecisionDate: "2024-03-02"}, f.err
}

// StrategyMonitorDate 記錄指定日期並提供可查閱的不可用歷史紀錄。
func (f *historyReporterFake) StrategyMonitorDate(_ context.Context, date string) (dto.StrategyMonitor, error) {
	f.date = date
	return dto.StrategyMonitor{SchemaVersion: 1, DecisionDate: date, Source: "reconstructed", Availability: "unavailable", Stocks: []dto.StrategyMonitorStock{}, Warnings: []string{"歷史現金缺少"}}, f.err
}

// StrategyHistory 回傳日期索引及回補進度。
func (f *historyReporterFake) StrategyHistory(context.Context) (dto.StrategyMonitorHistory, error) {
	return dto.StrategyMonitorHistory{Entries: []dto.StrategyHistoryEntry{{DecisionDate: "2024-03-02", Source: "original"}}, NewestDate: "2024-03-02", OldestDate: "2024-03-02", Backfill: dto.StrategyBackfillStatus{Status: "running", Completed: 3, Total: 10}}, f.err
}

// TestStrategyHistoryController_DateValidationAndStatus 驗證日期格式、404、503以及不可用快照仍可正常查看。
func TestStrategyHistoryController_DateValidationAndStatus(t *testing.T) {
	for _, tc := range []struct {
		query  string
		err    error
		status int
	}{{"?date=2024-03-01", nil, 200}, {"?date=", nil, 400}, {"?date=2024-02-30", nil, 400}, {"?date=24-03-01", nil, 400}, {"?date=2024-03-01", service.ErrStrategyHistoryNotFound, 404}, {"?date=2024-03-01", errors.New("private connection detail"), 503}} {
		t.Run(tc.query+http.StatusText(tc.status), func(t *testing.T) {
			f := &historyReporterFake{err: tc.err}
			ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}).WithStrategyMonitor(f)
			rec := invoke(t, ctl.StrategyMonitor, "/api/get_strategy_monitor"+tc.query)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.status == 400 && f.date != "" {
				t.Fatal("invalid date reached service")
			}
			if tc.status == 200 {
				var got dto.StrategyMonitor
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Availability != "unavailable" || got.DecisionDate != "2024-03-01" {
					t.Fatalf("unavailable record lost: %+v %v", got, err)
				}
			}
		})
	}
}

// TestStrategyHistoryController_IndexAndUnavailableDependency 驗證索引包含進度且相依服務缺失回503。
func TestStrategyHistoryController_IndexAndUnavailableDependency(t *testing.T) {
	ctl := newController(fakePortfolio{}, fakeStatistic{}, fakeHistory{}, fakePerformance{}).WithStrategyMonitor(&historyReporterFake{})
	rec := invoke(t, ctl.StrategyMonitorHistory, "/api/get_strategy_monitor_history")
	var got dto.StrategyMonitorHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 || got.Backfill.Total != 10 || len(got.Entries) != 1 {
		t.Fatalf("index=%+v status=%d err=%v", got, rec.Code, err)
	}
	ctl.monitor = nil
	rec = invoke(t, ctl.StrategyMonitorHistory, "/api/get_strategy_monitor_history")
	if rec.Code != 503 {
		t.Fatal("missing history dependency accepted")
	}
}
