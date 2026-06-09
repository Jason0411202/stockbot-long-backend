// internal/service/performance_service.go 組裝策略績效摘要 API 回應 (本金明細 + 實盤現況 + 回測指標)。
package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
)

// portfolioReader 是 PerformanceService 取得實盤未實現 / 已實現損益所需的最小讀取介面,由 *PortfolioService 實作。
type portfolioReader interface {
	UnrealizedGainsLosses(ctx context.Context) ([]dto.UnrealizedGainLoss, error)
	RealizedGainsLosses(ctx context.Context) ([]dto.RealizedGainLoss, error)
}

// PerformanceService 匯整三類資訊供前端一次取得：
//   - 本金明細：期初現金 + 累計注資 (BotState) = 投入本金 (外部注入,不含滾出的獲利)。
//   - 實盤現況：從帳本與 BotState 算目前現金、持股市值、總權益、已 / 未實現損益、總報酬率。
//   - 回測績效：以 config 期初本金 + 每月注資跑全期 + walk-forward,輸出策略 vs B&H 指標與五道關卡。
//
// 本金 / 實盤區塊永遠回傳 (來源為帳本與 BotState,成本低);回測區塊為 best-effort,
// 載入序列或評估失敗時記錄錯誤並回傳 nil (前端見 backtest=null),不影響本金 / 實盤資料。
type PerformanceService struct {
	cfg       *config.Config
	portfolio portfolioReader
	state     StateStore
	series    SeriesLoader
	log       *logrus.Logger
}

// NewPerformanceService 建立並回傳一個已完成依賴注入的 PerformanceService。
func NewPerformanceService(cfg *config.Config, portfolio portfolioReader, state StateStore, series SeriesLoader, log *logrus.Logger) *PerformanceService {
	return &PerformanceService{cfg: cfg, portfolio: portfolio, state: state, series: series, log: log}
}

// round4 將浮點數四捨五入至小數點後四位 (供報酬率 / 回撤 / 比率使用);NaN/Inf 原樣通過 (交由 JSONFloat 降級為 null)。
func round4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}

// metric 將回測比率指標轉為可安全序列化的 JSONFloat (先 round4,NaN/Inf 由 JSONFloat 編成 null)。
func metric(x float64) dto.JSONFloat {
	return dto.JSONFloat(round4(x))
}

// stateFloat 讀取 BotState 的數值鍵;鍵不存在回傳 fallback,解析失敗回傳 fallback + 錯誤。
func stateFloat(ctx context.Context, store StateStore, key string, fallback float64) (float64, error) {
	v, ok, err := store.Get(ctx, key)
	if err != nil {
		return fallback, err
	}
	if !ok {
		return fallback, nil
	}
	f, perr := strconv.ParseFloat(v, 64)
	if perr != nil {
		return fallback, fmt.Errorf("parse state %q=%q: %w", key, v, perr)
	}
	return f, nil
}

// Summary 組裝完整績效摘要:本金明細 + 實盤現況 (必有) + 回測績效 (best-effort)。
// 任一實盤資料來源 (帳本 / BotState) 失敗時回傳錯誤;回測失敗僅記錄,Backtest 欄位留 nil。
func (s *PerformanceService) Summary(ctx context.Context) (dto.PerformanceSummary, error) {
	// 讀取累計注資與目前現金 (現金缺紀錄時退回期初現金,對應首次啟動尚未持久化)。
	totalContributed, err := stateFloat(ctx, s.state, stateKeyTotalContributed, 0)
	if err != nil {
		return dto.PerformanceSummary{}, fmt.Errorf("load total_contributed: %w", err)
	}
	currentCash, err := stateFloat(ctx, s.state, stateKeyCash, s.cfg.InitialCash)
	if err != nil {
		return dto.PerformanceSummary{}, fmt.Errorf("load current_cash: %w", err)
	}

	// 取得實盤未實現持倉 (含即時價) 與已實現損益,彙總持股市值與損益。
	unreal, err := s.portfolio.UnrealizedGainsLosses(ctx)
	if err != nil {
		return dto.PerformanceSummary{}, fmt.Errorf("unrealized gains losses: %w", err)
	}
	realized, err := s.portfolio.RealizedGainsLosses(ctx)
	if err != nil {
		return dto.PerformanceSummary{}, fmt.Errorf("realized gains losses: %w", err)
	}
	holdingValue, unrealizedPnL := 0.0, 0.0
	for _, u := range unreal {
		holdingValue += u.NowValue
		unrealizedPnL += u.PredictProfitLoss
	}
	realizedPnL := 0.0
	for _, r := range realized {
		realizedPnL += r.ProfitLoss
	}

	// 由本金與現況推導投入本金、總權益、總損益與總報酬率。
	totalInvested := s.cfg.InitialCash + totalContributed
	totalEquity := currentCash + holdingValue
	totalPnL := totalEquity - totalInvested
	returnRate := 0.0
	if totalInvested > 0 {
		returnRate = totalPnL / totalInvested * 100
	}

	// 計算資產配置比例:持股與預備現金各佔總權益的百分比 (總權益<=0 時兩者皆 0)。
	holdingRatio, cashRatio := 0.0, 0.0
	if totalEquity > 0 {
		holdingRatio = holdingValue / totalEquity * 100
		cashRatio = currentCash / totalEquity * 100
	}

	// 組裝摘要 (金額兩位小數),回測區塊以 best-effort 附加。
	return dto.PerformanceSummary{
		InitialCash:         s.cfg.InitialCash,
		MonthlyContribution: s.cfg.MonthlyContribution,
		TotalContributed:    round2(totalContributed),
		TotalInvested:       round2(totalInvested),
		CurrentCash:         round2(currentCash),
		HoldingValue:        round2(holdingValue),
		TotalEquity:         round2(totalEquity),
		HoldingRatio:        round2(holdingRatio),
		CashRatio:           round2(cashRatio),
		RealizedPnL:         round2(realizedPnL),
		UnrealizedPnL:       round2(unrealizedPnL),
		TotalPnL:            round2(totalPnL),
		TotalReturnRate:     round2(returnRate),
		Backtest:            s.backtestPerformance(ctx),
	}, nil
}

// backtestPerformance 載入序列並跑全期 + walk-forward 評估,組成回測 DTO。
// 任一步失敗時記錄錯誤並回傳 nil (best-effort),使本金 / 實盤資料仍可正常回傳。
func (s *PerformanceService) backtestPerformance(ctx context.Context) *dto.BacktestPerformance {
	series, err := LoadTradingSeries(ctx, s.series, s.cfg.TrackStocks)
	if err != nil {
		s.log.Error("回測載入序列失敗 (略過回測區塊):", err)
		return nil
	}
	if len(series) == 0 {
		s.log.Warn("無任何序列資料,略過回測區塊")
		return nil
	}

	// 全期連續回測 (策略 vs B&H vs Blend,含每月注資)。
	full, err := backtest.EvaluateFullSpan(s.cfg, series)
	if err != nil {
		s.log.Error("全期回測失敗 (略過回測區塊):", err)
		return nil
	}

	// walk-forward 多視窗穩健性 scorecard (視窗 24 月 / 步進 3 月,對齊 cmd/eval_csv 預設)。
	wfp := backtest.WalkForwardParams{WindowMonths: 24, StepMonths: 3, MinTradeDays: 200}
	_, agg, err := backtest.EvaluateWalkForward(s.cfg, series, wfp)
	if err != nil {
		s.log.Error("walk-forward 評估失敗 (略過回測區塊):", err)
		return nil
	}

	return buildBacktestDTO(full, agg, wfp)
}

// buildBacktestDTO 把全期 WindowReport 與 walk-forward AggregateReport 映射成 API 回測 DTO。
func buildBacktestDTO(full backtest.WindowReport, agg backtest.AggregateReport, wfp backtest.WalkForwardParams) *dto.BacktestPerformance {
	return &dto.BacktestPerformance{
		SpanStart:   full.Start.Format(dateLayout),
		SpanEnd:     full.End.Format(dateLayout),
		Years:       round2(full.Years),
		TotalIn:     round2(full.TotalIn),
		Strategy:    armMetricsDTO(full.TotalIn, full.Strat),
		BuyHold:     armMetricsDTO(full.TotalIn, full.BH),
		Buys:        full.Buys,
		Sells:       full.Sells,
		TrailSells:  full.TrailSells,
		ProfitSells: full.ProfitSells,
		Skipped:     full.Skipped,
		FinalCash:   round2(full.StratFinalCash),
		EquityCurve: equityCurveDTO(full.Dates, full.StratCurve, full.BHCurve),
		WalkForward: walkForwardDTO(wfp, agg),
	}
}

// maxEquityCurvePoints 為回測權益曲線回傳的最大取樣點數;超過時以等距取樣壓縮,
// 使多年 (數千交易日) 曲線維持合理回應大小,同時保留足夠折線解析度。
const maxEquityCurvePoints = 400

// sampleStride 回傳讓長度 n 序列等距取樣後點數不超過 maxPoints 的步長 (最小 1)。
func sampleStride(n, maxPoints int) int {
	if maxPoints <= 0 || n <= maxPoints {
		return 1
	}
	stride := n / maxPoints
	if n%maxPoints != 0 {
		stride++
	}
	return stride
}

// equityCurveDTO 把對齊的「日期 / 策略權益 / B&H 權益」三序列轉成等距取樣的 EquityPoint 切片。
// 三序列長度需一致 (回測引擎逐日對齊);長度不符或為空時回傳 nil。期末點 (最後一日) 必定入列,確保折線收在期末權益。
func equityCurveDTO(dates []time.Time, strat, bh []float64) []dto.EquityPoint {
	n := len(dates)
	if n == 0 || len(strat) != n || len(bh) != n {
		return nil
	}
	stride := sampleStride(n, maxEquityCurvePoints)
	pts := make([]dto.EquityPoint, 0, n/stride+1)
	// 以等距步長取樣每個權益點 (金額兩位小數)。
	for i := 0; i < n; i += stride {
		pts = append(pts, dto.EquityPoint{
			Date:        dates[i].Format(dateLayout),
			StratEquity: round2(strat[i]),
			BHEquity:    round2(bh[i]),
		})
	}
	// 補上最後一日 (期末權益),若未恰好落在取樣步長上。
	if last := n - 1; last%stride != 0 {
		pts = append(pts, dto.EquityPoint{
			Date:        dates[last].Format(dateLayout),
			StratEquity: round2(strat[last]),
			BHEquity:    round2(bh[last]),
		})
	}
	return pts
}

// armMetricsDTO 把單條曲線的 SeriesMetrics 映射成 API DTO;期末權益 = 投入本金 × 倍數。
func armMetricsDTO(totalIn float64, m backtest.SeriesMetrics) dto.ArmMetrics {
	return dto.ArmMetrics{
		FinalEquity: round2(totalIn * m.Multiple),
		Multiple:    metric(m.Multiple),
		MWR:         metric(m.MWR),
		MaxDrawdown: metric(m.MaxDD),
		Calmar:      metric(m.Calmar),
		Sortino:     metric(m.Sortino),
		AvgExposure: metric(m.AvgExp),
	}
}

// walkForwardDTO 把跨視窗 AggregateReport 與評估參數映射成 API scorecard DTO。
func walkForwardDTO(wfp backtest.WalkForwardParams, a backtest.AggregateReport) dto.WalkForwardScore {
	return dto.WalkForwardScore{
		WindowMonths:          wfp.WindowMonths,
		StepMonths:            wfp.StepMonths,
		NWindows:              a.NWindows,
		MedStratMWR:           metric(a.MedStratMWR),
		MedBHMWR:              metric(a.MedBHMWR),
		MedStratMaxDD:         metric(a.MedStratMDD),
		MedBHMaxDD:            metric(a.MedBHMDD),
		MedStratCalmar:        metric(a.MedStratCalmar),
		MedBHCalmar:           metric(a.MedBHCalmar),
		CalmarWinRate:         metric(a.CalmarWinRate),
		BlendSkillRate:        metric(a.BlendSkillRate),
		RetParticipation:      metric(a.MedRetParticipation),
		G1ReturnParticipation: a.G1RetParticipation,
		G2RiskReduction:       a.G2RiskReduction,
		G3CalmarVsBH:          a.G3CalmarVsBH,
		G4Skill:               a.G4Skill,
		G5Robustness:          a.G5Robustness,
		OverallPass:           a.OverallPass,
	}
}
