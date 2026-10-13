// internal/service/strategy_monitor_service.go 讀取已提交決策快照並轉換交易所顯示單位。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// stateKeyStrategyMonitor 保存單一完整交易日的監控快照。
const stateKeyStrategyMonitor = "strategy_monitor_v1"

// StrategyMonitorService 僅讀取持久化快照，不持有或操作交易引擎。
type StrategyMonitorService struct{ state StateStore }

// NewStrategyMonitorService 建立只讀的策略監控查詢服務。
func NewStrategyMonitorService(state StateStore) *StrategyMonitorService {
	return &StrategyMonitorService{state: state}
}

// StrategyMonitor 回傳最後一次成功提交的真實決策；舊資料缺失時明確標記不可用。
func (s *StrategyMonitorService) StrategyMonitor(ctx context.Context) (dto.StrategyMonitor, error) {
	raw, ok, err := s.state.Get(ctx, stateKeyStrategyMonitor)
	if err != nil {
		return dto.StrategyMonitor{}, fmt.Errorf("read strategy monitor: %w", err)
	}
	if !ok {
		return dto.StrategyMonitor{SchemaVersion: 1, Availability: "unavailable", Message: "尚未保存實際決策快照；下一次交易日決策提交後即可顯示。", Stocks: []dto.StrategyMonitorStock{}}, nil
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
