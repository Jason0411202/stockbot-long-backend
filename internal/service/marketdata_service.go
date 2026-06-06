package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
)

// fetchSleep is the inter-fetch courtesy delay preserved from sqls.go
// (time.Sleep(3 * time.Second) between TWSE calls).
const fetchSleep = 3 * time.Second

// MarketDataService refreshes the StockHistory table from TWSE. It reproduces
// UpdataDatebase (sqls.go 72-119), updateDatabaseWithMonths (231-271) and
// fetchAndInsertMonth (276-300), orchestrating the MarketFetcher, StockStore and
// BackfillStore ports.
type MarketDataService struct {
	twse     MarketFetcher
	stock    StockStore
	backfill BackfillStore
	cfg      *config.Config
	log      *logrus.Logger
}

// NewMarketDataService wires a MarketDataService to its ports and config.
func NewMarketDataService(twse MarketFetcher, stock StockStore, backfill BackfillStore, cfg *config.Config, log *logrus.Logger) *MarketDataService {
	return &MarketDataService{twse: twse, stock: stock, backfill: backfill, cfg: cfg, log: log}
}

// monthlyBackfillDates walks back months months from currentDate ("YYYYMMDD"),
// taking day=1 each step, newest-first. Ported verbatim from sqls.go 123-138
// (pure, no I/O).
func monthlyBackfillDates(currentDate string, months int) []string {
	dates := make([]string, 0, months+1)
	dates = append(dates, currentDate)

	t, err := time.Parse("20060102", currentDate)
	if err != nil {
		return dates
	}
	for i := 0; i < months; i++ {
		t = t.AddDate(0, -1, 0)
		firstOfMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		dates = append(dates, firstOfMonth.Format("20060102"))
	}
	return dates
}

// dateToYearMonth converts "YYYYMMDD" to "YYYY-MM". Ported verbatim from
// sqls.go 350-357 (pure, no I/O).
func dateToYearMonth(date string) (string, error) {
	t, err := time.Parse("20060102", date)
	if err != nil {
		return "", fmt.Errorf("parse date %s: %w", date, err)
	}
	return t.Format("2006-01"), nil
}

// UpdateDatabase performs the daily refresh, porting UpdataDatebase (72-119):
// always re-fetch the current month, and allow previous-month overwrite, across
// every tracked stock and each monthly-backfill date. Sleeps 3s between fetches.
func (s *MarketDataService) UpdateDatabase(ctx context.Context) error {
	now := time.Now()
	currentDate := now.Format("20060102")
	s.log.Info("currentDate: ", currentDate)

	maxBackMonths := s.cfg.MaxBackMonths
	if maxBackMonths < 0 {
		maxBackMonths = 1
	}

	dates := monthlyBackfillDates(currentDate, maxBackMonths)
	s.log.Info("Dates: ", dates)

	currentMonth := now.Format("2006-01")

	for _, stockID := range s.cfg.TrackStocks {
		for _, date := range dates {
			ym, err := dateToYearMonth(date)
			if err != nil {
				s.log.Error("dateToYearMonth 錯誤: ", err)
				continue
			}
			// 每日 daily 一律重抓 (currentMonth 必抓;previous month 也允許覆蓋)。
			if err := s.fetchAndInsertMonth(ctx, stockID, date, ym, currentMonth); err != nil {
				s.log.Error("fetchAndInsertMonth 錯誤: ", err)
				break
			}
			time.Sleep(fetchSleep)
		}
	}
	return nil
}

// BackfillMonths performs the init-path refresh, porting updateDatabaseWithMonths
// (231-271): per stock it loads completed months first and skips any completed
// month unless it is the current month; on any month error it stops that stock's
// remaining months. Sleeps 3s between fetches.
func (s *MarketDataService) BackfillMonths(ctx context.Context, months int) error {
	currentDate := time.Now().Format("20060102")
	dates := monthlyBackfillDates(currentDate, months)
	s.log.Info("Init Dates: ", dates)

	currentMonth := time.Now().Format("2006-01")

	for _, stockID := range s.cfg.TrackStocks {
		completedMonths, err := s.backfill.CompletedMonths(ctx, stockID)
		if err != nil {
			return fmt.Errorf("CompletedMonths(%s) 失敗: %w", stockID, err)
		}

		for _, date := range dates {
			ym, err := dateToYearMonth(date)
			if err != nil {
				s.log.Error("dateToYearMonth 錯誤: ", err)
				continue
			}
			if ym != currentMonth && completedMonths[ym] {
				s.log.Infof("%s 月份 %s 已標記完成,跳過 TWSE API 呼叫", stockID, ym)
				continue
			}

			if err := s.fetchAndInsertMonth(ctx, stockID, date, ym, currentMonth); err != nil {
				s.log.Error("fetchAndInsertMonth 錯誤: ", err)
				break // 該股票後續月份直接停止,避免持續打 API 失敗
			}
			time.Sleep(fetchSleep)
		}
	}
	return nil
}

// fetchAndInsertMonth fetches one month for stockID and INSERT IGNOREs every
// bar; on success and only when ym is not the current month it marks the month
// complete (a mark failure is logged but non-fatal). Ports fetchAndInsertMonth
// (276-300); the per-bar ROC→AD conversion is now done inside the TWSE client.
func (s *MarketDataService) fetchAndInsertMonth(ctx context.Context, stockID, date, ym, currentMonth string) error {
	bars, stockName, err := s.twse.FetchMonth(date, stockID)
	if err != nil {
		return fmt.Errorf("FetchMonth(%s, %s) 失敗: %w", stockID, date, err)
	}

	for _, bar := range bars {
		if err := s.stock.InsertBarIgnore(ctx, stockID, stockName, bar); err != nil {
			return fmt.Errorf("InsertBarIgnore(%s, %s) 失敗: %w", stockID, bar.Date, err)
		}
	}

	// 整月成功才標記;當月不標,因為當月仍有未到的交易日。
	if ym != currentMonth {
		if err := s.backfill.MarkComplete(ctx, stockID, ym); err != nil {
			s.log.Warn("MarkComplete 失敗 (不致命,下次會重抓): ", err)
		}
	}
	return nil
}
