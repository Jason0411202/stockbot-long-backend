// internal/service/strategy_history_revision.go 以初版規則追加帳本金額精度修訂並永久保留初版。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// cashflowInitialBuild 指定使用價格乘股數重建現金的初版部署，其他既有資料不參與修訂。
const cashflowInitialBuild = "228990cff58f5c8f0a7675ec5d877832f605b2b9:"

// cashflowRevisionInput 凍結初版設定及單位簿對應的行情。
type cashflowRevisionInput struct {
	initial dto.StrategyMonitor
	cfg     *config.Config
	loader  *frozenHistorySeries
	series  map[string]*trading.StockSeries
}

// prepareCashflowRevisions 只挑選指定部署生成的重建初版，不更動任何已存在版次。
func (s *StrategyMonitorService) prepareCashflowRevisions(ctx context.Context, rows []entity.StrategyHistoryRecord, in historyInputs) (map[string]cashflowRevisionInput, error) {
	plans := map[string]cashflowRevisionInput{}
	store, ok := s.history.(StrategyRevisionStore)
	if !ok {
		return plans, nil
	}
	revised, err := store.ListRevisions(ctx, StrategyCashflowRevision)
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for _, r := range revised {
		existing[r.DecisionDate] = true
	}
	cache := map[string]cashflowRevisionInput{}
	// 初版不變，僅缺少修訂且來源與部署版本吻合的日期需要載入本文。
	for _, row := range rows {
		if row.Source != "reconstructed" || !strings.HasPrefix(row.StrategyVersion, cashflowInitialBuild) || existing[row.DecisionDate] || row.DecisionDate > in.watermark {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		base, found, err := s.history.GetDate(ctx, row.DecisionDate)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("missing immutable initial history %s", row.DecisionDate)
		}
		var initial dto.StrategyMonitor
		if err := json.Unmarshal([]byte(base.Snapshot), &initial); err != nil {
			return nil, fmt.Errorf("decode initial revision %s: %w", row.DecisionDate, err)
		}
		if initial.DecisionDate != row.DecisionDate || initial.Source != "reconstructed" || initial.StrategyVersion != row.StrategyVersion {
			return nil, fmt.Errorf("initial revision metadata mismatch %s", row.DecisionDate)
		}
		// 修訂只需要初版的溯源欄位，條件本文持續保存在初版表。
		initial.Stocks = nil
		initial.Warnings = nil
		key := string(initial.StrategyConfig) + "/" + string(initial.UnitBook)
		plan, cached := cache[key]
		if !cached {
			var cfg config.Config
			var book marketunits.Book
			if err := json.Unmarshal(initial.StrategyConfig, &cfg); err != nil || len(cfg.TrackStocks) == 0 {
				return nil, fmt.Errorf("initial strategy configuration unavailable %s", row.DecisionDate)
			}
			hash := sha256.Sum256(initial.StrategyConfig)
			if initial.ConfigHash == "" || fmt.Sprintf("%x", hash) != initial.ConfigHash {
				return nil, fmt.Errorf("initial strategy configuration hash mismatch %s", row.DecisionDate)
			}
			if err := json.Unmarshal(initial.UnitBook, &book); err != nil {
				return nil, fmt.Errorf("decode initial unit book: %w", err)
			}
			if err := book.Validate(); err != nil {
				return nil, fmt.Errorf("validate initial unit book: %w", err)
			}
			loader, err := s.revisionSeries(ctx, in.series, book, cfg.TrackStocks)
			if err != nil {
				return nil, err
			}
			series, err := LoadTradingSeries(ctx, loader, cfg.TrackStocks)
			if err != nil {
				return nil, err
			}
			plan = cashflowRevisionInput{cfg: &cfg, loader: loader, series: series}
			cache[key] = plan
		}
		plan.initial = initial
		plans[row.DecisionDate] = plan
	}
	return plans, nil
}

// revisionSeries 以初版單位簿轉換正規化行情，未追蹤的舊股票另行唯讀載入。
func (s *StrategyMonitorService) revisionSeries(ctx context.Context, source *frozenHistorySeries, book marketunits.Book, ids []string) (*frozenHistorySeries, error) {
	rows := map[string][]entity.StockHistory{}
	missing := []string{}
	for _, id := range ids {
		if _, ok := source.rows[id]; !ok {
			missing = append(missing, id)
		}
	}
	extra := map[string][]entity.StockHistory{}
	if len(missing) > 0 {
		before := bookFor(s.series)
		var err error
		extra, err = s.series.LoadSeries(ctx, missing)
		if err != nil {
			return nil, fmt.Errorf("load initial tracked stocks: %w", err)
		}
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(bookFor(s.series))
		capturedJSON, _ := json.Marshal(source.book)
		if string(beforeJSON) != string(afterJSON) || string(beforeJSON) != string(capturedJSON) {
			return nil, fmt.Errorf("market unit book changed while reading revision inputs")
		}
	}
	// 複製價格切片，所有計算均維持初版的價格與股數基準。
	for _, id := range ids {
		input := source.rows[id]
		if _, ok := source.rows[id]; !ok {
			input = extra[id]
		}
		for _, row := range input {
			day, err := parseLedgerDate(row.Date)
			if err != nil {
				return nil, fmt.Errorf("invalid revision price date: %w", err)
			}
			date := day.Format(dateLayout)
			factor := book.Factor(id, date) / source.book.Factor(id, date)
			row.OpenPrice *= factor
			row.ClosePrice *= factor
			rows[id] = append(rows[id], row)
		}
	}
	return &frozenHistorySeries{rows: rows, book: book}, nil
}

// appendCashflowRevision 只追加新版本文，初版規則、日期及來源永久可追溯。
func (s *StrategyMonitorService) appendCashflowRevision(ctx context.Context, date string, in historyInputs, lots []historicalLot, trades map[string][]trading.HistoricalTrade, plan cashflowRevisionInput) error {
	reader := &StrategyMonitorService{cfg: plan.cfg}
	in.series = plan.loader
	out := reader.reconstructHistory(date, in, plan.series, lots, trades, nil, nil)
	if plan.initial.Availability == "available" && out.Availability != "available" {
		return fmt.Errorf("cashflow revision %s would make an available initial snapshot unavailable: %s", date, out.Message)
	}
	out.Revision = StrategyCashflowRevision
	out.SupersedesRevision = StrategyInitialRevision
	out.ReconstructionBaseVersion = plan.initial.StrategyVersion
	out.ConfigHash = plan.initial.ConfigHash
	out.StrategyConfig = append(json.RawMessage(nil), plan.initial.StrategyConfig...)
	out.UnitBook = append(json.RawMessage(nil), plan.initial.UnitBook...)
	for i, warning := range out.Warnings {
		warning = strings.ReplaceAll(warning, "回補時的目前規則", "初版保存的策略規則")
		warning = strings.ReplaceAll(warning, "目前規則的預期", "初版規則的預期")
		out.Warnings[i] = strings.ReplaceAll(warning, "股票處理順序採目前設定", "股票處理順序沿用初版設定")
	}
	out.Warnings = append(out.Warnings, "現金以帳本保存的買入成本及出售收入重建，避免歷史成交價取位精度改變現金；初版保留，可切換查看。")
	if out.Availability == "available" {
		out.Message = "沿用初版策略與單位設定，以帳本實際成交金額重建的現金精度修訂。"
	}
	record, err := historyRecord(out)
	if err != nil {
		return err
	}
	store, ok := s.history.(StrategyRevisionStore)
	if !ok {
		return fmt.Errorf("strategy revision store unavailable")
	}
	return store.InsertRevision(ctx, record)
}
