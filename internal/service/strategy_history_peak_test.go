// internal/service/strategy_history_peak_test.go 驗證歷史高點保留完整持倉期間且排除未來資料。
package service

import (
	"testing"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// peakTestDate 提供歷史高點測試的固定日曆日期。
func peakTestDate(offset int) time.Time {
	return time.Date(2026, 10, 1+offset, 0, 0, 0, 0, time.UTC)
}

// peakTestSeries 建立開盤與收盤相同的歷史測試行情。
func peakTestSeries(prices ...float64) map[string]*trading.StockSeries {
	dates := make([]time.Time, len(prices))
	for i := range dates {
		dates[i] = peakTestDate(i)
	}
	return map[string]*trading.StockSeries{"AAA": trading.NewStockSeries(dates, prices, prices, nil, nil, nil)}
}

// TestHistoricalPeaks_PartialFragmentsKeepEarlierPeak 驗證較早批次賣完後仍保留連續持倉的高點。
func TestHistoricalPeaks_PartialFragmentsKeepEarlierPeak(t *testing.T) {
	lots := []historicalLot{
		{stock: "AAA", buy: peakTestDate(0), sell: peakTestDate(2), shares: 3, price: 80},
		{stock: "AAA", buy: peakTestDate(0), sell: peakTestDate(3), shares: 7, price: 80},
		{stock: "AAA", buy: peakTestDate(2), shares: 5, price: 90},
	}
	series := peakTestSeries(80, 150, 90, 100, 95)
	if got := historicalPeaks(lots, nil, series, peakTestDate(5), true)["AAA"]; got != 150 {
		t.Fatalf("partial exit discarded prior continuous holding peak: %v", got)
	}
	trades := map[string][]trading.HistoricalTrade{"2026-10-04": {{StockID: "AAA", Shares: 7, Price: 160, BuyDate: peakTestDate(0)}}}
	if got := historicalPeaks(lots, trades, series, peakTestDate(5), true)["AAA"]; got != 160 {
		t.Fatalf("known historical sell quote was omitted from active holding peak: %v", got)
	}
}

// TestHistoricalPeaks_FullExitResetsAndSameDayBuysComeFirst 驗證完整出清重設、同日先買再賣保持連續持股。
func TestHistoricalPeaks_FullExitResetsAndSameDayBuysComeFirst(t *testing.T) {
	series := peakTestSeries(80, 200, 100, 90, 70, 75)
	lots := []historicalLot{
		{stock: "AAA", buy: peakTestDate(0), sell: peakTestDate(3), shares: 10, price: 80},
		{stock: "AAA", buy: peakTestDate(3), shares: 5, price: 90},
	}
	if got := historicalPeaks(lots, nil, series, peakTestDate(6), true)["AAA"]; got != 200 {
		t.Fatalf("same-day buy then sell incorrectly reset peak: %v", got)
	}
	lots[1].buy, lots[1].price = peakTestDate(4), 70
	if got := historicalPeaks(lots, nil, series, peakTestDate(4), true)["AAA"]; got != 0 {
		t.Fatalf("full exit did not clear peak: %v", got)
	}
	if got := historicalPeaks(lots, nil, series, peakTestDate(6), true)["AAA"]; got != 75 {
		t.Fatalf("new holding inherited old peak: %v", got)
	}
}

// TestHistoricalPeaks_PriceBasisAndNoFutureLeak 驗證開盤與收盤基準、缺值與決策當日的排除。
func TestHistoricalPeaks_PriceBasisAndNoFutureLeak(t *testing.T) {
	lots := []historicalLot{{stock: "AAA", buy: peakTestDate(0), shares: 10, price: 80}}
	dates := []time.Time{peakTestDate(0), peakTestDate(1), peakTestDate(2), peakTestDate(3)}
	series := map[string]*trading.StockSeries{"AAA": trading.NewStockSeries(dates, []float64{80, 100, 0, 999}, []float64{80, 150, 180, 999}, nil, nil, nil)}
	trades := map[string][]trading.HistoricalTrade{"2026-10-04": {{StockID: "AAA", Buy: true, Shares: 5, Price: 999}}}
	if got := historicalPeaks(lots, trades, series, peakTestDate(3), true)["AAA"]; got != 100 {
		t.Fatalf("open peak used missing-price fallback or future information: %v", got)
	}
	if got := historicalPeaks(lots, trades, series, peakTestDate(3), false)["AAA"]; got != 180 {
		t.Fatalf("close peak used wrong basis: %v", got)
	}
	lots[0].buy = peakTestDate(3)
	if got := historicalPeaks(lots, trades, series, peakTestDate(3), true)["AAA"]; got != 0 {
		t.Fatalf("future purchase leaked into historical holding: %v", got)
	}
}

// TestHistoricalPeaks_RecordedPricesReplaceDailyQuotes 驗證成交日的帳本價優先於不同的歷史行情。
func TestHistoricalPeaks_RecordedPricesReplaceDailyQuotes(t *testing.T) {
	lots := []historicalLot{
		{stock: "AAA", buy: peakTestDate(0), sell: peakTestDate(2), shares: 5, price: 80},
		{stock: "AAA", buy: peakTestDate(0), shares: 5, price: 80},
	}
	series := peakTestSeries(200, 100, 999)
	trades := map[string][]trading.HistoricalTrade{"2026-10-03": {{StockID: "AAA", Shares: 5, Price: 110, BuyDate: peakTestDate(0)}}}
	if got := historicalPeaks(lots, trades, series, peakTestDate(3), true)["AAA"]; got != 110 {
		t.Fatalf("daily quote overrode known historical decision price: %v", got)
	}
}
