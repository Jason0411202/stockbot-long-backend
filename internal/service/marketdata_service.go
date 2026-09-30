// internal/service/marketdata_service.go 負責 TWSE 月資料回補與每日資料更新。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
)

// fetchSleep 是相鄰兩次 TWSE API 呼叫之間的禮貌性等待時間。
const fetchSleep = 3 * time.Second

// UpdateDatabaseSince includes the entire outage, even across several months.
func (s *MarketDataService) UpdateDatabaseSince(ctx context.Context, from, through time.Time) error {
	copyService := *s
	cfg := *s.cfg
	if !from.IsZero() {
		months := (through.Year()-from.Year())*12 + int(through.Month()-from.Month())
		if months > cfg.MaxBackMonths {
			cfg.MaxBackMonths = months
		}
	}
	copyService.cfg = &cfg
	return copyService.UpdateDatabase(ctx)
}

// MarketDataService 負責從 TWSE 抓取月線資料並寫入 StockHistory 資料表。
// 它編排 MarketFetcher、StockStore 與 BackfillStore 三個 port，
// 不持有任何 SQL，所有資料存取皆透過 port 介面完成。
type MarketDataService struct {
	twse     MarketFetcher
	stock    StockStore
	backfill BackfillStore
	cfg      *config.Config
	log      *logrus.Entry
}

// NewMarketDataService 建立並回傳一個已完成依賴注入的 MarketDataService。
func NewMarketDataService(twse MarketFetcher, stock StockStore, backfill BackfillStore, cfg *config.Config, log *logrus.Logger) *MarketDataService {
	return &MarketDataService{twse: twse, stock: stock, backfill: backfill, cfg: cfg, log: log.WithField("component", "market_data")}
}

// monthlyBackfillDates 以 currentDate（"YYYYMMDD"）為起點，往前推算 months 個月的日期清單，
// 每個月取該月第 1 日，結果由新到舊排列。此為純計算函式，不含任何 I/O。
func monthlyBackfillDates(currentDate string, months int) []string {
	dates := make([]string, 0, months+1)
	dates = append(dates, currentDate)

	t, err := time.Parse("20060102", currentDate)
	if err != nil {
		return dates
	}
	t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	// 逐月往前推，每次取該月第 1 日格式化後加入清單。
	for i := 0; i < months; i++ {
		t = t.AddDate(0, -1, 0)
		firstOfMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		dates = append(dates, firstOfMonth.Format("20060102"))
	}
	return dates
}

// dateToYearMonth 將 "YYYYMMDD" 字串轉換為 "YYYY-MM" 格式。此為純計算函式，不含任何 I/O。
func dateToYearMonth(date string) (string, error) {
	t, err := time.Parse("20060102", date)
	if err != nil {
		return "", fmt.Errorf("parse date %s: %w", date, err)
	}
	return t.Format("2006-01"), nil
}

// UpdateDatabase 執行每日資料更新：對所有追蹤股票的每個月份日期，一律重抓 TWSE 資料並寫入。
// 當月資料必抓；前月資料也允許覆蓋（以修正尚未完整的資料）。每次抓取間隔 3 秒。
func (s *MarketDataService) UpdateDatabase(ctx context.Context) error {
	if units, ok := s.stock.(interface{ RefreshUnits(context.Context) error }); ok {
		if err := units.RefreshUnits(ctx); err != nil {
			return err
		}
	}
	now := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60))
	currentDate := now.Format("20060102")
	s.log.WithField("current_date", currentDate).Info("開始每日資料更新")

	// 取得回補月數設定，最低不得為負值。
	maxBackMonths := s.cfg.MaxBackMonths
	if maxBackMonths < 0 {
		maxBackMonths = 1
	}

	dates := monthlyBackfillDates(currentDate, maxBackMonths)
	s.log.WithField("dates", dates).Info("每日更新回補月份清單")

	currentMonth := now.Format("2006-01")

	var failures []error
	// 對每檔追蹤股票、每個月份日期依序執行抓取與寫入。
	for _, stockID := range s.cfg.TrackStocks {
		for _, date := range dates {
			ym, err := dateToYearMonth(date)
			if err != nil {
				s.log.WithError(err).WithField("date", date).Error("dateToYearMonth 錯誤")
				continue
			}
			// 每日 daily 一律重抓 (currentMonth 必抓;previous month 也允許覆蓋)。
			if err := s.fetchAndInsertMonth(ctx, stockID, date, ym, currentMonth); err != nil {
				s.log.WithError(err).WithFields(logrus.Fields{"stock_id": stockID, "month": ym}).Error("fetchAndInsertMonth 錯誤")
				failures = append(failures, err)
				break
			}
			if err := waitMarketFetch(ctx); err != nil {
				return err
			}
		}
	}
	return errors.Join(failures...)
}

// BackfillMonths 執行初始化路徑的歷史回補：對每檔追蹤股票先讀取已完成月份清單，
// 跳過已完成且非當月的月份，其餘月份依序抓取並寫入。任一月份發生錯誤即停止該股票後續月份的抓取。
// 每次抓取間隔 3 秒。
func (s *MarketDataService) BackfillMonths(ctx context.Context, months int) error {
	currentDate := time.Now().Format("20060102")
	dates := monthlyBackfillDates(currentDate, months)
	s.log.WithField("dates", dates).Info("初始回補月份清單")

	currentMonth := time.Now().Format("2006-01")

	// 對每檔追蹤股票執行回補流程。
	for _, stockID := range s.cfg.TrackStocks {
		completedMonths, err := s.backfill.CompletedMonths(ctx, stockID)
		if err != nil {
			return fmt.Errorf("CompletedMonths(%s) 失敗: %w", stockID, err)
		}

		for _, date := range dates {
			ym, err := dateToYearMonth(date)
			if err != nil {
				s.log.WithError(err).WithField("date", date).Error("dateToYearMonth 錯誤")
				continue
			}
			// 已完成且非當月的月份直接跳過，避免重複呼叫 TWSE API。
			if ym != currentMonth && completedMonths[ym] {
				s.log.WithFields(logrus.Fields{"stock_id": stockID, "month": ym}).Info("月份已標記完成,跳過 TWSE API 呼叫")
				continue
			}

			if err := s.fetchAndInsertMonth(ctx, stockID, date, ym, currentMonth); err != nil {
				s.log.WithError(err).WithFields(logrus.Fields{"stock_id": stockID, "month": ym}).Error("fetchAndInsertMonth 錯誤")
				break // 該股票後續月份直接停止,避免持續打 API 失敗
			}
			time.Sleep(fetchSleep)
		}
	}
	return nil
}

// fetchAndInsertMonth 抓取指定股票單一月份的 TWSE 資料，逐筆執行 INSERT IGNORE 寫入；
// 整月成功且非當月時，將該月份標記為已完成（標記失敗為非致命警告，下次會重抓）。
func (s *MarketDataService) fetchAndInsertMonth(ctx context.Context, stockID, date, ym, currentMonth string) error {
	bars, stockName, err := s.twse.FetchMonth(date, stockID)
	if err != nil {
		return fmt.Errorf("FetchMonth(%s, %s) 失敗: %w", stockID, date, err)
	}

	// 逐筆寫入，採 INSERT IGNORE 以避免重複資料導致錯誤。
	for _, bar := range bars {
		if err := s.stock.InsertBarIgnore(ctx, stockID, stockName, bar); err != nil {
			return fmt.Errorf("InsertBarIgnore(%s, %s) 失敗: %w", stockID, bar.Date, err)
		}
	}

	// 整月成功才標記;當月不標,因為當月仍有未到的交易日。
	if ym != currentMonth {
		if err := s.backfill.MarkComplete(ctx, stockID, ym); err != nil {
			s.log.WithError(err).WithFields(logrus.Fields{"stock_id": stockID, "month": ym}).Warn("MarkComplete 失敗 (不致命,下次會重抓)")
		}
	}
	return nil
}

func waitMarketFetch(ctx context.Context) error {
	t := time.NewTimer(fetchSleep)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
