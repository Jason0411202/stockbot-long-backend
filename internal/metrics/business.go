// internal/metrics/business.go 定義交易機器人的業務層 Prometheus 指標與更新入口。
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 業務指標:投資組合當前狀態與交易活動,由 TradingService (imperative shell) 在
// 每日決策 / catch-up 回放後更新;交易引擎本身維持零 I/O,不觸碰本套件。
var (
	// portfolioCash 記錄引擎當前現金餘額。
	portfolioCash = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "stockbot_portfolio_cash",
		Help: "Current cash balance of the trading engine",
	})

	// portfolioHoldingValue 記錄持股市值 (以最近收盤估值)。
	portfolioHoldingValue = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "stockbot_portfolio_holding_value",
		Help: "Market value of current holdings (valued at latest close)",
	})

	// portfolioTotalEquity 記錄總權益 (現金 + 持股市值)。
	portfolioTotalEquity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "stockbot_portfolio_total_equity",
		Help: "Total account equity (cash + holding value)",
	})

	// portfolioCostBasis 記錄當前持倉的成本基礎。
	portfolioCostBasis = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "stockbot_portfolio_cost_basis",
		Help: "Cost basis of current holdings",
	})

	// lastProcessedDate 記錄水位線日期 (Unix 秒),可監控交易 loop 是否停滯。
	lastProcessedDate = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "stockbot_last_processed_date_seconds",
		Help: "Unix timestamp of the last processed trading day (watermark)",
	})

	// tradesTotal 依方向 (buy / sell) 累計成交筆數。
	tradesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stockbot_trades_total",
		Help: "Total number of executed trades by action",
	}, []string{"action"})

	// marketDataErrorsTotal 累計 TWSE 資料回補失敗次數 (資料源健康度觀測)。
	marketDataErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "stockbot_market_data_errors_total",
		Help: "Total number of failed TWSE market data update attempts",
	})
)

// SetPortfolioSnapshot 更新投資組合狀態的四個 gauge (現金 / 持股市值 / 總權益 / 成本基礎)。
func SetPortfolioSnapshot(cash, holdingValue, totalEquity, costBasis float64) {
	portfolioCash.Set(cash)
	portfolioHoldingValue.Set(holdingValue)
	portfolioTotalEquity.Set(totalEquity)
	portfolioCostBasis.Set(costBasis)
}

// SetLastProcessedDate 以水位線日期更新最後處理日 gauge。
func SetLastProcessedDate(day time.Time) {
	lastProcessedDate.Set(float64(day.Unix()))
}

// IncTrade 依方向 ("buy" / "sell") 累計一筆成交。
func IncTrade(action string) {
	tradesTotal.WithLabelValues(action).Inc()
}

// IncMarketDataError 累計一次 TWSE 資料回補失敗。
func IncMarketDataError() {
	marketDataErrorsTotal.Inc()
}
