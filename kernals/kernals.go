package kernals

import (
	"main/helper"
	"main/sqls"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// 用來確認過去一個月內有沒有買過與 stockID 同型的股票
func CheckIfBuy_TimeChecking(log *logrus.Logger, stockID string, Stocks_array []string) int {
	for _, trackStocks := range Stocks_array { // 依序取出每一個同型股票 id
		lastBuyTime, err := sqls.LastBuyTime(log, trackStocks) // 取得該股票的最後一次買入時間
		if err != nil {
			log.Error("LastBuyTime 發生錯誤:", err)
			return -1
		}

		if lastBuyTime == "" { // 如果沒有關於該型未實現損益紀錄
			log.Info("trackStocks: ", trackStocks, " 沒有未實現損益紀錄")
		} else {
			// lastBuyTime, err = helper.ROCToAD(lastBuyTime) // 將 lastBuyTime 轉換成西元年格式
			// if err != nil {
			// 	log.Error("ROCToAD 發生錯誤:", err)
			// 	return -1
			// }

			// 將 lastBuyTime 轉成 time.time 型態
			lastBuyTime, err := time.Parse("2006-01-02", lastBuyTime)
			if err != nil {
				log.Error("無法將 lastBuyTime 轉成 time.time:", err)
				return -1
			}
			log.Info("lastBuyTime: ", lastBuyTime)

			// 如果一個月內有買過
			if time.Now().Sub(lastBuyTime).Hours() < 720 {
				log.Info("過去一個月內有買過與 "+stockID+" 同型的股票: ", trackStocks, ", 買入時間: ", lastBuyTime)
				return 0
			}
		}
	}

	return 1
}

func CheckIfBuy_BuyPointChecking(log *logrus.Logger, stockID string) int {
	lowerPointDays := sqls.LowerPointDays(log, stockID)
	log.Info("stockID: ", stockID, " 目前落於近 ", lowerPointDays, " 天的低點")
	if lowerPointDays >= 30 { // 如果是近 30 天以上的低點
		return 1
	} else {
		log.Info("stockID: ", stockID, " 非近 30 天內的低點")
		return 0
	}
}

func AveragingUpAndDown(log *logrus.Logger, stockID string, buyAmount int) (int, error) {
	AverageStockPrice180, err := sqls.GetAverageStockPrice(log, stockID, 180)
	AverageStockPrice360, err := sqls.GetAverageStockPrice(log, stockID, 360)
	todayStockPrice, err := sqls.GetTodayStockPrice(log, stockID, "close_price")
	if err != nil {
		log.Error("GetAverageStockPrice 錯誤:")
		return -1, err
	}

	// 如果目前股價高於 180 日均價
	if todayStockPrice > AverageStockPrice180 {
		log.Info("stockID: ", stockID, " 目前股價高於 180 日均價")
		buyAmount = buyAmount - 2000 // 少買 2000
	} else {
		log.Info("stockID: ", stockID, " 目前股價低於 180 日均價")
		buyAmount = buyAmount + 3000 // 多買 3000
	}

	// 如果目前股價低於 360 日均價
	if todayStockPrice < AverageStockPrice360 {
		log.Info("stockID: ", stockID, " 目前股價低於 360 日均價")
		buyAmount = buyAmount + 2000 // 再多買 2000
	}

	return buyAmount, nil
}

func CheckIfBuy(log *logrus.Logger, stockID string, trackStocks_market_array []string, trackStocks_highDividend_array []string) int {
	// 確認過去一個月內是否有買過同類型的股票
	if helper.ValueInStringArray(stockID, trackStocks_market_array) == 1 { // 如果是市值型股票
		if CheckIfBuy_TimeChecking(log, stockID, trackStocks_market_array) != 1 { // 如果過去一個月內有買過同類型的股票
			return 0
		}
	} else if helper.ValueInStringArray(stockID, trackStocks_highDividend_array) == 1 { // 如果是高股息型股票
		if CheckIfBuy_TimeChecking(log, stockID, trackStocks_highDividend_array) != 1 { // 如果過去一個月內有買過同類型的股票
			return 0
		}
	} else {
		log.Error("stockID: ", stockID, " 不屬於市值型或高股息型股票")
		return -1
	}

	// 確認是否符合買點條件
	if CheckIfBuy_BuyPointChecking(log, stockID) != 1 {
		return 0
	}

	return 1
}

func BuyStock(log *logrus.Logger) {
	log.Info("BuyStock 開始執行")
	trackStocks_market_array := strings.Split(os.Getenv("TrackStocks_Market"), "&")
	trackStocks_highDividend_array := strings.Split(os.Getenv("TrackStocks_HighDividend"), "&")
	trackStocksArray := append(trackStocks_market_array, trackStocks_highDividend_array...)
	log.Info("TrackStocksArray: ", trackStocksArray)

	for _, stockID := range trackStocksArray { // 依序取出每一個股票 id
		if CheckIfBuy(log, stockID, trackStocks_market_array, trackStocks_highDividend_array) == 1 {
			buyAmount := 5000 // 基本買入金額
			buyAmount, err := AveragingUpAndDown(log, stockID, buyAmount)
			if err != nil {
				log.Error("AveragingUpAndDown 錯誤:", err)
				continue
			}
			log.Info("stockID: ", stockID, " 買入金額: ", buyAmount)
			err = sqls.SQLBuyStock(log, stockID, buyAmount)
			if err != nil {
				log.Error("SQLBuyStock 錯誤:", err)
				continue
			}
			log.Info("stockID: ", stockID, " 買入成功，買入金額: ", buyAmount)
		}
	}
}

func DailyCheck(log *logrus.Logger) {
	log.Info("DailyCheck 開始執行")
	taiwanTimeZone, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		log.Fatal("取得 taiwanTimeZone 時發生錯誤", err)
	}

	for {
		now := time.Now().In(taiwanTimeZone)
		if now.Hour() == 1 && now.Minute() == 1 {
			log.Info("現在時間: ", now)

			BuyStock(log)
			// SellStock(log)
		}
		BuyStock(log)

		time.Sleep(60 * time.Second)
	}
}
