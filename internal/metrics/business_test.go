// internal/metrics/business_test.go 驗證業務指標更新入口正確寫入 Prometheus gauge / counter。
package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestSetPortfolioSnapshot 驗證四個投資組合 gauge 依參數更新。
func TestSetPortfolioSnapshot(t *testing.T) {
	// Act
	SetPortfolioSnapshot(1000, 2000, 3000, 1500)

	// Assert — 各 gauge 應等於對應參數值。
	if got := testutil.ToFloat64(portfolioCash); got != 1000 {
		t.Fatalf("cash = %v, want 1000", got)
	}
	if got := testutil.ToFloat64(portfolioHoldingValue); got != 2000 {
		t.Fatalf("holding = %v, want 2000", got)
	}
	if got := testutil.ToFloat64(portfolioTotalEquity); got != 3000 {
		t.Fatalf("equity = %v, want 3000", got)
	}
	if got := testutil.ToFloat64(portfolioCostBasis); got != 1500 {
		t.Fatalf("cost basis = %v, want 1500", got)
	}
}

// TestSetLastProcessedDate 驗證水位線 gauge 寫入該日期的 Unix 秒數。
func TestSetLastProcessedDate(t *testing.T) {
	// Arrange
	day := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// Act
	SetLastProcessedDate(day)

	// Assert
	if got := testutil.ToFloat64(lastProcessedDate); got != float64(day.Unix()) {
		t.Fatalf("last processed = %v, want %v", got, day.Unix())
	}
}

// TestIncTrade 驗證成交 counter 依方向累計。
func TestIncTrade(t *testing.T) {
	// Arrange — 記下現值 (counter 為套件層級,可能已被其他測試累計)。
	before := testutil.ToFloat64(tradesTotal.WithLabelValues("buy"))

	// Act
	IncTrade("buy")

	// Assert
	if got := testutil.ToFloat64(tradesTotal.WithLabelValues("buy")); got != before+1 {
		t.Fatalf("trades buy = %v, want %v", got, before+1)
	}
}

// TestIncMarketDataError 驗證資料源失敗 counter 累計。
func TestIncMarketDataError(t *testing.T) {
	// Arrange
	before := testutil.ToFloat64(marketDataErrorsTotal)

	// Act
	IncMarketDataError()

	// Assert
	if got := testutil.ToFloat64(marketDataErrorsTotal); got != before+1 {
		t.Fatalf("market data errors = %v, want %v", got, before+1)
	}
}
