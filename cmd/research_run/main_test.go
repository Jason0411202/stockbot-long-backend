package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"main/kernals"
)

// main_test.go 驗證回測結果輸出格式 (含 PnL 百分比與零本金防呆)。

func TestPrintResult(t *testing.T) {
	// Arrange
	res := &kernals.BacktestResult{
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

func TestPrintResult_ZeroInvestedNoDivByZero(t *testing.T) {
	// Arrange — 本金為 0 不可除零 panic。
	res := &kernals.BacktestResult{}

	// Act
	var out bytes.Buffer
	printResult(&out, nil, 0, res, 0)

	// Assert
	if !strings.Contains(out.String(), "+0.00%") {
		t.Fatalf("zero-invested PnL should be +0.00%%, got:\n%s", out.String())
	}
}
