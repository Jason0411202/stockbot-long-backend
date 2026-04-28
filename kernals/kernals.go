package kernals

import (
	"fmt"
	"main/app_context"
	"main/discord"
	"main/sqls"
	"math"
	"time"
)

// cooldownHours 回傳設定檔中 CooldownDays 轉成的 time.Duration。
func cooldownHours(appCtx *app_context.AppContext) time.Duration {
	return time.Duration(appCtx.Cfg.CooldownDays) * 24 * time.Hour
}

// CheckIfBuy_TimeChecking 每隻股票都有自己的冷卻期。回傳 1 表示可以買，0 表示冷卻中，-1 表示錯誤。
func CheckIfBuy_TimeChecking(appCtx *app_context.AppContext, stockID string, today string) int {
	lastBuyTime, err := sqls.LastBuyTime(appCtx, stockID, today)
	if err != nil {
		appCtx.Log.Error("LastBuyTime 發生錯誤:", err)
		return -1
	}
	if lastBuyTime == "" {
		return 1
	}

	lastBuy, err := time.Parse("2006-01-02", lastBuyTime)
	if err != nil {
		appCtx.Log.Error("無法將 lastBuyTime 轉成 time.time:", err)
		return -1
	}
	todayT, err := time.Parse("2006-01-02", today)
	if err != nil {
		appCtx.Log.Error("無法將 today 轉成 time.time:", err)
		return -1
	}

	diff := todayT.Sub(lastBuy)
	if diff >= 0 && diff < cooldownHours(appCtx) {
		appCtx.Log.Info(stockID, " 仍在買入冷卻期內, lastBuy=", lastBuyTime)
		return 0
	}
	return 1
}

// CheckIfSell_TimeChecking 每隻股票都有自己的冷卻期。
func CheckIfSell_TimeChecking(appCtx *app_context.AppContext, stockID string, today string) int {
	lastSellTime, err := sqls.LastSellTime(appCtx, stockID, today)
	if err != nil {
		appCtx.Log.Error("LastSellTime 發生錯誤:", err)
		return -1
	}
	if lastSellTime == "" {
		return 1
	}
	lastSell, err := time.Parse("2006-01-02", lastSellTime)
	if err != nil {
		appCtx.Log.Error("無法將 lastSellTime 轉成 time.time:", err)
		return -1
	}
	todayT, err := time.Parse("2006-01-02", today)
	if err != nil {
		appCtx.Log.Error("無法將 today 轉成 time.time:", err)
		return -1
	}

	diff := todayT.Sub(lastSell)
	if diff >= 0 && diff < cooldownHours(appCtx) {
		appCtx.Log.Info(stockID, " 仍在賣出冷卻期內, lastSell=", lastSellTime)
		return 0
	}
	return 1
}

func CheckIfBuy_BuyPointChecking(appCtx *app_context.AppContext, stockID string, today string) int {
	AverageStockPrice20, err := sqls.GetAverageStockPrice(appCtx, stockID, today, 20)
	if err != nil {
		appCtx.Log.Error("GetAverageStockPrice 錯誤:", err)
		return -1
	}
	todayPrice, err := sqls.GetTodayStockPrice(appCtx, stockID, today, "close_price")
	if err != nil {
		appCtx.Log.Error("GetTodayStockPrice 錯誤:", err)
		return -1
	}

	if todayPrice >= AverageStockPrice20 {
		appCtx.Log.Info(stockID, " 目前股價高於 20 日均價")
		return 0
	}
	appCtx.Log.Info(stockID, " 目前股價低於 20 日均價")
	return 1
}

// CheckIfBuy 簡化後僅檢查單一股票自己的冷卻期與買點。
func CheckIfBuy(appCtx *app_context.AppContext, stockID string, today string) int {
	if CheckIfBuy_TimeChecking(appCtx, stockID, today) != 1 {
		return 0
	}
	if CheckIfBuy_BuyPointChecking(appCtx, stockID, today) != 1 {
		return 0
	}
	return 1
}

// CheckIfSell baseline 策略下，只要有滿足獲利門檻即可賣出 (無需冷卻)。
func CheckIfSell(appCtx *app_context.AppContext, stockID string, today string) int {
	// Baseline 策略下，賣出由 SellStock 內部的獲利門檻決定，此處一律放行。
	return 1
}

// baselineBuyAmount 依照 config 中 tier 決定買入目標金額。
func baselineBuyAmount(appCtx *app_context.AppContext, percentages float64) float64 {
	for _, tier := range appCtx.Cfg.BaselineBuyTiers {
		if percentages > tier.Above {
			return tier.Amount
		}
	}
	return appCtx.Cfg.BaselineBuyFallbackAmount
}

// amountToShares 將金額轉為最接近的股數 (四捨五入)。若 price<=0 則回傳 0。
func amountToShares(amount float64, price float64) int {
	if price <= 0 || amount <= 0 {
		return 0
	}
	return int(math.Round(amount / price))
}

func BuyStock(appCtx *app_context.AppContext, today string) {
	appCtx.Log.Info("BuyStock 開始執行")
	for _, stockID := range appCtx.Cfg.TrackStocks {
		if CheckIfBuy(appCtx, stockID, today) != 1 {
			continue
		}
		if appCtx.Cfg.ScalingStrategy != "Baseline" {
			appCtx.Log.Error("目前僅支援 Scaling_Strategy=Baseline, got ", appCtx.Cfg.ScalingStrategy)
			return
		}

		todayPrice, err := sqls.GetTodayStockPrice(appCtx, stockID, today, "close_price")
		if err != nil {
			appCtx.Log.Error("GetTodayStockPrice 錯誤:", err)
			continue
		}
		highestPrice, err := sqls.GetTransactionPriceOfUnrealizedGainsLosses(appCtx, stockID, today, "Highest")
		if err != nil {
			appCtx.Log.Error("GetTransactionPriceOfUnrealizedGainsLosses 錯誤:", err)
			continue
		}

		percentages := 0.0
		if highestPrice > 0 {
			percentages = (todayPrice - highestPrice) / highestPrice
		}
		appCtx.Log.Info(stockID, " 今日股價: ", todayPrice, " 最高價: ", highestPrice, " 與最高價之相對比例: ", percentages)

		buyAmount := baselineBuyAmount(appCtx, percentages) * appCtx.Cfg.BuyAndSellMultiplier
		shares := amountToShares(buyAmount, todayPrice)
		if shares <= 0 {
			appCtx.Log.Info(stockID, " 計算出的買入股數為 0，略過")
			continue
		}

		if err := sqls.SQLBuyStock(appCtx, stockID, today, shares); err != nil {
			appCtx.Log.Error("SQLBuyStock 錯誤:", err)
			continue
		}
		actualCost := float64(shares) * todayPrice
		appCtx.Log.Infof("%s 買入成功，股數: %d, 單價: %.2f, 實際金額: %.2f", stockID, shares, todayPrice, actualCost)
		if err := discord.SendEmbedDiscordMessage(appCtx, "🔴 買入通知", fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f", stockID, shares, todayPrice, actualCost), 0xD50000); err != nil {
			appCtx.Log.Error("發送 Discord 訊息失敗:", err)
		}
	}
}

func SellStock(appCtx *app_context.AppContext, today string) {
	appCtx.Log.Info("SellStock 開始執行")
	for _, stockID := range appCtx.Cfg.TrackStocks {
		if CheckIfSell(appCtx, stockID, today) != 1 {
			continue
		}
		if appCtx.Cfg.ScalingStrategy != "Baseline" {
			appCtx.Log.Error("目前僅支援 Scaling_Strategy=Baseline, got ", appCtx.Cfg.ScalingStrategy)
			return
		}

		todayPrice, err := sqls.GetTodayStockPrice(appCtx, stockID, today, "close_price")
		if err != nil {
			appCtx.Log.Error("GetTodayStockPrice 錯誤:", err)
			continue
		}

		lowestPrice, err := sqls.GetTransactionPriceOfUnrealizedGainsLosses(appCtx, stockID, today, "Lowest")
		if err != nil {
			appCtx.Log.Error("GetTransactionPriceOfUnrealizedGainsLosses 錯誤:", err)
			continue
		}
		if lowestPrice <= 0 {
			continue // 無持倉
		}
		gain := (todayPrice - lowestPrice) / lowestPrice
		if gain < appCtx.Cfg.BaselineSellThreshold {
			continue // 未達獲利門檻
		}

		sellAmount := appCtx.Cfg.BaselineSellAmount * appCtx.Cfg.BuyAndSellMultiplier
		targetShares := amountToShares(sellAmount, todayPrice)
		if targetShares <= 0 {
			continue
		}

		if err := sqls.SQLSellStock(appCtx, stockID, today, targetShares); err != nil {
			appCtx.Log.Error("SQLSellStock 錯誤:", err)
			continue
		}
		actualRevenue := float64(targetShares) * todayPrice
		appCtx.Log.Infof("%s 賣出股數: %d, 單價: %.2f, 估計金額: %.2f", stockID, targetShares, todayPrice, actualRevenue)
		if err := discord.SendEmbedDiscordMessage(appCtx, "🟢 賣出通知", fmt.Sprintf("stockID: %s, 股數: %d, 單價: %.2f, 金額: %.2f", stockID, targetShares, todayPrice, actualRevenue), 0x00C853); err != nil {
			appCtx.Log.Error("發送 Discord 訊息失敗:", err)
		}
	}
}

func DailyCheck(appCtx *app_context.AppContext) {
	appCtx.Log.Info("DailyCheck 開始執行")

	backTesting := appCtx.Cfg.BackTestingMonths
	if backTesting > 0 {
		appCtx.Log.Info("進入回測模式 (in-memory), months=", backTesting)
		if err := sqls.UpdataDatebase(appCtx); err != nil {
			appCtx.Log.Error("回測模式出錯，UpdataDatebase 錯誤:", err)
		} else {
			result, err := RunBacktest(appCtx, backTesting)
			if err != nil {
				appCtx.Log.Error("回測執行錯誤:", err)
			} else {
				appCtx.Log.Infof("=== 回測結果 === 起始現金: %.2f, 期末現金: %.2f, 期末持股市值: %.2f, 合計: %.2f",
					result.InitialCash, result.FinalCash, result.FinalHoldingValue, result.FinalTotal)
				_ = discord.SendEmbedDiscordMessage(appCtx, "📊 回測結果",
					fmt.Sprintf("起始現金: %.2f\n期末現金: %.2f\n期末持股市值: %.2f\n合計: %.2f",
						result.InitialCash, result.FinalCash, result.FinalHoldingValue, result.FinalTotal),
					0x2196F3)
			}
		}
	}

	taiwanTimeZone, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		appCtx.Log.Fatal("取得 taiwanTimeZone 時發生錯誤", err)
	}

	for {
		now := time.Now().In(taiwanTimeZone)
		if now.Hour() == 14 && now.Minute() == 0 {
			appCtx.Log.Info("現在時間: ", now)
			if err = sqls.UpdataDatebase(appCtx); err != nil {
				appCtx.Log.Error("UpdataDatebase 錯誤:", err)
			} else {
				BuyStock(appCtx, time.Now().Format("2006-01-02"))
				SellStock(appCtx, time.Now().Format("2006-01-02"))
			}
		}
		time.Sleep(60 * time.Second)
	}
}
