// internal/dto/performance.go 定義策略績效摘要 API 的回應 DTO (本金明細 + 實盤現況 + 回測指標)。
package dto

import (
	"encoding/json"
	"math"
)

// JSONFloat 是可安全序列化的浮點數:NaN / ±Inf 會被編成 JSON null。
// encoding/json 無法直接編 NaN/Inf (會回錯),而回測的 MWR/Calmar/Sortino 等指標在
// 邊界情況 (無賣出、零回撤、樣本不足) 可能為 NaN/Inf,故統一以此型別承載並降級為 null。
type JSONFloat float64

// MarshalJSON 將 NaN / ±Inf 編為 null,其餘照一般浮點數輸出。
func (f JSONFloat) MarshalJSON() ([]byte, error) {
	v := float64(f)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

// PerformanceSummary 是 GET /api/get_performance_summary 的回應:一次回傳本金明細、實盤現況與回測績效。
// 本金 (investing principal) 指「從外部注入股市的資金」(期初一次性 + 每月定額),不含後續滾出的獲利。
type PerformanceSummary struct {
	// ── 本金明細 (外部注入,非滾出的獲利) ──
	InitialCash         float64 `json:"initial_cash"`         // 期初一次性投入本金
	MonthlyContribution float64 `json:"monthly_contribution"` // 每月定額注資設定 (0 = 關閉)
	TotalContributed    float64 `json:"total_contributed"`    // 累計已注資 (不含期初)
	TotalInvested       float64 `json:"total_invested"`       // 投入本金合計 = 期初 + 累計注資

	// ── 實盤現況 (真實帳本 + BotState) ──
	CurrentCash     float64 `json:"current_cash"`      // 目前閒置現金 (未投入股市的預備現金)
	HoldingValue    float64 `json:"holding_value"`     // 目前持股市值 (即時收盤價估)
	TotalEquity     float64 `json:"total_equity"`      // 總權益 = 現金 + 持股市值
	HoldingRatio    float64 `json:"holding_ratio"`     // 持股佔總資產比例 (%) = 持股市值 / 總權益;總權益<=0 時為 0
	CashRatio       float64 `json:"cash_ratio"`        // 預備現金佔總資產比例 (%) = 現金 / 總權益;與 holding_ratio 合計約 100
	RealizedPnL     float64 `json:"realized_pnl"`      // 累計已實現損益
	UnrealizedPnL   float64 `json:"unrealized_pnl"`    // 目前未實現損益
	TotalPnL        float64 `json:"total_pnl"`         // 總損益 = 總權益 - 投入本金
	TotalReturnRate float64 `json:"total_return_rate"` // 總報酬率 (%) = 總損益 / 投入本金

	// ── 回測績效 (全期 + walk-forward;資料不足或失敗時為 null) ──
	Backtest *BacktestPerformance `json:"backtest"`
}

// BacktestPerformance 是以 config 期初本金 + 每月注資跑歷史回測 (策略 vs 買進持有) 的績效輸出。
type BacktestPerformance struct {
	SpanStart string  `json:"span_start"` // 回測起點 (YYYY-MM-DD;所有追蹤股票都已發行的最早日)
	SpanEnd   string  `json:"span_end"`   // 回測終點 (YYYY-MM-DD)
	Years     float64 `json:"years"`      // 回測年數 (Actual/365)
	TotalIn   float64 `json:"total_in"`   // 回測投入本金合計 (期初 + 期間注資)

	Strategy ArmMetrics `json:"strategy"` // 本策略
	BuyHold  ArmMetrics `json:"buy_hold"` // Buy & Hold 對照組 (資金一解鎖就買滿)

	// 策略交易統計
	Buys        int     `json:"buys"`         // 買入次數
	Sells       int     `json:"sells"`        // 賣出次數
	TrailSells  int     `json:"trail_sells"`  // 移動停利賣出次數
	ProfitSells int     `json:"profit_sells"` // 獲利了結賣出次數
	Skipped     int     `json:"skipped"`      // 現金不足被夾取跳過的買入次數
	FinalCash   float64 `json:"final_cash"`   // 策略期末閒置現金 (現金尾巴)

	EquityCurve []EquityPoint    `json:"equity_curve"` // 全期 (取樣) 每日權益曲線:策略 vs B&H,供前端折線圖
	WalkForward WalkForwardScore `json:"walk_forward"` // 多視窗穩健性 scorecard
}

// EquityPoint 是回測權益曲線時間軸上的單一取樣點 (供前端策略 vs B&H 折線圖)。
// 金額為當日總權益 (現金 + 持股市值,含已注入資金);長序列會被等距取樣以控制回應大小。
type EquityPoint struct {
	Date        string  `json:"date"`         // 交易日 (YYYY-MM-DD)
	StratEquity float64 `json:"strat_equity"` // 策略當日總權益
	BHEquity    float64 `json:"bh_equity"`    // Buy & Hold 當日總權益
}

// ArmMetrics 是某一條權益曲線 (策略 / B&H) 在全期回測下的核心績效指標。
// 比率型欄位以 JSONFloat 承載 (邊界可能 NaN/Inf → null)。
type ArmMetrics struct {
	FinalEquity float64   `json:"final_equity"` // 期末總權益
	Multiple    JSONFloat `json:"multiple"`     // 本金倍數 = 期末權益 / 投入本金
	MWR         JSONFloat `json:"mwr"`          // 資金加權年化報酬 (XIRR)
	MaxDrawdown JSONFloat `json:"max_drawdown"` // NAV 單位淨值最大回撤 (<=0,扣除注資灌水)
	Calmar      JSONFloat `json:"calmar"`       // MWR / |MaxDrawdown|
	Sortino     JSONFloat `json:"sortino"`      // 年化 Sortino (用 NAV 日報酬)
	AvgExposure JSONFloat `json:"avg_exposure"` // 平均持股佔比 (資金利用率)
}

// WalkForwardScore 是多視窗 walk-forward 評估的中位數指標與五道關卡 (回答「這策略是否穩健」)。
type WalkForwardScore struct {
	WindowMonths int `json:"window_months"` // 每個視窗長度 (月)
	StepMonths   int `json:"step_months"`   // 視窗步進 (月)
	NWindows     int `json:"n_windows"`     // 視窗總數

	MedStratMWR      JSONFloat `json:"med_strat_mwr"`          // 中位 策略 MWR
	MedBHMWR         JSONFloat `json:"med_bh_mwr"`             // 中位 B&H MWR
	MedStratMaxDD    JSONFloat `json:"med_strat_max_drawdown"` // 中位 策略 MaxDD
	MedBHMaxDD       JSONFloat `json:"med_bh_max_drawdown"`    // 中位 B&H MaxDD
	MedStratCalmar   JSONFloat `json:"med_strat_calmar"`       // 中位 策略 Calmar (僅取有限值)
	MedBHCalmar      JSONFloat `json:"med_bh_calmar"`          // 中位 B&H Calmar (僅取有限值)
	CalmarWinRate    JSONFloat `json:"calmar_win_rate"`        // 策略 Calmar 勝 B&H 的視窗比例
	BlendSkillRate   JSONFloat `json:"blend_skill_rate"`       // 策略雙贏同曝險 Blend 的視窗比例 (真擇時)
	RetParticipation JSONFloat `json:"ret_participation"`      // 中位 報酬參與率 (策略 MWR / B&H MWR)

	G1ReturnParticipation bool `json:"g1_return_participation"` // 守住 B&H 七成報酬
	G2RiskReduction       bool `json:"g2_risk_reduction"`       // 回撤 <=60% B&H
	G3CalmarVsBH          bool `json:"g3_calmar_vs_bh"`         // Calmar 勝率 >=70%
	G4Skill               bool `json:"g4_skill"`                // 真擇時 >=50% (vs Blend)
	G5Robustness          bool `json:"g5_robustness"`           // 最差視窗回撤不輸 B&H
	OverallPass           bool `json:"overall_pass"`            // G1~G4 全過
}
