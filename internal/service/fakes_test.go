package service

import (
	"context"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// newTestLogger returns a logger that discards output so tests stay quiet.
func newTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

// fakeStock is an in-memory StockStore. Prices are keyed by stock_id (latest
// close); priceErr forces GetPriceAsOf to fail.
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

type insertedBar struct {
	stockID   string
	stockName string
	bar       entity.Bar
}

func newFakeStock() *fakeStock {
	return &fakeStock{
		names:      map[string]string{},
		prices:     map[string]float64{},
		priceErr:   map[string]error{},
		closesDesc: map[string][]float64{},
	}
}

func (f *fakeStock) GetStockName(_ context.Context, stockID string) (string, error) {
	if f.nameErr != nil {
		return "", f.nameErr
	}
	return f.names[stockID], nil
}

func (f *fakeStock) GetPriceAsOf(_ context.Context, stockID, _, _ string) (float64, error) {
	if err := f.priceErr[stockID]; err != nil {
		return 0, err
	}
	return f.prices[stockID], nil
}

func (f *fakeStock) GetClosePricesDescAsOf(_ context.Context, stockID, _ string) ([]float64, error) {
	return f.closesDesc[stockID], nil
}

func (f *fakeStock) GetCloseHistoryAsc(_ context.Context, stockID string) ([]entity.StockHistory, error) {
	if f.nameErr != nil { // reuse nameErr as a generic read-error switch in tests
		return nil, f.nameErr
	}
	return f.history[stockID], nil
}

func (f *fakeStock) InsertBarIgnore(_ context.Context, stockID, stockName string, b entity.Bar) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.insertedBars = append(f.insertedBars, insertedBar{stockID: stockID, stockName: stockName, bar: b})
	return nil
}

// fakeLedger is an in-memory LedgerStore. lots is the ordered queue returned by
// GetLowestUnrealized (cheapest first); callers pop via delete/update. It also
// records every mutating call for assertions.
type fakeLedger struct {
	lots     []entity.UnrealizedGainsLoss
	realized []entity.RealizedGainsLoss
	listU    []entity.UnrealizedGainsLoss
	listR    []entity.RealizedGainsLoss

	deletes []deleteCall
	updates []updateCall

	getLowestErr error
}

type deleteCall struct {
	stockID         string
	transactionDate string
}

type updateCall struct {
	stockID         string
	transactionDate string
	investmentCost  float64
	shares          int
}

func (f *fakeLedger) GetLowestUnrealized(_ context.Context, _, _ string) (entity.UnrealizedGainsLoss, bool, error) {
	if f.getLowestErr != nil {
		return entity.UnrealizedGainsLoss{}, false, f.getLowestErr
	}
	if len(f.lots) == 0 {
		return entity.UnrealizedGainsLoss{}, false, nil
	}
	return f.lots[0], true, nil
}

func (f *fakeLedger) InsertUnrealized(_ context.Context, e entity.UnrealizedGainsLoss) error {
	f.lots = append(f.lots, e)
	return nil
}

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

func (f *fakeLedger) InsertRealized(_ context.Context, e entity.RealizedGainsLoss) error {
	f.realized = append(f.realized, e)
	return nil
}

func (f *fakeLedger) ListUnrealized(_ context.Context) ([]entity.UnrealizedGainsLoss, error) {
	return f.listU, nil
}

func (f *fakeLedger) ListRealized(_ context.Context) ([]entity.RealizedGainsLoss, error) {
	return f.listR, nil
}

// fakeBackfill is an in-memory BackfillStore recording MarkComplete calls.
type fakeBackfill struct {
	completed map[string]map[string]bool // stockID -> set of months
	marked    []markCall
	markErr   error
}

type markCall struct {
	stockID string
	month   string
}

func newFakeBackfill() *fakeBackfill {
	return &fakeBackfill{completed: map[string]map[string]bool{}}
}

func (f *fakeBackfill) CompletedMonths(_ context.Context, stockID string) (map[string]bool, error) {
	if m, ok := f.completed[stockID]; ok {
		return m, nil
	}
	return map[string]bool{}, nil
}

func (f *fakeBackfill) MarkComplete(_ context.Context, stockID, month string) error {
	f.marked = append(f.marked, markCall{stockID: stockID, month: month})
	if f.markErr != nil {
		return f.markErr
	}
	return nil
}

// fakeFetcher is an in-memory MarketFetcher. months maps a "date" key to a
// canned result.
type fakeFetcher struct {
	bars      []entity.Bar
	stockName string
	err       error
	calls     []fetchCall
}

type fetchCall struct {
	date    string
	stockID string
}

func (f *fakeFetcher) FetchMonth(date, stockID string) ([]entity.Bar, string, error) {
	f.calls = append(f.calls, fetchCall{date: date, stockID: stockID})
	if f.err != nil {
		return nil, "", f.err
	}
	return f.bars, f.stockName, nil
}

// errFake is a sentinel error for the fakes.
var errFake = fmt.Errorf("fake error")
