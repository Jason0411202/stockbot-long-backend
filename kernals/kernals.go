package kernals

import (
	"fmt"
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

func CheckIfSell_TimeChecking(log *logrus.Logger, stockID string, Stocks_array []string) int {
	for _, trackStocks := range Stocks_array { // 依序取出每一個同型股票 id
		lastSellTime, err := sqls.LastSellTime(log, trackStocks) // 取得該股票的最後一次賣出時間
		if err != nil {
			log.Error("LastSellTime 發生錯誤:", err)
			return -1
		}

		if lastSellTime == "" { // 如果沒有關於該型已實現損益紀錄
			log.Info("trackStocks: ", trackStocks, " 沒有已實現損益紀錄")
		} else {
			// 將 lastSellTime 轉成 time.time 型態
			lastSellTime, err := time.Parse("2006-01-02", lastSellTime)
			if err != nil {
				log.Error("無法將 lastSellTime 轉成 time.time:", err)
				return -1
			}
			log.Info("lastSellTime: ", lastSellTime)

			// 如果一個月內有賣過
			if time.Now().Sub(lastSellTime).Hours() < 720 {
				log.Info("過去一個月內有賣過與 "+stockID+" 同型的股票: ", trackStocks, ", 賣出時間: ", lastSellTime)
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

func CheckIfSell_SellPointChecking(log *logrus.Logger, stockID string) int {
	upperPointDays := sqls.UpperPointDays(log, stockID)
	log.Info("stockID: ", stockID, " 目前落於近 ", upperPointDays, " 天的高點")
	if upperPointDays >= 90 { // 如果是近 90 天以上的高點
		return 1
	} else {
		log.Info("stockID: ", stockID, " 非近 90 天內的高點")
		return 0
	}

}

func AveragingUpAndDown(log *logrus.Logger, stockID string, buyAmount float64, action string) (float64, error) {
	AverageStockPrice180, err := sqls.GetAverageStockPrice(log, stockID, 180)
	AverageStockPrice360, err := sqls.GetAverageStockPrice(log, stockID, 360)
	todayStockPrice, err := sqls.GetTodayStockPrice(log, stockID, "close_price")
	if err != nil {
		log.Error("GetAverageStockPrice 錯誤:")
		return -1, err
	}

	if action == "buy" {
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
	} else if action == "sell" {
		// 如果目前股價低於 180 日均價
		if todayStockPrice < AverageStockPrice180 {
			log.Info("stockID: ", stockID, " 目前股價低於 180 日均價")
			buyAmount = buyAmount - 2000 // 少賣 2000
		}
		return buyAmount, nil
	}

	return -1, fmt.Errorf("action 參數錯誤")
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

func CheckIfSell(log *logrus.Logger, stockID string, trackStocks_market_array []string, trackStocks_highDividend_array []string) int {
	// 確認過去一個月內是否有賣過同類型的股票
	if helper.ValueInStringArray(stockID, trackStocks_market_array) == 1 { // 如果是市值型股票
		if CheckIfSell_TimeChecking(log, stockID, trackStocks_market_array) != 1 { // 如果過去一個月內有賣過同類型的股票
			return 0
		}
	} else if helper.ValueInStringArray(stockID, trackStocks_highDividend_array) == 1 { // 如果是高股息型股票
		if CheckIfSell_TimeChecking(log, stockID, trackStocks_highDividend_array) != 1 { // 如果過去一個月內有賣過同類型的股票
			return 0
		}
	} else {
		log.Error("stockID: ", stockID, " 不屬於市值型或高股息型股票")
		return -1
	}

	// 確認是否符合賣點條件
	if CheckIfSell_SellPointChecking(log, stockID) != 1 {
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
			buyAmount := 0.0
			if os.Getenv("Scaling_Strategy") == "AverageLine" { // 採用均線策略
				log.Info("stockID: ", stockID, " 採用均線策略")
				buyAmount = 5000.0 // 基本買入金額
				afterUpAndDown, err := AveragingUpAndDown(log, stockID, buyAmount, "buy")
				if err != nil {
					log.Error("AveragingUpAndDown 錯誤:", err)
					continue
				}

				buyAmount = afterUpAndDown
			} else if os.Getenv("Scaling_Strategy") == "Pyramid" { // 採用金字塔策略
				log.Info("stockID: ", stockID, " 採用金字塔策略")
				todayPrice, err := sqls.GetTodayStockPrice(log, stockID, "close_price")
				if err != nil {
					log.Error("GetTodayStockPrice 錯誤:", err)
					continue
				}

				HighestPrice, err := sqls.GetTransactionPriceOfUnrealizedGainsLosses(log, stockID, "Highest")
				percentages := 0.0
				if err != nil {
					log.Error("GetBuyPriceOfUnrealizedGainsLosses 錯誤:", err)
					continue
				}

				if HighestPrice != -1 {
					percentages = (todayPrice - HighestPrice) / HighestPrice
				}
				log.Info("stockID: ", stockID, " 今日股價: ", todayPrice, " 最高價: ", HighestPrice, " 與最高價之相對比例: ", percentages)

				if percentages > -0.15 {
					buyAmount = 3000.0
				} else if percentages > -0.3 {
					buyAmount = 4500.0
				} else if percentages > -0.45 {
					buyAmount = 6000.0
				} else if percentages > -0.6 {
					buyAmount = 7500.0
				} else {
					buyAmount = 9000.0
				}
			}

			log.Info("stockID: ", stockID, " 買入金額: ", buyAmount)
			err := sqls.SQLBuyStock(log, stockID, buyAmount)
			if err != nil {
				log.Error("SQLBuyStock 錯誤:", err)
				continue
			}
			log.Info("stockID: ", stockID, " 買入成功，買入金額: ", buyAmount)
		}
	}
}

func SellStock(log *logrus.Logger) {
	log.Info("SellStock 開始執行")
	trackStocks_market_array := strings.Split(os.Getenv("TrackStocks_Market"), "&")
	trackStocks_highDividend_array := strings.Split(os.Getenv("TrackStocks_HighDividend"), "&")
	trackStocksArray := append(trackStocks_market_array, trackStocks_highDividend_array...)
	log.Info("TrackStocksArray: ", trackStocksArray)

	for _, stockID := range trackStocksArray { // 依序取出每一個股票 id
		if CheckIfSell(log, stockID, trackStocks_market_array, trackStocks_highDividend_array) == 1 || 1 == 1 {
			sellAmount := 0.0
			if os.Getenv("Scaling_Strategy") == "AverageLine" { // 採用均線策略
				log.Info("stockID: ", stockID, " 採用均線策略")
				sellAmount = 5000.0                                                         // 基本賣出金額
				afterUpAndDown, err := AveragingUpAndDown(log, stockID, sellAmount, "sell") // 調整賣出金額
				if err != nil {
					log.Error("AveragingUpAndDown 錯誤:", err)
					continue
				}

				sellAmount = afterUpAndDown
			} else if os.Getenv("Scaling_Strategy") == "Pyramid" { // 採用金字塔策略
				log.Info("stockID: ", stockID, " 採用金字塔策略")
				todayPrice, err := sqls.GetTodayStockPrice(log, stockID, "close_price")
				if err != nil {
					log.Error("GetTodayStockPrice 錯誤:", err)
					continue
				}

				LowestPrice, err := sqls.GetTransactionPriceOfUnrealizedGainsLosses(log, stockID, "Lowest")
				percentages := 0.0
				if err != nil {
					log.Error("GetBuyPriceOfUnrealizedGainsLosses 錯誤:", err)
					continue
				}

				if LowestPrice != -1 {
					percentages = (todayPrice - LowestPrice) / LowestPrice
				}
				log.Info("stockID: ", stockID, " 今日股價: ", todayPrice, " 最低價: ", LowestPrice, " 與最低價之相對比例: ", percentages)

				if percentages < 0.15 {
					sellAmount = 3000.0
				} else if percentages < 0.3 {
					sellAmount = 4500.0
				} else if percentages < 0.45 {
					sellAmount = 6000.0
				} else if percentages < 0.6 {
					sellAmount = 7500.0
				} else {
					sellAmount = 9000.0
				}
			}

			log.Info("stockID: ", stockID, " 預計賣出金額: ", sellAmount)
			err := sqls.SQLSellStock(log, stockID, sellAmount)
			if err != nil {
				log.Error("SQLSellStock 錯誤:", err)
				continue
			}
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
		if now.Hour() == 14 && now.Minute() == 0 {
			log.Info("現在時間: ", now)
			err = sqls.UpdataDatebase(log) // 先更新資料庫
			if err != nil {
				log.Error("UpdataDatebase 錯誤:")
			} else {
				BuyStock(log)
				SellStock(log)
			}
		}
		//BuyStock(log)
		SellStock(log)

		time.Sleep(60 * time.Second)
	}
}
