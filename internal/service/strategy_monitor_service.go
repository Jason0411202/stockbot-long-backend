// internal/service/strategy_monitor_service.go 讀取已提交決策快照並轉換交易所顯示單位。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// stateKeyStrategyMonitor 保存單一完整交易日的監控快照。
const stateKeyStrategyMonitor = "strategy_monitor_v1"

// StrategyInitialRevision 與 StrategyCashflowRevision 定義永久可查詢的歷史版號。
const (
	StrategyInitialRevision  = "initial"
	StrategyCashflowRevision = "ledger-cashflow-v2"
)

// StrategyMonitorService 查詢永久決策，並以獨立記憶體引擎背景重建缺少的歷史圖表。
type StrategyMonitorService struct {
	state               StateStore
	history             StrategyHistoryStore
	ledger              LedgerStore
	equity              EquityStore
	series              SeriesLoader
	cfg                 *config.Config
	mu                  sync.RWMutex
	backfill            dto.StrategyBackfillStatus
	runMu               sync.Mutex
	startOnce           sync.Once
	reportBackfillError func(error)
}

// WithBackfillErrorReporter 注入背景錯誤紀錄器，HTTP 回應維持不包含內部錯誤細節。
func (s *StrategyMonitorService) WithBackfillErrorReporter(report func(error)) *StrategyMonitorService {
	s.reportBackfillError = report
	return s
}

// ErrStrategyHistoryNotFound 表示指定日期尚無保存紀錄。
var ErrStrategyHistoryNotFound = errors.New("strategy history not found")

// NewStrategyMonitorService 建立只讀的策略監控查詢服務。
func NewStrategyMonitorService(state StateStore) *StrategyMonitorService {
	return &StrategyMonitorService{state: state, backfill: dto.StrategyBackfillStatus{Status: "pending", Message: "等待自動檢查歷史決策。"}}
}

// WithHistory 注入只讀重建來源與獨立歷史儲存器。
func (s *StrategyMonitorService) WithHistory(history StrategyHistoryStore, ledger LedgerStore, equity EquityStore, series SeriesLoader, cfg *config.Config) *StrategyMonitorService {
	s.history, s.ledger, s.equity, s.series, s.cfg = history, ledger, equity, series, cfg
	return s
}

// StrategyMonitor 回傳最新保存的原始或重建決策，無資料時明確標記不可用。
func (s *StrategyMonitorService) StrategyMonitor(ctx context.Context) (dto.StrategyMonitor, error) {
	// 日期索引直接來自儲存器，最新已保存重建紀錄同樣可以開啟。
	if s.history != nil {
		entries, err := s.history.ListHistory(ctx)
		if err != nil {
			return dto.StrategyMonitor{}, err
		}
		if len(entries) > 0 {
			// 舊部署的最後原始快照可在背景封存完成前立即查看。
			legacy, legacyErr := s.legacyMonitor(ctx)
			if legacyErr == nil && legacy.DecisionDate > entries[0].DecisionDate {
				return legacy, nil
			}
			return s.StrategyMonitorDate(ctx, entries[0].DecisionDate)
		}
	}
	return s.legacyMonitor(ctx)
}

// legacyMonitor 讀取尚未移入歷史表的原始單筆決策。
func (s *StrategyMonitorService) legacyMonitor(ctx context.Context) (dto.StrategyMonitor, error) {
	raw, ok, err := s.state.Get(ctx, stateKeyStrategyMonitor)
	if err != nil {
		return dto.StrategyMonitor{}, fmt.Errorf("read strategy monitor: %w", err)
	}
	if !ok {
		return dto.StrategyMonitor{SchemaVersion: 1, Source: "original", Warnings: []string{}, Availability: "unavailable", Message: "尚未保存決策快照；系統將自動回補可還原的歷史日期。", Stocks: []dto.StrategyMonitorStock{}}, nil
	}

	// 只接受目前版型，缺損資料不會轉成看似正常的空交易訊號。
	var snapshot dto.StrategyMonitor
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return dto.StrategyMonitor{}, fmt.Errorf("decode strategy monitor: %w", err)
	}
	if snapshot.SchemaVersion != 1 || snapshot.Availability != "available" || snapshot.Stocks == nil {
		return dto.StrategyMonitor{}, fmt.Errorf("invalid strategy monitor schema")
	}
	decisionDate, err := time.Parse(dateLayout, snapshot.DecisionDate)
	if err != nil {
		return dto.StrategyMonitor{}, fmt.Errorf("invalid strategy monitor decision date: %w", err)
	}
	if decisionDate.IsZero() {
		return dto.StrategyMonitor{}, fmt.Errorf("missing strategy monitor decision date")
	}
	return snapshot, nil
}

// StrategyMonitorDate 讀取不可變的指定日期快照，不觸發交易或重算條件。
func (s *StrategyMonitorService) StrategyMonitorDate(ctx context.Context, date string) (dto.StrategyMonitor, error) {
	return s.StrategyMonitorRevision(ctx, date, "")
}

// StrategyMonitorRevision 依指定版號讀取快照，省略版號時優先讀取已保存修订。
func (s *StrategyMonitorService) StrategyMonitorRevision(ctx context.Context, date, revision string) (dto.StrategyMonitor, error) {
	if s.history == nil {
		return dto.StrategyMonitor{}, ErrStrategyHistoryNotFound
	}
	var r entity.StrategyHistoryRecord
	var found bool
	var err error
	if revision != StrategyInitialRevision {
		if revisions, ok := s.history.(StrategyRevisionStore); ok {
			r, found, err = revisions.GetRevision(ctx, date, StrategyCashflowRevision)
		}
		if err != nil {
			return dto.StrategyMonitor{}, err
		}
		if !found && revision == StrategyCashflowRevision {
			return dto.StrategyMonitor{}, ErrStrategyHistoryNotFound
		}
	}
	if !found {
		r, found, err = s.history.GetDate(ctx, date)
	}
	if err != nil {
		return dto.StrategyMonitor{}, err
	}
	if !found {
		return dto.StrategyMonitor{}, ErrStrategyHistoryNotFound
	}
	var out dto.StrategyMonitor
	if err := json.Unmarshal([]byte(r.Snapshot), &out); err != nil {
		return out, fmt.Errorf("decode strategy history: %w", err)
	}
	if out.SchemaVersion != 1 || out.DecisionDate != date || out.Stocks == nil || (out.Availability != "available" && out.Availability != "unavailable") {
		return out, fmt.Errorf("invalid strategy history schema")
	}
	if out.Revision == "" {
		out.Revision = StrategyInitialRevision
	}
	return out, nil
}

// StrategyHistory 讀取最新日期索引與背景回補進度。
func (s *StrategyMonitorService) StrategyHistory(ctx context.Context) (dto.StrategyMonitorHistory, error) {
	out := dto.StrategyMonitorHistory{Entries: []dto.StrategyHistoryEntry{}}
	if s.history == nil {
		return out, fmt.Errorf("strategy history not configured")
	}
	rows, err := s.history.ListHistory(ctx)
	if err != nil {
		return out, err
	}
	updated := map[string]entity.StrategyHistoryRecord{}
	if revisions, ok := s.history.(StrategyRevisionStore); ok {
		items, err := revisions.ListRevisions(ctx, StrategyCashflowRevision)
		if err != nil {
			return out, err
		}
		for _, r := range items {
			updated[r.DecisionDate] = r
		}
	}
	// 只回傳列表摘要，選取日期時才傳送完整條件圖資料。
	for _, r := range rows {
		revision := StrategyInitialRevision
		hasInitial := false
		if v, ok := updated[r.DecisionDate]; ok {
			r = v
			revision = StrategyCashflowRevision
			hasInitial = true
		}
		out.Entries = append(out.Entries, dto.StrategyHistoryEntry{Revision: revision, HasInitialRevision: hasInitial, DecisionDate: r.DecisionDate, Source: r.Source, ExecutionMode: r.ExecutionMode, RecordedAt: r.RecordedAt, StrategyVersion: r.StrategyVersion, Availability: r.Availability})
	}
	if len(rows) > 0 {
		out.NewestDate = rows[0].DecisionDate
		out.OldestDate = rows[len(rows)-1].DecisionDate
	}
	s.mu.RLock()
	out.Backfill = s.backfill
	s.mu.RUnlock()
	return out, nil
}

// historyRecord 將整日快照編碼並建立永久索引欄位。
func historyRecord(v dto.StrategyMonitor) (entity.StrategyHistoryRecord, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return entity.StrategyHistoryRecord{}, fmt.Errorf("encode strategy history: %w", err)
	}
	return entity.StrategyHistoryRecord{Revision: v.Revision, DecisionDate: v.DecisionDate, Source: v.Source, ExecutionMode: v.ExecutionMode, RecordedAt: v.RecordedAt, StrategyVersion: v.StrategyVersion, Availability: v.Availability, Snapshot: string(b)}, nil
}

// stampMonitor 保存此次採用的設定、程式版本及公司行動單位依據。
func stampMonitor(out *dto.StrategyMonitor, cfg *config.Config, loader SeriesLoader) {
	out.Source, out.Warnings = "original", []string{}
	out.Revision = StrategyInitialRevision
	out.StrategyConfig, _ = json.Marshal(cfg)
	hash := sha256.Sum256(out.StrategyConfig)
	out.ConfigHash = fmt.Sprintf("%x", hash)
	version := os.Getenv("APP_COMMIT")
	if version == "" {
		version = "dev"
	}
	out.StrategyVersion = version + ":" + out.ConfigHash[:12]
	out.UnitBook, _ = json.Marshal(bookFor(loader))
}

// monitorNumber 轉換價格及股數，金額與百分比保持不變。
func monitorNumber(value *float64, unit string, factor float64) *float64 {
	if value == nil {
		return nil
	}
	n := *value
	if unit == "price" {
		n /= factor
	} else if unit == "shares" {
		n *= factor
	}
	return &n
}

// monitorPhase 將純引擎觀察轉成凍結的 API 資料，不重算門檻是否通過。
func monitorPhase(p trading.DecisionPhase, factor float64) dto.StrategyDecision {
	out := dto.StrategyDecision{Status: p.Status, Reasons: append([]string{}, p.Reasons...), Conditions: []dto.StrategyCondition{}, Metrics: []dto.StrategyMetric{}, IntendedShares: float64(p.IntendedShares) * factor, ExecutedShares: float64(p.ExecutedShares) * factor, CashBefore: p.CashBefore, CashAfter: p.CashAfter, HeldSharesBefore: float64(p.HeldSharesBefore) * factor, HeldSharesAfter: float64(p.HeldSharesAfter) * factor}
	for _, c := range p.Conditions {
		out.Conditions = append(out.Conditions, dto.StrategyCondition{ID: c.ID, Label: c.Label, Status: c.Status, Value: monitorNumber(c.Value, c.Unit, factor), Threshold: monitorNumber(c.Threshold, c.Unit, factor), Operator: c.Operator, Unit: c.Unit, Description: c.Description, RemainingDays: c.RemainingDays, ReleaseDate: c.ReleaseDate})
	}
	for _, m := range p.Metrics {
		out.Metrics = append(out.Metrics, dto.StrategyMetric{ID: m.ID, Label: m.Label, Value: monitorNumber(m.Value, m.Unit, factor), Unit: m.Unit, Description: m.Description})
	}
	return out
}

// monitorSnapshot 在提交時固定所有顯示值及其時間基準。
func (s *TradingService) monitorSnapshot(day time.Time, observations []trading.DecisionObservation, live bool) dto.StrategyMonitor {
	mode := "catchup"
	if live {
		mode = "live"
	}
	rule := "使用決策當日及之前的收盤資料。"
	if s.cfg.DecisionPriceBasis == "open" {
		rule = "以當日開盤價決策；指標只使用前一交易日及之前的收盤資料。"
	}
	out := dto.StrategyMonitor{SchemaVersion: 1, Availability: "available", Message: "最後一次已提交的實際決策快照；各條件及冷卻天數固定於該決策日。", DecisionDate: day.Format(dateLayout), RecordedAt: time.Now().UTC().Format(time.RFC3339), DecisionPriceBasis: s.cfg.DecisionPriceBasis, ExecutionMode: mode, IndicatorRule: rule, Stocks: []dto.StrategyMonitorStock{}}
	stampMonitor(&out, s.cfg, s.series)
	for _, o := range observations {
		factor := displayFactor(s.series, o.StockID, day.Format(dateLayout))
		indicatorDate := ""
		if !o.IndicatorDate.IsZero() {
			indicatorDate = o.IndicatorDate.Format(dateLayout)
		}
		out.Stocks = append(out.Stocks, dto.StrategyMonitorStock{StockID: o.StockID, Status: o.Status, DecisionPrice: monitorNumber(o.Price, "price", factor), IndicatorDate: indicatorDate, Regime: o.Regime, RegimeMethod: o.RegimeMethod, RegimeWindow: o.RegimeWindow, RegimeValue: monitorNumber(o.RegimeValue, "price", factor), RegimeBase: monitorNumber(o.RegimeBase, "price", factor), Buy: monitorPhase(o.Buy, factor), Sell: monitorPhase(o.Sell, factor)})
	}
	return out
}
