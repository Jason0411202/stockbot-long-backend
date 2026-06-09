// internal/service/performance_history_service.go 組裝統一日期時間軸的策略績效歷史 API 回應
// (回測曲線 + 實盤每日快照對齊同一條日期軸,逐日衍生倍數/報酬/回撤/CAGR,等距取樣後回傳)。
package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
)

// minCAGRYears 為計算年化報酬 (CAGR) 的最短基期;短於此 (約 3 個月) 年化會被極端放大、不可靠,故回 null。
const minCAGRYears = 0.25

// PerformanceHistoryService 產生「回測 + 實盤」對齊同一日期軸的逐日績效序列。
// 日期軸取自全期回測 (EvaluateFullSpan 的逐日權益曲線,涵蓋共同上市日 ~ 今天);
// 實盤每日快照 (EquityHistory) 依日期對齊到該軸,go-live 之前的日期實盤欄位為 null。
type PerformanceHistoryService struct {
	cfg    *config.Config
	series SeriesLoader
	equity equityHistoryReader
	log    *logrus.Logger
}

// NewPerformanceHistoryService 建立並回傳一個已完成依賴注入的 PerformanceHistoryService。
func NewPerformanceHistoryService(cfg *config.Config, series SeriesLoader, equity equityHistoryReader, log *logrus.Logger) *PerformanceHistoryService {
	return &PerformanceHistoryService{cfg: cfg, series: series, equity: equity, log: log}
}

// History 跑全期回測取得逐日權益曲線、讀取實盤快照,組裝成統一日期軸序列 (等距取樣 <=400 點)。
// 序列載入或回測失敗時回傳錯誤;實盤讀取失敗亦回傳錯誤 (由 controller 決定對外寬鬆契約)。
func (s *PerformanceHistoryService) History(ctx context.Context) ([]dto.PerformanceHistoryPoint, error) {
	series, err := LoadTradingSeries(ctx, s.series, s.cfg.TrackStocks)
	if err != nil {
		return nil, fmt.Errorf("load series: %w", err)
	}
	if len(series) == 0 {
		return []dto.PerformanceHistoryPoint{}, nil
	}
	full, err := backtest.EvaluateFullSpan(s.cfg, series)
	if err != nil {
		return nil, fmt.Errorf("full span: %w", err)
	}
	snaps, err := s.equity.ListEquityAsc(ctx)
	if err != nil {
		return nil, fmt.Errorf("list equity history: %w", err)
	}
	return buildPerformanceHistory(s.cfg, full, snaps), nil
}

// buildPerformanceHistory 把全期回測曲線與實盤快照組成統一日期軸的逐日點,計算後等距取樣 (<=400 點)。
// 回測欄位每日皆有;實盤欄位依日期對齊,缺對應快照的日期 (go-live 前) 留 nil → JSON null。
func buildPerformanceHistory(cfg *config.Config, full backtest.WindowReport, snaps []entity.EquitySnapshot) []dto.PerformanceHistoryPoint {
	dates, strat, bh := full.Dates, full.StratCurve, full.BHCurve
	n := len(dates)
	if n == 0 || len(strat) != n || len(bh) != n {
		return []dto.PerformanceHistoryPoint{}
	}

	// 逐日投入本金 = 期初 + 截至當日累計注資 (lump-sum 時注資全為 0,investe 為常數)。
	contrib := backtest.ContributionAmounts(dates, cfg.MonthlyContribution)
	invested := make([]float64, n)
	cum := cfg.InitialCash
	for i := 0; i < n; i++ {
		cum += contrib[i]
		invested[i] = cum
	}

	// 將實盤快照建成 date → snapshot 索引,供日期軸對齊查詢。
	snapByDate := make(map[string]entity.EquitySnapshot, len(snaps))
	for _, sp := range snaps {
		snapByDate[sp.Date] = sp
	}

	// 逐日組裝:回測指標 (含累進高點回撤) + 對齊的實盤指標 (含上線以來累進高點回撤)。
	pts := make([]dto.PerformanceHistoryPoint, n)
	stratPeak, bhPeak := strat[0], bh[0]
	var liveStart time.Time
	var livePeak float64
	haveLive := false
	for i := 0; i < n; i++ {
		if strat[i] > stratPeak {
			stratPeak = strat[i]
		}
		if bh[i] > bhPeak {
			bhPeak = bh[i]
		}
		inv := invested[i]
		years := dates[i].Sub(dates[0]).Hours() / (24 * 365)

		p := dto.PerformanceHistoryPoint{
			Date:            dates[i].Format(dateLayout),
			Invested:        round2(inv),
			StratEquity:     round2(strat[i]),
			BHEquity:        round2(bh[i]),
			StratMultiple:   round2(safeRatio(strat[i], inv)),
			BHMultiple:      round2(safeRatio(bh[i], inv)),
			StratReturnRate: round2(returnPct(strat[i], inv)),
			BHReturnRate:    round2(returnPct(bh[i], inv)),
			StratDrawdown:   round2(drawdownPct(strat[i], stratPeak)),
			BHDrawdown:      round2(drawdownPct(bh[i], bhPeak)),
			StratCAGR:       cagrPctPtr(strat[i], inv, years),
		}

		// 對齊實盤快照:存在才填實盤欄位,並維護「上線以來」高點與起算日。
		if sp, ok := snapByDate[p.Date]; ok {
			if !haveLive {
				haveLive = true
				liveStart = dates[i]
				livePeak = sp.TotalEquity
			}
			if sp.TotalEquity > livePeak {
				livePeak = sp.TotalEquity
			}
			yearsLive := dates[i].Sub(liveStart).Hours() / (24 * 365)
			unreal := sp.HoldingValue - sp.CostBasis
			totalPnL := sp.TotalEquity - inv

			p.Cash = numPtr(sp.Cash)
			p.HoldingValue = numPtr(sp.HoldingValue)
			p.TotalEquity = numPtr(sp.TotalEquity)
			p.HoldingRatio = ratioPtr(pctOf(sp.HoldingValue, sp.TotalEquity))
			p.CashRatio = ratioPtr(pctOf(sp.Cash, sp.TotalEquity))
			p.TotalPnL = numPtr(totalPnL)
			p.TotalReturnRate = ratioPtr(returnPct(sp.TotalEquity, inv))
			p.Multiple = ratioPtr(safeRatio(sp.TotalEquity, inv))
			p.RealizedPnL = numPtr(totalPnL - unreal)
			p.UnrealizedPnL = numPtr(unreal)
			p.CAGR = cagrPctPtr(sp.TotalEquity, inv, yearsLive)
			p.MaxDrawdown = ratioPtr(drawdownPct(sp.TotalEquity, livePeak))
		}
		pts[i] = p
	}

	return downsampleHistory(pts)
}

// downsampleHistory 對逐日點等距取樣至 <=maxEquityCurvePoints 點;末點 (最新一日) 必定保留。
func downsampleHistory(pts []dto.PerformanceHistoryPoint) []dto.PerformanceHistoryPoint {
	n := len(pts)
	if n == 0 {
		return []dto.PerformanceHistoryPoint{}
	}
	stride := sampleStride(n, maxEquityCurvePoints)
	out := make([]dto.PerformanceHistoryPoint, 0, n/stride+1)
	for i := 0; i < n; i += stride {
		out = append(out, pts[i])
	}
	if last := n - 1; last%stride != 0 {
		out = append(out, pts[last])
	}
	return out
}

// safeRatio 回傳 a/b;b<=0 回傳 0。
func safeRatio(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return a / b
}

// returnPct 回傳 (equity−invested)/invested 的百分比;invested<=0 回傳 0。
func returnPct(equity, invested float64) float64 {
	if invested <= 0 {
		return 0
	}
	return (equity - invested) / invested * 100
}

// pctOf 回傳 part/total 的百分比;total<=0 回傳 NaN (交由 ratioPtr 降為 null)。
func pctOf(part, total float64) float64 {
	if total <= 0 {
		return math.NaN()
	}
	return part / total * 100
}

// drawdownPct 回傳 value 相對 peak 的回撤百分比 (<=0);peak<=0 回傳 0。
func drawdownPct(value, peak float64) float64 {
	if peak <= 0 {
		return 0
	}
	return (value/peak - 1) * 100
}

// cagrPctPtr 回傳年化報酬 (%) 指標;基期過短 / 基準非正 / 結果非有限時回 nil (→ JSON null)。
func cagrPctPtr(end, start, years float64) *float64 {
	if start <= 0 || end <= 0 || years < minCAGRYears {
		return nil
	}
	v := (math.Pow(end/start, 1.0/years) - 1) * 100
	return ratioPtr(v)
}

// numPtr 回傳指向四捨五入 2 位金額的指標 (供實盤 *float64 欄位;假設值為有限)。
func numPtr(x float64) *float64 {
	v := round2(x)
	return &v
}

// ratioPtr 回傳指向四捨五入 2 位比率的指標;NaN/±Inf 回 nil (→ JSON null,避免 encoding/json 編碼錯誤)。
func ratioPtr(x float64) *float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return nil
	}
	v := round2(x)
	return &v
}
