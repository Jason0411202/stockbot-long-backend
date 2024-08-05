package kernals

import (
	"fmt"
	"main/helper"
	"main/sqls"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// 用來確認過去一個月內有沒有買過與 stockID 同型的股票
func CheckIfBuy_TimeChecking(log *logrus.Logger, stockID string, Stocks_array []string, today string) int {
	for _, trackStocks := range Stocks_array { // 依序取出每一個同型股票 id
		lastBuyTime, err := sqls.LastBuyTime(log, trackStocks, today) // 取得該股票的最後一次買入時間
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

			today, err := time.Parse("2006-01-02", today)
			if err != nil {
				log.Error("無法將 today 轉成 time.time:", err)
				return -1
			}
			log.Info("today: ", today)

			// 如果一個月內有買過
			if today.Sub(lastBuyTime).Hours() < 720 && today.Sub(lastBuyTime).Hours() >= 0 {
				log.Info("過去一個月內有買過與 "+stockID+" 同型的股票: ", trackStocks, ", 買入時間: ", lastBuyTime)
				return 0
			}
		}
	}

	return 1
}

func CheckIfSell_TimeChecking(log *logrus.Logger, stockID string, Stocks_array []string, today string) int {
	for _, trackStocks := range Stocks_array { // 依序取出每一個同型股票 id
		lastSellTime, err := sqls.LastSellTime(log, trackStocks, today) // 取得該股票的最後一次賣出時間
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

			today, err := time.Parse("2006-01-02", today)
			if err != nil {
				log.Error("無法將 today 轉成 time.time:", err)
				return -1
			}
			log.Info("today: ", today)

			// 如果一個月內有賣過
			if today.Sub(lastSellTime).Hours() < 720 && today.Sub(lastSellTime).Hours() >= 0 {
				log.Info("過去一個月內有賣過與 "+stockID+" 同型的股票: ", trackStocks, ", 賣出時間: ", lastSellTime)
				return 0
			}
		}
	}

	return 1

}

func CheckIfBuy_BuyPointChecking(log *logrus.Logger, stockID string, today string) int {
	// lowerPointDays := sqls.LowerPointDays(log, stockID, today)
	// log.Info("stockID: ", stockID, " 目前落於近 ", lowerPointDays, " 天的低點")
	// if lowerPointDays >= 30 { // 如果是近 30 天以上的低點
	// 	return 1
	// } else {
	// 	log.Info("stockID: ", stockID, " 非近 30 天內的低點")
	// 	return 0
	// }

	AverageStockPrice20, err := sqls.GetAverageStockPrice(log, stockID, today, 20) // 取得 20 日均價
	if err != nil {
		log.Error("GetAverageStockPrice 錯誤:", err)
		return -1
	}

	todayPrice, err := sqls.GetTodayStockPrice(log, stockID, today, "close_price") // 取得今日股價
	if err != nil {
		log.Error("GetTodayStockPrice 錯誤:", err)
		return -1
	}

	if todayPrice >= AverageStockPrice20 { // 如果今日股價高於 20 日均價，則不買
		log.Info("stockID: ", stockID, " 目前股價高於 20 日均價")
		return 0
	} else { // 如果今日股價低於 20 日均價，則買
		log.Info("stockID: ", stockID, " 目前股價低於 20 日均價")
		return 1
	}
}

func CheckIfSell_SellPointChecking(log *logrus.Logger, stockID string, today string) int {
	upperPointDays := sqls.UpperPointDays(log, stockID, today)
	log.Info("stockID: ", stockID, " 目前落於近 ", upperPointDays, " 天的高點")
	if upperPointDays >= 90 { // 如果是近 90 天以上的高點
		return 1
	} else {
		log.Info("stockID: ", stockID, " 非近 90 天內的高點")
		return 0
	}

}

func AveragingUpAndDown(log *logrus.Logger, stockID string, buyAmount float64, today string, action string) (float64, error) {
	AverageStockPrice180, _ := sqls.GetAverageStockPrice(log, stockID, today, 180)
	AverageStockPrice360, _ := sqls.GetAverageStockPrice(log, stockID, today, 360)
	todayStockPrice, err := sqls.GetTodayStockPrice(log, stockID, today, "close_price")
	if err != nil {
		log.Error("GetAverageStockPrice 錯誤:")
		return -1, err
	}

	if action == "buy" {
		// 如果目前股價高於 180 日均價
		if AverageStockPrice180 == -1 {
			log.Error("stockID: ", stockID, " 資料無法計算出 180 日均價")
		} else if todayStockPrice > AverageStockPrice180 {
			log.Info("stockID: ", stockID, " 目前股價高於 180 日均價")
			buyAmount = buyAmount - 2000 // 少買 2000
		} else {
			log.Info("stockID: ", stockID, " 目前股價低於 180 日均價")
			buyAmount = buyAmount + 3000 // 多買 3000
		}

		// 如果目前股價低於 360 日均價
		if AverageStockPrice360 == -1 {
			log.Error("stockID: ", stockID, " 資料無法計算出 360 日均價")
		} else if todayStockPrice < AverageStockPrice360 {
			log.Info("stockID: ", stockID, " 目前股價低於 360 日均價")
			buyAmount = buyAmount + 2000 // 再多買 2000
		}

		return buyAmount, nil
	} else if action == "sell" {
		// 如果目前股價低於 180 日均價
		if AverageStockPrice180 == -1 {
			log.Error("stockID: ", stockID, " 資料無法計算出 180 日均價")
		} else if todayStockPrice < AverageStockPrice180 {
			log.Info("stockID: ", stockID, " 目前股價低於 180 日均價")
			buyAmount = buyAmount - 2000 // 少賣 2000
		}
		return buyAmount, nil
	}

	return -1, fmt.Errorf("action 參數錯誤")
}

func CheckIfBuy(log *logrus.Logger, stockID string, trackStocks_market_array []string, trackStocks_highDividend_array []string, today string) int {
	// 確認從 today 開始的過去一個月內是否有買過同類型的股票
	if helper.ValueInStringArray(stockID, trackStocks_market_array) == 1 { // 如果是市值型股票
		if CheckIfBuy_TimeChecking(log, stockID, trackStocks_market_array, today) != 1 { // 如果過去一個月內有買過同類型的股票
			return 0
		}
	} else if helper.ValueInStringArray(stockID, trackStocks_highDividend_array) == 1 { // 如果是高股息型股票
		if CheckIfBuy_TimeChecking(log, stockID, trackStocks_highDividend_array, today) != 1 { // 如果過去一個月內有買過同類型的股票
			return 0
		}
	} else {
		log.Error("stockID: ", stockID, " 不屬於市值型或高股息型股票")
		return -1
	}

	// 確認是否符合買點條件
	if CheckIfBuy_BuyPointChecking(log, stockID, today) != 1 {
		return 0
	}

	return 1
}

func CheckIfSell(log *logrus.Logger, stockID string, trackStocks_market_array []string, trackStocks_highDividend_array []string, today string) int {
	if os.Getenv("Scaling_Strategy") == "Pyramid" { // 如果採用金字塔策略，則忽略賣點判斷
		return 1
	}

	// 確認過去一個月內是否有賣過同類型的股票
	if helper.ValueInStringArray(stockID, trackStocks_market_array) == 1 { // 如果是市值型股票
		if CheckIfSell_TimeChecking(log, stockID, trackStocks_market_array, today) != 1 { // 如果過去一個月內有賣過同類型的股票
			return 0
		}
	} else if helper.ValueInStringArray(stockID, trackStocks_highDividend_array) == 1 { // 如果是高股息型股票
		if CheckIfSell_TimeChecking(log, stockID, trackStocks_highDividend_array, today) != 1 { // 如果過去一個月內有賣過同類型的股票
			return 0
		}
	} else {
		log.Error("stockID: ", stockID, " 不屬於市值型或高股息型股票")
		return -1
	}

	// // 確認是否符合賣點條件
	// if CheckIfSell_SellPointChecking(log, stockID, today) != 1 {
	// 	return 0
	// }

	return 1

}

func BuyStock(log *logrus.Logger, today string) {
	log.Info("BuyStock 開始執行")
	trackStocks_market_array := strings.Split(os.Getenv("TrackStocks_Market"), "&")
	trackStocks_highDividend_array := strings.Split(os.Getenv("TrackStocks_HighDividend"), "&")
	trackStocksArray := append(trackStocks_market_array, trackStocks_highDividend_array...)
	log.Info("TrackStocksArray: ", trackStocksArray)

	for _, stockID := range trackStocksArray { // 依序取出每一個股票 id
		if CheckIfBuy(log, stockID, trackStocks_market_array, trackStocks_highDividend_array, today) == 1 {
			buyAmount := 0.0
			if os.Getenv("Scaling_Strategy") == "AverageLine" { // 採用均線策略
				log.Info("stockID: ", stockID, " 採用均線策略")
				buyAmount = 5000.0 // 基本買入金額
				afterUpAndDown, err := AveragingUpAndDown(log, stockID, buyAmount, today, "buy")
				if err != nil {
					log.Error("AveragingUpAndDown 錯誤:", err)
					continue
				}

				buyAmount = afterUpAndDown
			} else if os.Getenv("Scaling_Strategy") == "Pyramid" { // 採用金字塔策略
				log.Info("stockID: ", stockID, " 採用金字塔策略")
				todayPrice, err := sqls.GetTodayStockPrice(log, stockID, today, "close_price")
				if err != nil {
					log.Error("GetTodayStockPrice 錯誤:", err)
					continue
				}

				HighestPrice, err := sqls.GetTransactionPriceOfUnrealizedGainsLosses(log, stockID, today, "Highest")
				percentages := 0.0
				if err != nil {
					log.Error("GetBuyPriceOfUnrealizedGainsLosses 錯誤:", err)
					continue
				}

				if HighestPrice != -1 {
					percentages = (todayPrice - HighestPrice) / HighestPrice
				}
				log.Info("stockID: ", stockID, " 今日股價: ", todayPrice, " 最高價: ", HighestPrice, " 與最高價之相對比例: ", percentages)

				if percentages > -0.1 {
					buyAmount = 1000.0
				} else if percentages > -0.2 {
					buyAmount = 1500.0
				} else if percentages > -0.3 {
					buyAmount = 2500.0
				} else if percentages > -0.4 {
					buyAmount = 4000.0
				} else {
					buyAmount = 6000.0
				}
			} else {
				log.Error("環境變數 Scaling_Strategy 設定錯誤")
			}

			log.Info("stockID: ", stockID, " 買入金額: ", buyAmount)
			err := sqls.SQLBuyStock(log, stockID, today, buyAmount)
			if err != nil {
				log.Error("SQLBuyStock 錯誤:", err)
				continue
			}
			log.Info("stockID: ", stockID, " 買入成功，買入金額: ", buyAmount)
		}
	}
}

func SellStock(log *logrus.Logger, today string) {
	log.Info("SellStock 開始執行")
	trackStocks_market_array := strings.Split(os.Getenv("TrackStocks_Market"), "&")
	trackStocks_highDividend_array := strings.Split(os.Getenv("TrackStocks_HighDividend"), "&")
	trackStocksArray := append(trackStocks_market_array, trackStocks_highDividend_array...)
	log.Info("TrackStocksArray: ", trackStocksArray)

	for _, stockID := range trackStocksArray { // 依序取出每一個股票 id
		if CheckIfSell(log, stockID, trackStocks_market_array, trackStocks_highDividend_array, today) == 1 {
			sellAmount := 0.0
			if os.Getenv("Scaling_Strategy") == "AverageLine" { // 採用均線策略
				log.Info("stockID: ", stockID, " 採用均線策略")
				sellAmount = 5000.0                                                                // 基本賣出金額
				afterUpAndDown, err := AveragingUpAndDown(log, stockID, sellAmount, today, "sell") // 調整賣出金額
				if err != nil {
					log.Error("AveragingUpAndDown 錯誤:", err)
					continue
				}

				sellAmount = afterUpAndDown
			} else if os.Getenv("Scaling_Strategy") == "Pyramid" { // 採用金字塔策略
				log.Info("stockID: ", stockID, " 採用金字塔策略")
				todayPrice, err := sqls.GetTodayStockPrice(log, stockID, today, "close_price")
				if err != nil {
					log.Error("GetTodayStockPrice 錯誤:", err)
					continue
				}

				LowestPrice, err := sqls.GetTransactionPriceOfUnrealizedGainsLosses(log, stockID, today, "Lowest")
				percentages := 0.0
				if err != nil {
					log.Error("GetBuyPriceOfUnrealizedGainsLosses 錯誤:", err)
					continue
				}

				if LowestPrice != -1 {
					percentages = (todayPrice - LowestPrice) / LowestPrice
				}
				log.Info("stockID: ", stockID, " 今日股價: ", todayPrice, " 最低價: ", LowestPrice, " 與最低價之相對比例: ", percentages)

				if percentages < 0.1 {
					sellAmount = 0.0
				} else if percentages < 0.2 {
					sellAmount = 0.0
				} else if percentages < 0.3 {
					sellAmount = 0.0
				} else if percentages < 1 {
					sellAmount = 0.0
				} else {
					sellAmount = 20000.0
				}
			} else {
				log.Error("環境變數 Scaling_Strategy 設定錯誤")
			}

			if sellAmount == 0.0 {
				log.Info("stockID: ", stockID, " 不符合賣出條件")
				continue
			}

			log.Info("stockID: ", stockID, " 預計賣出金額: ", sellAmount)
			err := sqls.SQLSellStock(log, stockID, today, sellAmount)
			if err != nil {
				log.Error("SQLSellStock 錯誤:", err)
				continue
			}
		}
	}
}

func DailyCheck(log *logrus.Logger) {
	log.Info("DailyCheck 開始執行")

	BackTesting, err := strconv.Atoi(os.Getenv("BackTesting")) // 是否開啟回測模式
	if err != nil {
		log.Error("BackTesting 環墋變數設定錯誤:", err)
		return
	}

	if BackTesting != -1 { // 開啟回測模式
		log.Info("進入回測模式")
		err := sqls.UpdataDatebase(log) // 先更新資料庫
		if err != nil {
			log.Error("回測模式出錯，UpdataDatebase 錯誤:", err)
		} else {
			dates := helper.GenerateDates(log, BackTesting) // 生成從現在開始，往前推 BackTesting 年，每次間隔一天的日期，格式類似 "2006-01-02"，由小至大排序
			for _, date := range dates {
				log.Info("date: ", date)
				BuyStock(log, date)
				SellStock(log, date)
			}
		}

	}

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
				log.Error("UpdataDatebase 錯誤:", err)
			} else {
				BuyStock(log, time.Now().Format("2006-01-02"))
				SellStock(log, time.Now().Format("2006-01-02"))
			}
		}
		//BuyStock(log)
		//SellStock(log)

		time.Sleep(60 * time.Second)
	}
}
