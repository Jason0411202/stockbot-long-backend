// cmd/research_run/main_test.go 驗證回測結果的輸出格式，包含 PnL 百分比計算與零本金防呆邏輯。
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/service/backtest"
)

// TestPrintResult 驗證 printResult 函式輸出包含正確的欄位名稱、股票代號及 PnL 百分比計算結果。
func TestPrintResult(t *testing.T) {
	// Arrange
	res := &backtest.BacktestResult{
		InitialCash: 100000, TotalContributed: 50000,
		FinalCash: 20000, FinalHoldingValue: 180000, FinalTotal: 200000,
		TotalBuys: 30, TotalSells: 10, SkippedBuys: 2,
	}

	// Act
	var out bytes.Buffer
	printResult(&out, []string{"00631L", "00830"}, 60, res, 1500*time.Millisecond)

	// Assert — 關鍵欄位 + PnL 計算 (200000-150000=+50000, +33.33%)。
	s := out.String()
	for _, want := range []string{"BACKTEST RESULT", "00631L", "FinalTotal:          200000.00", "+50000.00", "+33.33%"} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
}

// TestPrintResult_ZeroInvestedNoDivByZero 驗證本金為零時 printResult 不發生除零 panic 且輸出正確百分比。
func TestPrintResult_ZeroInvestedNoDivByZero(t *testing.T) {
	// Arrange — 本金為 0 不可除零 panic。
	res := &backtest.BacktestResult{}

	// Act
	var out bytes.Buffer
	printResult(&out, nil, 0, res, 0)

	// Assert
	if !strings.Contains(out.String(), "+0.00%") {
		t.Fatalf("zero-invested PnL should be +0.00%%, got:\n%s", out.String())
	}
}
