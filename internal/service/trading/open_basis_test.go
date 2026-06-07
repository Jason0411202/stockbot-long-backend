// internal/service/trading/open_basis_test.go 驗證開盤價基準決策 (decision_price_basis=open) 與線上進入點 ProcessOpenDecision。
package trading

import (
	"testing"
	"time"
)

// recordedTrade 記錄一次成交的方向、日期、股數、成交價與決策理由,供開盤價基準斷言使用。
type recordedTrade struct {
	side   string // "buy" / "sell"
	day    time.Time
	shares int
	price  float64
	reason TradeReason
}

// recordingExec 是測試用 Executor,把每筆買賣成交記錄下來 (不產生任何真實副作用)。
type recordingExec struct {
	trades []recordedTrade
}

// OnBuyApplied 記錄一筆買入成交 (含決策理由,供斷言檢視)。
func (r *recordingExec) OnBuyApplied(_ string, day time.Time, shares int, price, _ float64, reason TradeReason) error {
	r.trades = append(r.trades, recordedTrade{side: "buy", day: day, shares: shares, price: price, reason: reason})
	return nil
}

// OnSellApplied 記錄一筆賣出成交 (含決策理由,供斷言檢視)。
func (r *recordingExec) OnSellApplied(_ string, day time.Time, shares int, price, _ float64, reason TradeReason) error {
	r.trades = append(r.trades, recordedTrade{side: "sell", day: day, shares: shares, price: price, reason: reason})
	return nil
}

// seriesOC 由起始日 + 開盤 / 收盤序列建立 *StockSeries (開盤與收盤可不同),供開盤價基準測試。
func seriesOC(start time.Time, opens, closes []float64) *StockSeries {
	n := len(closes)
	dates := make([]time.Time, n)
	for i := 0; i < n; i++ {
		dates[i] = start.AddDate(0, 0, i)
	}
	op := make([]float64, n)
	copy(op, opens)
	cp := make([]float64, n)
	copy(cp, closes)
	return NewStockSeries(dates, op, cp, nil, nil, nil)
}

// TestProcessDay_OpenBasis_UsesOpenNotClose 驗證 open 基準下每筆成交價皆為當日開盤價而非收盤價。
func TestProcessDay_OpenBasis_UsesOpenNotClose(t *testing.T) {
	cfg := baseCfg("TEST")
	cfg.DecisionPriceBasis = "open"

	// 線性上漲收盤 (多頭),開盤一律比收盤低 5 元 → 成交價若取開盤必為 close-5。
	n := 80
	closes := make([]float64, n)
	opens := make([]float64, n)
	for i := 0; i < n; i++ {
		closes[i] = 100 + float64(i)
		opens[i] = closes[i] - 5
	}
	start := mustDate(t, "2024-01-01")
	s := seriesOC(start, opens, closes)
	series := map[string]*StockSeries{"TEST": s}

	engine := NewEngine(cfg)
	rec := &recordingExec{}
	dates := CollectDateUnion(series)
	if err := engine.ProcessDates(dates, series, rec); err != nil {
		t.Fatalf("ProcessDates: %v", err)
	}

	if len(rec.trades) == 0 {
		t.Fatalf("open 基準應至少觸發一筆買入,卻沒有任何成交")
	}
	// 每筆成交價必等於當日開盤 (= 該日 idx 的 OpenAt),絕不等於收盤。
	for _, tr := range rec.trades {
		idx, ok := s.DateIndex[tr.day.Format("2006-01-02")]
		if !ok {
			t.Fatalf("成交日 %s 不在序列中", tr.day.Format("2006-01-02"))
		}
		wantOpen := s.OpenAt(idx)
		if tr.price != wantOpen {
			t.Fatalf("成交價應為當日開盤 %.2f,得 %.2f (收盤=%.2f)", wantOpen, tr.price, s.ClosePrices[idx])
		}
		if tr.price == s.ClosePrices[idx] {
			t.Fatalf("成交價誤用收盤 %.2f", tr.price)
		}
	}
}

// TestProcessOpenDecision_DecidesAtInjectedOpen 驗證線上進入點以「T-1 series + 注入今日開盤價」於開盤價成交。
func TestProcessOpenDecision_DecidesAtInjectedOpen(t *testing.T) {
	cfg := baseCfg("TEST")
	cfg.DecisionPriceBasis = "open" // 線上一律 open 基準

	// 建立到 T-1 為止的線性上漲收盤序列 (多頭:close[last] 高於 MA50)。
	n := 60
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		closes[i] = 100 + float64(i) // 100..159
	}
	start := mustDate(t, "2024-01-01")
	s := seriesOC(start, closes, closes) // 歷史開盤=收盤,不影響今日注入
	series := map[string]*StockSeries{"TEST": s}

	engine := NewEngine(cfg)
	rec := &recordingExec{}

	// 今日 T = 序列最後一天的隔天;注入開盤價 160 (< MA10(asOf)×1.05 → 觸發逢低買入)。
	today := s.Dates[n-1].AddDate(0, 0, 1)
	const todayOpen = 160.0
	if err := engine.ProcessOpenDecision(today, map[string]float64{"TEST": todayOpen}, series, rec); err != nil {
		t.Fatalf("ProcessOpenDecision: %v", err)
	}

	if len(rec.trades) != 1 || rec.trades[0].side != "buy" {
		t.Fatalf("應恰好觸發一筆買入,得 %+v", rec.trades)
	}
	if rec.trades[0].price != todayOpen {
		t.Fatalf("成交價應為注入的今日開盤 %.2f,得 %.2f", todayOpen, rec.trades[0].price)
	}
	if rec.trades[0].day.Format("2006-01-02") != today.Format("2006-01-02") {
		t.Fatalf("成交日應為今日 %s,得 %s", today.Format("2006-01-02"), rec.trades[0].day.Format("2006-01-02"))
	}
	if rec.trades[0].shares <= 0 {
		t.Fatalf("買入股數應 > 0,得 %d", rec.trades[0].shares)
	}
}

// TestProcessOpenDecision_SkipsMissingOpen 驗證未提供 (或 <=0) 開盤價的股票被略過,不致成交。
func TestProcessOpenDecision_SkipsMissingOpen(t *testing.T) {
	cfg := baseCfg("TEST")
	cfg.DecisionPriceBasis = "open"

	n := 60
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		closes[i] = 100 + float64(i)
	}
	s := seriesOC(mustDate(t, "2024-01-01"), closes, closes)
	series := map[string]*StockSeries{"TEST": s}

	engine := NewEngine(cfg)
	rec := &recordingExec{}
	today := s.Dates[n-1].AddDate(0, 0, 1)

	// 開盤價 0 (尚未取得) → 略過,無成交。
	if err := engine.ProcessOpenDecision(today, map[string]float64{"TEST": 0}, series, rec); err != nil {
		t.Fatalf("ProcessOpenDecision: %v", err)
	}
	if len(rec.trades) != 0 {
		t.Fatalf("無開盤價應略過,卻產生成交 %+v", rec.trades)
	}
}
