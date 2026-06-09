// internal/service/fakes_test.go 定義測試用的記憶體假實作，供各服務測試共用。
package service

import (
	"context"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// newTestLogger 回傳一個將輸出丟棄的 logger，使測試輸出保持乾淨。
func newTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

// fakeStock 模擬 StockStore，以記憶體 map 儲存股票名稱、收盤價及歷史資料，供測試注入。
type fakeStock struct {
	names      map[string]string
	prices     map[string]float64
	priceErr   map[string]error
	closesDesc map[string][]float64
	history    map[string][]entity.StockHistory
	nameErr    error
	insertErr  error

	insertedBars []insertedBar
}

// insertedBar 記錄一次 InsertBarIgnore 呼叫的參數，供斷言使用。
type insertedBar struct {
	stockID   string
	stockName string
	bar       entity.Bar
}

// newFakeStock 建立並回傳已初始化欄位的 fakeStock 實例。
func newFakeStock() *fakeStock {
	return &fakeStock{
		names:      map[string]string{},
		prices:     map[string]float64{},
		priceErr:   map[string]error{},
		closesDesc: map[string][]float64{},
	}
}

// GetStockName 回傳指定股票代碼的名稱，nameErr 非 nil 時回傳錯誤。
func (f *fakeStock) GetStockName(_ context.Context, stockID string) (string, error) {
	if f.nameErr != nil {
		return "", f.nameErr
	}
	return f.names[stockID], nil
}

// GetPriceAsOf 回傳指定股票的收盤價，priceErr 有設定時回傳對應錯誤。
func (f *fakeStock) GetPriceAsOf(_ context.Context, stockID, _, _ string) (float64, error) {
	if err := f.priceErr[stockID]; err != nil {
		return 0, err
	}
	return f.prices[stockID], nil
}

// GetClosePricesDescAsOf 回傳指定股票由新到舊排列的收盤價序列。
func (f *fakeStock) GetClosePricesDescAsOf(_ context.Context, stockID, _ string) ([]float64, error) {
	return f.closesDesc[stockID], nil
}

// GetCloseHistoryAsc 回傳指定股票由舊到新的歷史收盤記錄，nameErr 非 nil 時作為通用讀取錯誤。
func (f *fakeStock) GetCloseHistoryAsc(_ context.Context, stockID string) ([]entity.StockHistory, error) {
	if f.nameErr != nil { // reuse nameErr as a generic read-error switch in tests
		return nil, f.nameErr
	}
	return f.history[stockID], nil
}

// InsertBarIgnore 記錄插入的 K 線資料，insertErr 非 nil 時回傳錯誤。
func (f *fakeStock) InsertBarIgnore(_ context.Context, stockID, stockName string, b entity.Bar) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.insertedBars = append(f.insertedBars, insertedBar{stockID: stockID, stockName: stockName, bar: b})
	return nil
}

// fakeLedger 模擬 LedgerStore，以有序佇列維護未實現損益批次，並記錄所有變更操作供斷言使用。
type fakeLedger struct {
	lots     []entity.UnrealizedGainsLoss
	realized []entity.RealizedGainsLoss
	listU    []entity.UnrealizedGainsLoss
	listR    []entity.RealizedGainsLoss

	deletes []deleteCall
	updates []updateCall

	getLowestErr error
}

// deleteCall 記錄一次 DeleteUnrealized 呼叫的股票代碼與交易日期。
type deleteCall struct {
	stockID         string
	transactionDate string
}

// updateCall 記錄一次 UpdateUnrealized 呼叫的參數，供部分賣出斷言使用。
type updateCall struct {
	stockID         string
	transactionDate string
	investmentCost  float64
	shares          int
}

// GetLowestUnrealized 回傳佇列中的第一筆未實現損益批次，佇列為空時回傳 false。
func (f *fakeLedger) GetLowestUnrealized(_ context.Context, _, _ string) (entity.UnrealizedGainsLoss, bool, error) {
	if f.getLowestErr != nil {
		return entity.UnrealizedGainsLoss{}, false, f.getLowestErr
	}
	if len(f.lots) == 0 {
		return entity.UnrealizedGainsLoss{}, false, nil
	}
	return f.lots[0], true, nil
}

// InsertUnrealized 將未實現損益批次附加至 lots 佇列。
func (f *fakeLedger) InsertUnrealized(_ context.Context, e entity.UnrealizedGainsLoss) error {
	f.lots = append(f.lots, e)
	return nil
}

// DeleteUnrealized 記錄刪除呼叫並從 lots 佇列中移除對應批次。
func (f *fakeLedger) DeleteUnrealized(_ context.Context, stockID, transactionDate string) error {
	f.deletes = append(f.deletes, deleteCall{stockID: stockID, transactionDate: transactionDate})
	// Drop the matching lot from the queue (the head, in practice).
	out := f.lots[:0]
	removed := false
	for _, lot := range f.lots {
		if !removed && lot.StockID == stockID && lot.TransactionDate == transactionDate {
			removed = true
			continue
		}
		out = append(out, lot)
	}
	f.lots = out
	return nil
}

// UpdateUnrealized 記錄更新呼叫並就地修改 lots 佇列中對應批次的成本與股數。
func (f *fakeLedger) UpdateUnrealized(_ context.Context, stockID, transactionDate string, investmentCost float64, shares int) error {
	f.updates = append(f.updates, updateCall{stockID: stockID, transactionDate: transactionDate, investmentCost: investmentCost, shares: shares})
	for i := range f.lots {
		if f.lots[i].StockID == stockID && f.lots[i].TransactionDate == transactionDate {
			f.lots[i].InvestmentCost = investmentCost
			f.lots[i].Shares = shares
			break
		}
	}
	return nil
}

// InsertRealized 將已實現損益記錄附加至 realized 切片。
func (f *fakeLedger) InsertRealized(_ context.Context, e entity.RealizedGainsLoss) error {
	f.realized = append(f.realized, e)
	return nil
}

// ListUnrealized 回傳預設的未實現損益清單 listU。
func (f *fakeLedger) ListUnrealized(_ context.Context) ([]entity.UnrealizedGainsLoss, error) {
	return f.listU, nil
}

// ListRealized 回傳預設的已實現損益清單 listR。
func (f *fakeLedger) ListRealized(_ context.Context) ([]entity.RealizedGainsLoss, error) {
	return f.listR, nil
}

// fakeBackfill 模擬 BackfillStore，記錄 MarkComplete 呼叫並以記憶體 map 維護已完成月份。
type fakeBackfill struct {
	completed map[string]map[string]bool // stockID -> set of months
	marked    []markCall
	markErr   error
}

// markCall 記錄一次 MarkComplete 呼叫的股票代碼與月份字串。
type markCall struct {
	stockID string
	month   string
}

// newFakeBackfill 建立並回傳已初始化的 fakeBackfill 實例。
func newFakeBackfill() *fakeBackfill {
	return &fakeBackfill{completed: map[string]map[string]bool{}}
}

// CompletedMonths 回傳指定股票已完成補資料的月份集合。
func (f *fakeBackfill) CompletedMonths(_ context.Context, stockID string) (map[string]bool, error) {
	if m, ok := f.completed[stockID]; ok {
		return m, nil
	}
	return map[string]bool{}, nil
}

// MarkComplete 記錄標記完成的呼叫，markErr 非 nil 時回傳錯誤。
func (f *fakeBackfill) MarkComplete(_ context.Context, stockID, month string) error {
	f.marked = append(f.marked, markCall{stockID: stockID, month: month})
	if f.markErr != nil {
		return f.markErr
	}
	return nil
}

// fakeFetcher 模擬 MarketFetcher，回傳預設的 K 線資料並記錄每次 FetchMonth 呼叫。
type fakeFetcher struct {
	bars      []entity.Bar
	stockName string
	err       error
	calls     []fetchCall
}

// fetchCall 記錄一次 FetchMonth 呼叫的日期與股票代碼。
type fetchCall struct {
	date    string
	stockID string
}

// FetchMonth 記錄呼叫並回傳預設的 K 線切片與股票名稱，err 非 nil 時回傳錯誤。
func (f *fakeFetcher) FetchMonth(date, stockID string) ([]entity.Bar, string, error) {
	f.calls = append(f.calls, fetchCall{date: date, stockID: stockID})
	if f.err != nil {
		return nil, "", f.err
	}
	return f.bars, f.stockName, nil
}

// fakeRealtime 模擬 RealtimeFetcher，回傳預設的即時開盤價 map 供線上開盤決策測試使用。
type fakeRealtime struct {
	opens map[string]float64
	err   error
	calls int
}

// FetchOpens 回傳預設的開盤價 map，err 非 nil 時回傳錯誤,並記錄呼叫次數。
func (f *fakeRealtime) FetchOpens(_ context.Context, _ []string) (map[string]float64, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.opens, nil
}

// fakeEquity 模擬 EquityStore，記錄每次 RecordEquity 寫入的快照供斷言，並可回傳預設清單供讀取測試。
type fakeEquity struct {
	recorded []entity.EquitySnapshot
	list     []entity.EquitySnapshot
	recErr   error
	listErr  error
}

// newFakeEquity 建立並回傳已初始化的 fakeEquity 實例。
func newFakeEquity() *fakeEquity {
	return &fakeEquity{}
}

// RecordEquity 記錄寫入的權益快照，recErr 非 nil 時回傳錯誤。
func (f *fakeEquity) RecordEquity(_ context.Context, snap entity.EquitySnapshot) error {
	if f.recErr != nil {
		return f.recErr
	}
	f.recorded = append(f.recorded, snap)
	return nil
}

// ListEquityAsc 回傳預設的每日權益快照清單，listErr 非 nil 時回傳錯誤。
func (f *fakeEquity) ListEquityAsc(_ context.Context) ([]entity.EquitySnapshot, error) {
	return f.list, f.listErr
}

// errFake is a sentinel error for the fakes.
var errFake = fmt.Errorf("fake error")
