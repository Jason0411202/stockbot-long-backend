// internal/service/marketdata_service.go 負責 TWSE 月資料回補與每日資料更新。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/config"
)

// fetchSleep 是相鄰兩次 TWSE API 呼叫之間的禮貌性等待時間。
const fetchSleep = 3 * time.Second

// MarketDataService 負責從 TWSE 抓取月線資料並寫入 StockHistory 資料表。
// 它編排 MarketFetcher、StockStore 與 BackfillStore 三個 port，
// 不持有任何 SQL，所有資料存取皆透過 port 介面完成。
type MarketDataService struct {
	twse     MarketFetcher
	stock    StockStore
	backfill BackfillStore
	cfg      *config.Config
	log      *logrus.Logger
}

// NewMarketDataService 建立並回傳一個已完成依賴注入的 MarketDataService。
func NewMarketDataService(twse MarketFetcher, stock StockStore, backfill BackfillStore, cfg *config.Config, log *logrus.Logger) *MarketDataService {
	return &MarketDataService{twse: twse, stock: stock, backfill: backfill, cfg: cfg, log: log}
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
	now := time.Now()
	currentDate := now.Format("20060102")
	s.log.Info("currentDate: ", currentDate)

	// 取得回補月數設定，最低不得為負值。
	maxBackMonths := s.cfg.MaxBackMonths
	if maxBackMonths < 0 {
		maxBackMonths = 1
	}

	dates := monthlyBackfillDates(currentDate, maxBackMonths)
	s.log.Info("Dates: ", dates)

	currentMonth := now.Format("2006-01")

	// 對每檔追蹤股票、每個月份日期依序執行抓取與寫入。
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

// BackfillMonths 執行初始化路徑的歷史回補：對每檔追蹤股票先讀取已完成月份清單，
// 跳過已完成且非當月的月份，其餘月份依序抓取並寫入。任一月份發生錯誤即停止該股票後續月份的抓取。
// 每次抓取間隔 3 秒。
func (s *MarketDataService) BackfillMonths(ctx context.Context, months int) error {
	currentDate := time.Now().Format("20060102")
	dates := monthlyBackfillDates(currentDate, months)
	s.log.Info("Init Dates: ", dates)

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
				s.log.Error("dateToYearMonth 錯誤: ", err)
				continue
			}
			// 已完成且非當月的月份直接跳過，避免重複呼叫 TWSE API。
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
			s.log.Warn("MarkComplete 失敗 (不致命,下次會重抓): ", err)
		}
	}
	return nil
}
