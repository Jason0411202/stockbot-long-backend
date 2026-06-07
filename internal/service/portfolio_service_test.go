// internal/service/portfolio_service_test.go 驗證 PortfolioService 的買賣股票、未實現損益計算及已實現損益四捨五入邏輯。
package service

import (
	"context"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// newPortfolioService 建立注入 fakeLedger 與 fakeStock 的 PortfolioService，供各子測試共用。
func newPortfolioService(ledger *fakeLedger, stock *fakeStock) *PortfolioService {
	return NewPortfolioService(ledger, stock, newTestLogger())
}

// --- SellShares: full-lot sale -------------------------------------------------

// TestSellShares_FullLotSale 驗證賣出數量等於單一批次全部股數時，批次被刪除並產生正確的已實現損益記錄。
func TestSellShares_FullLotSale(t *testing.T) {
	ledger := &fakeLedger{
		lots: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2024-01-02", StockID: "00631L", StockName: "元大台灣50正2", TransactionPrice: 10, InvestmentCost: 1000, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.prices["00631L"] = 12 // todayClose

	svc := newPortfolioService(ledger, stock)
	// 成交價 12 由呼叫端傳入 (引擎開盤成交價);不再以 DB 查價。
	if err := svc.SellShares(context.Background(), "00631L", "2024-03-01", 100, 12); err != nil {
		t.Fatalf("SellShares: %v", err)
	}

	if len(ledger.deletes) != 1 || ledger.deletes[0].transactionDate != "2024-01-02" {
		t.Fatalf("expected one delete of the full lot, got %+v", ledger.deletes)
	}
	if len(ledger.updates) != 0 {
		t.Fatalf("expected no partial updates, got %+v", ledger.updates)
	}
	if len(ledger.realized) != 1 {
		t.Fatalf("expected one realized row, got %d", len(ledger.realized))
	}
	r := ledger.realized[0]
	// revenue = 12*100 = 1200; profitLoss = 1200-1000 = 200; rate = 200/1000*100 = 20
	if r.Revenue != 1200 || r.ProfitLoss != 200 || r.ProfitRate != 20 {
		t.Fatalf("full-lot math wrong: revenue=%v pl=%v rate=%v", r.Revenue, r.ProfitLoss, r.ProfitRate)
	}
	if r.Shares != 100 || r.BuyDate != "2024-01-02" || r.SellDate != "2024-03-01" {
		t.Fatalf("realized fields wrong: %+v", r)
	}
	if r.PurchasePrice != 10 || r.SellPrice != 12 || r.InvestmentCost != 1000 {
		t.Fatalf("realized price/cost fields wrong: %+v", r)
	}
}

// --- SellShares: partial-lot sale ---------------------------------------------

// TestSellShares_PartialLotSale 驗證部分賣出時批次股數與成本被正確更新，並產生對應的已實現損益。
func TestSellShares_PartialLotSale(t *testing.T) {
	ledger := &fakeLedger{
		lots: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2024-01-02", StockID: "00631L", StockName: "元大台灣50正2", TransactionPrice: 10, InvestmentCost: 1000, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.prices["00631L"] = 15

	svc := newPortfolioService(ledger, stock)
	if err := svc.SellShares(context.Background(), "00631L", "2024-03-01", 40, 15); err != nil {
		t.Fatalf("SellShares: %v", err)
	}

	if len(ledger.deletes) != 0 {
		t.Fatalf("expected no deletes on partial sale, got %+v", ledger.deletes)
	}
	if len(ledger.updates) != 1 {
		t.Fatalf("expected one update, got %+v", ledger.updates)
	}
	u := ledger.updates[0]
	// soldCost = 10*40 = 400; newShares = 60; newCost = 1000-400 = 600
	if u.shares != 60 || u.investmentCost != 600 {
		t.Fatalf("partial update wrong: shares=%d cost=%v", u.shares, u.investmentCost)
	}
	if len(ledger.realized) != 1 {
		t.Fatalf("expected one realized row, got %d", len(ledger.realized))
	}
	r := ledger.realized[0]
	// revenue = 15*40 = 600; soldCost = 400; profitLoss = 200; rate = 200/400*100 = 50
	if r.Revenue != 600 || r.ProfitLoss != 200 || r.ProfitRate != 50 {
		t.Fatalf("partial-lot math wrong: revenue=%v pl=%v rate=%v", r.Revenue, r.ProfitLoss, r.ProfitRate)
	}
	if r.InvestmentCost != 400 || r.Shares != 40 {
		t.Fatalf("partial realized cost/shares wrong: cost=%v shares=%d", r.InvestmentCost, r.Shares)
	}
}

// --- SellShares: multi-lot drain to target ------------------------------------

// TestSellShares_MultiLotDrainToTarget 驗證跨多批次賣出時依序耗盡批次並正確計算各批次的已實現損益。
func TestSellShares_MultiLotDrainToTarget(t *testing.T) {
	ledger := &fakeLedger{
		lots: []entity.UnrealizedGainsLoss{
			// cheapest first (order GetLowestUnrealized would return)
			{TransactionDate: "2024-01-02", StockID: "X", StockName: "n", TransactionPrice: 10, InvestmentCost: 500, Shares: 50},
			{TransactionDate: "2024-02-02", StockID: "X", StockName: "n", TransactionPrice: 20, InvestmentCost: 2000, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.prices["X"] = 25

	svc := newPortfolioService(ledger, stock)
	// target 80 = full first lot (50) + partial 30 of second lot
	if err := svc.SellShares(context.Background(), "X", "2024-03-01", 80, 25); err != nil {
		t.Fatalf("SellShares: %v", err)
	}

	if len(ledger.deletes) != 1 || ledger.deletes[0].transactionDate != "2024-01-02" {
		t.Fatalf("expected first lot deleted, got %+v", ledger.deletes)
	}
	if len(ledger.updates) != 1 || ledger.updates[0].transactionDate != "2024-02-02" {
		t.Fatalf("expected second lot updated, got %+v", ledger.updates)
	}
	// second lot partial: soldShares=30, soldCost=20*30=600, newShares=70, newCost=2000-600=1400
	if ledger.updates[0].shares != 70 || ledger.updates[0].investmentCost != 1400 {
		t.Fatalf("second lot update wrong: %+v", ledger.updates[0])
	}
	if len(ledger.realized) != 2 {
		t.Fatalf("expected two realized rows, got %d", len(ledger.realized))
	}
	// first realized (full): revenue=25*50=1250, pl=1250-500=750
	if ledger.realized[0].Revenue != 1250 || ledger.realized[0].ProfitLoss != 750 {
		t.Fatalf("first realized wrong: %+v", ledger.realized[0])
	}
	// second realized (partial): revenue=25*30=750, soldCost=600, pl=150
	if ledger.realized[1].Revenue != 750 || ledger.realized[1].ProfitLoss != 150 {
		t.Fatalf("second realized wrong: %+v", ledger.realized[1])
	}
}

// --- SellShares: legacy shares<=0 lot deleted (no infinite loop) ---------------

// TestSellShares_LegacyZeroShareLotDeleted 驗證股數為零的遺留批次被直接刪除，不進入賣出計算，且不造成無限迴圈。
func TestSellShares_LegacyZeroShareLotDeleted(t *testing.T) {
	ledger := &fakeLedger{
		lots: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2023-12-01", StockID: "X", StockName: "n", TransactionPrice: 10, InvestmentCost: 500, Shares: 0}, // legacy
			{TransactionDate: "2024-01-02", StockID: "X", StockName: "n", TransactionPrice: 12, InvestmentCost: 1200, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.prices["X"] = 20

	svc := newPortfolioService(ledger, stock)
	if err := svc.SellShares(context.Background(), "X", "2024-03-01", 100, 20); err != nil {
		t.Fatalf("SellShares: %v", err)
	}

	// First delete is the legacy zero-share lot; second delete is the full real lot.
	if len(ledger.deletes) != 2 {
		t.Fatalf("expected 2 deletes (legacy + full lot), got %+v", ledger.deletes)
	}
	if ledger.deletes[0].transactionDate != "2023-12-01" {
		t.Fatalf("expected legacy lot deleted first, got %+v", ledger.deletes)
	}
	if ledger.deletes[1].transactionDate != "2024-01-02" {
		t.Fatalf("expected real lot deleted second, got %+v", ledger.deletes)
	}
	// Only the real lot produces a realized row.
	if len(ledger.realized) != 1 || ledger.realized[0].Shares != 100 {
		t.Fatalf("expected one realized row for the real lot, got %+v", ledger.realized)
	}
}

// --- SellShares: no inventory -> no-op ----------------------------------------

// TestSellShares_NoInventoryNoOp 驗證庫存為空時賣出操作為無動作，不修改任何資料。
func TestSellShares_NoInventoryNoOp(t *testing.T) {
	ledger := &fakeLedger{} // empty
	stock := newFakeStock()
	stock.prices["X"] = 20

	svc := newPortfolioService(ledger, stock)
	if err := svc.SellShares(context.Background(), "X", "2024-03-01", 100, 20); err != nil {
		t.Fatalf("SellShares no-op should not error: %v", err)
	}
	if len(ledger.deletes) != 0 || len(ledger.updates) != 0 || len(ledger.realized) != 0 {
		t.Fatalf("no-op should not mutate anything: deletes=%+v updates=%+v realized=%+v", ledger.deletes, ledger.updates, ledger.realized)
	}
}

// TestSellShares_NonPositiveTargetNoOp 驗證目標股數為零或負數時賣出操作為無動作且不回傳錯誤。
func TestSellShares_NonPositiveTargetNoOp(t *testing.T) {
	ledger := &fakeLedger{
		lots: []entity.UnrealizedGainsLoss{{TransactionDate: "2024-01-02", StockID: "X", Shares: 100}},
	}
	stock := newFakeStock()
	svc := newPortfolioService(ledger, stock)
	if err := svc.SellShares(context.Background(), "X", "2024-03-01", 0, 20); err != nil {
		t.Fatalf("zero target should be a no-op without error: %v", err)
	}
	if len(ledger.realized) != 0 {
		t.Fatalf("zero target must not realize anything")
	}
}

// --- BuyShares ----------------------------------------------------------------

// TestBuyShares_NonPositiveSharesError 驗證買入股數為零或負數時回傳錯誤且不插入任何批次。
func TestBuyShares_NonPositiveSharesError(t *testing.T) {
	ledger := &fakeLedger{}
	stock := newFakeStock()
	svc := newPortfolioService(ledger, stock)
	if err := svc.BuyShares(context.Background(), "X", "2024-03-01", 0, 10); err == nil {
		t.Fatal("expected error for shares<=0")
	}
	if len(ledger.lots) != 0 {
		t.Fatalf("no lot should be inserted on invalid shares")
	}
}

// TestBuyShares_HappyPathInsertsCorrectCost 驗證買入成功時插入的批次包含正確的成本、價格、股數及身分識別欄位。
func TestBuyShares_HappyPathInsertsCorrectCost(t *testing.T) {
	ledger := &fakeLedger{}
	stock := newFakeStock()
	stock.prices["00631L"] = 12.5
	stock.names["00631L"] = "元大台灣50正2"

	svc := newPortfolioService(ledger, stock)
	// 成交價 12.5 由呼叫端傳入 (引擎開盤成交價)。
	if err := svc.BuyShares(context.Background(), "00631L", "2024-03-01", 100, 12.5); err != nil {
		t.Fatalf("BuyShares: %v", err)
	}
	if len(ledger.lots) != 1 {
		t.Fatalf("expected one inserted lot, got %d", len(ledger.lots))
	}
	lot := ledger.lots[0]
	// cost = 12.5 * 100 = 1250
	if lot.InvestmentCost != 1250 || lot.TransactionPrice != 12.5 || lot.Shares != 100 {
		t.Fatalf("buy lot wrong: %+v", lot)
	}
	if lot.StockName != "元大台灣50正2" || lot.TransactionDate != "2024-03-01" || lot.StockID != "00631L" {
		t.Fatalf("buy lot identity wrong: %+v", lot)
	}
}

// --- UnrealizedGainsLosses ----------------------------------------------------

// TestUnrealizedGainsLosses_PnLMathAndRounding 驗證未實現損益的當前市值、損益金額及損益率計算結果正確。
func TestUnrealizedGainsLosses_PnLMathAndRounding(t *testing.T) {
	ledger := &fakeLedger{
		listU: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2024-01-02", StockID: "X", StockName: "n", TransactionPrice: 10, InvestmentCost: 1000, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.prices["X"] = 12.345 // todayClose

	svc := newPortfolioService(ledger, stock)
	rows, err := svc.UnrealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatalf("UnrealizedGainsLosses: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	r := rows[0]
	// nowValue = 12.345*100 = 1234.5; pl = 234.5; rate = 234.5/1000*100 = 23.45
	if r.TodayClosePrice != 12.345 {
		t.Fatalf("todayClosePrice wrong: %v", r.TodayClosePrice)
	}
	if r.NowValue != 1234.5 || r.PredictProfitLoss != 234.5 || r.PredictProfitRate != 23.45 {
		t.Fatalf("unrealized math wrong: now=%v pl=%v rate=%v", r.NowValue, r.PredictProfitLoss, r.PredictProfitRate)
	}
}

// TestUnrealizedGainsLosses_LegacyZeroSharesBranch 驗證股數為零的遺留批次以價格比例計算市值，結果正確。
func TestUnrealizedGainsLosses_LegacyZeroSharesBranch(t *testing.T) {
	ledger := &fakeLedger{
		listU: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2023-01-02", StockID: "X", StockName: "n", TransactionPrice: 10, InvestmentCost: 1000, Shares: 0},
		},
	}
	stock := newFakeStock()
	stock.prices["X"] = 15

	svc := newPortfolioService(ledger, stock)
	rows, err := svc.UnrealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatalf("UnrealizedGainsLosses: %v", err)
	}
	r := rows[0]
	// legacy: nowValue = (15/10)*1000 = 1500; pl = 500; rate = 50
	if r.NowValue != 1500 || r.PredictProfitLoss != 500 || r.PredictProfitRate != 50 {
		t.Fatalf("legacy branch math wrong: now=%v pl=%v rate=%v", r.NowValue, r.PredictProfitLoss, r.PredictProfitRate)
	}
}

// TestUnrealizedGainsLosses_PriceErrorUsesZero 驗證取得收盤價失敗時，市值以零計算且整體呼叫不回傳錯誤。
func TestUnrealizedGainsLosses_PriceErrorUsesZero(t *testing.T) {
	ledger := &fakeLedger{
		listU: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2024-01-02", StockID: "X", StockName: "n", TransactionPrice: 10, InvestmentCost: 1000, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.priceErr["X"] = errFake

	svc := newPortfolioService(ledger, stock)
	rows, err := svc.UnrealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatalf("UnrealizedGainsLosses should swallow price error: %v", err)
	}
	r := rows[0]
	// todayClose=0 -> nowValue=0; pl = -1000; rate = -100
	if r.TodayClosePrice != 0 || r.NowValue != 0 || r.PredictProfitLoss != -1000 || r.PredictProfitRate != -100 {
		t.Fatalf("price-error path wrong: close=%v now=%v pl=%v rate=%v", r.TodayClosePrice, r.NowValue, r.PredictProfitLoss, r.PredictProfitRate)
	}
}

// TestUnrealizedGainsLosses_ZeroCostGuardsRate 驗證投資成本為零時損益率被保護為 0，避免除以零。
func TestUnrealizedGainsLosses_ZeroCostGuardsRate(t *testing.T) {
	ledger := &fakeLedger{
		listU: []entity.UnrealizedGainsLoss{
			{TransactionDate: "2024-01-02", StockID: "X", StockName: "n", TransactionPrice: 0, InvestmentCost: 0, Shares: 100},
		},
	}
	stock := newFakeStock()
	stock.prices["X"] = 5

	svc := newPortfolioService(ledger, stock)
	rows, err := svc.UnrealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatalf("UnrealizedGainsLosses: %v", err)
	}
	if rows[0].PredictProfitRate != 0 {
		t.Fatalf("rate must be guarded to 0 when investment_cost==0, got %v", rows[0].PredictProfitRate)
	}
}

// --- RealizedGainsLosses ------------------------------------------------------

// TestRealizedGainsLosses_Rounding 驗證已實現損益的收益、損益金額及損益率皆四捨五入至小數點後兩位。
func TestRealizedGainsLosses_Rounding(t *testing.T) {
	ledger := &fakeLedger{
		listR: []entity.RealizedGainsLoss{
			{BuyDate: "2024-01-02", SellDate: "2024-03-01", StockID: "X", StockName: "n", PurchasePrice: 10, SellPrice: 12, InvestmentCost: 1000, Revenue: 1200.005, ProfitLoss: 200.004, ProfitRate: 20.006, Shares: 100},
		},
	}
	stock := newFakeStock()
	svc := newPortfolioService(ledger, stock)
	rows, err := svc.RealizedGainsLosses(context.Background())
	if err != nil {
		t.Fatalf("RealizedGainsLosses: %v", err)
	}
	r := rows[0]
	if r.Revenue != 1200.01 || r.ProfitLoss != 200.0 || r.ProfitRate != 20.01 {
		t.Fatalf("rounding wrong: revenue=%v pl=%v rate=%v", r.Revenue, r.ProfitLoss, r.ProfitRate)
	}
	if r.PurchasePrice != 10 || r.SellPrice != 12 || r.InvestmentCost != 1000 || r.Shares != 100 {
		t.Fatalf("passthrough fields wrong: %+v", r)
	}
}
