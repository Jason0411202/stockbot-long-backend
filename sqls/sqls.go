package sqls

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"main/app_context"
	"main/helper"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// 向 TWSE API 發送請求，取得股票歷史資料
func TWSEapi(date string, stockID string, appCtx *app_context.AppContext) (finalData [][]string, stockName string, err error) {
	apiurl := fmt.Sprintf("https://www.twse.com.tw/exchangeReport/STOCK_DAY?response=json&date=%s&stockNo=%s", date, stockID) // 準備好 api url
	appCtx.Log.Info("apiurl: ", apiurl)

	response, err := http.Get(apiurl) // 發送 GET 請求
	if err != nil {
		print("http.Get 發生錯誤")
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusOK { // 若 HTTP Status Code 為 200 OK
		var Datas map[string]interface{}
		err := json.NewDecoder(response.Body).Decode(&Datas) // 將 response.Body 解析成 map[string]interface{}
		if err != nil {
			appCtx.Log.Error("json.NewDecoder.Decode 發生錯誤")
			return nil, "", err
		}

		// 檢查是否有 Datas["data"] 這個 key
		if _, ok := Datas["data"]; !ok {
			return nil, "", fmt.Errorf("TWSE API 回傳的資料中，沒有 data 這個 key")
		}

		// 將 Datas["data"] 的部分，轉成一個二維陣列，回傳
		finalData := make([][]string, 0)
		for _, row := range Datas["data"].([]interface{}) {
			rowData := row.([]interface{})
			rowStrings := make([]string, 0)
			for _, item := range rowData {
				str := item.(string)
				str = strings.ReplaceAll(str, ",", "")
				str = strings.ReplaceAll(str, "X", "")
				rowStrings = append(rowStrings, str)
			}
			finalData = append(finalData, rowStrings)
		}
		// 反轉 finalData 陣列，讓日期由新到舊
		for i, j := 0, len(finalData)-1; i < j; i, j = i+1, j-1 {
			finalData[i], finalData[j] = finalData[j], finalData[i]
		}

		return finalData, strings.Fields(Datas["title"].(string))[2], nil // finalData 的部分，格式為一個二維陣列，stockName 為股票名稱，err 為 nil

	} else { // 若 HTTP Status Code 不為 200 OK
		appCtx.Log.Error("HTTP Status Code 不為 200")
		return nil, "", fmt.Errorf("HTTP Status Code: %d", response.StatusCode)
	}
}

// 將資料庫中的 StockHistory table 更新至最新
func UpdataDatebase(appCtx *app_context.AppContext) error {
	err := ConnectToMariadb(appCtx) // 連接至 Mariadb Server
	if err != nil {
		appCtx.Log.Error("ConnectToMariadb 錯誤:")
		return err
	}

	err = ConnectToDatabase(appCtx, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		appCtx.Log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// Generate dates from now, going back maxBackMonths months (default 1), format "20240501"
	now := time.Now()                     //取得現在時間
	currentDate := now.Format("20060102") // 格式化為 YYYYMMDD
	appCtx.Log.Info("currentDate: ", currentDate)

	maxBackMonths := appCtx.Cfg.MaxBackMonths
	if maxBackMonths < 0 {
		maxBackMonths = 1
	}

	// Backfill from currentDate monthly for maxBackMonths, store in Dates
	Dates := monthlyBackfillDates(currentDate, maxBackMonths)
	appCtx.Log.Info("Dates: ", Dates)

	for _, stockID := range appCtx.Cfg.TrackStocks { // 依序取出每一個股票 id
		for _, date := range Dates { // 依序取出每一個日期
			datas, stockName, err := TWSEapi(date, stockID, appCtx) // 呼叫 TWSEapi 函式，取得資料 (股票資料，股票名稱，錯誤訊息)
			if err != nil {
				appCtx.Log.Error("TWSEapi 發生錯誤", err)
				break
			}

			INSERT_FLAG := 0 // 如果某支股票在某個日期的資料新增失敗，則將 INSERT_FLAG 設為 1，換下一支股票
			for _, data := range datas {
				appCtx.Log.Info("data: ", data)
				AD_formatDate, err := helper.ROCToAD(data[0]) // 將 "113/01/01" 這種格式，轉換成 "2024-01-01"
				if err != nil {
					appCtx.Log.Error("helper.ROCToAD 錯誤: ", err)
					INSERT_FLAG = 1
					break
				}

				query := `INSERT IGNORE INTO StockHistory (stock_id, stock_name, date, volume, value, open_price, high_price, low_price, close_price, price_change, transactions) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
				_, err = appCtx.Db.Exec(query, stockID, stockName, AD_formatDate, data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8])
				appCtx.Log.Info("quary: ", fmt.Sprintf(query, stockID, stockName, AD_formatDate, data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8]))
				if err != nil {
					appCtx.Log.Error("資料庫新增資料錯誤, err: ", err)
					INSERT_FLAG = 1
					break
				}
			}
			if INSERT_FLAG == 1 {
				break
			}

			time.Sleep(3 * time.Second) // 每次執行完 TWSEapi 函式後，休息 3 秒
		}
	}

	return nil
}

// monthlyBackfillDates 從 currentDate (YYYYMMDD) 開始，回推 months 個月，
// 每月取 1 號，回傳 ["YYYYMMDD", ...]，由新到舊排序。
func monthlyBackfillDates(currentDate string, months int) []string {
	Dates := make([]string, 0, months+1)
	Dates = append(Dates, currentDate)

	t, err := time.Parse("20060102", currentDate)
	if err != nil {
		return Dates
	}
	for i := 0; i < months; i++ {
		t = t.AddDate(0, -1, 0)
		// Force day=1
		firstOfMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		Dates = append(Dates, firstOfMonth.Format("20060102"))
	}
	return Dates
}

// 連接至 Mariadb Server，回傳 *sql.DB
func ConnectToMariadb(appCtx *app_context.AppContext) error {
	// 測試 appCtx.Db 是否可用
	if appCtx.Db != nil {
		err := appCtx.Db.Ping()
		if err == nil {
			return nil
		}
	}

	// appCtx.Db 不可用，重連接至 Mariadb Server
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return fmt.Errorf("DB_DSN 環境變數未設定")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	appCtx.Db = db // 將 appCtx.Db 存入 appCtx.Db

	return nil
}

func ConnectToDatabase(appCtx *app_context.AppContext, databaseName string) error {
	_, err := appCtx.Db.Exec("USE " + databaseName) // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		appCtx.Log.Warn(databaseName+" 資料庫不存在:", err)
		return err
	}
	return nil
}

// 程式剛啟動時需要執行的函式，負責處理資料庫的初始化
func InitDatabase(appCtx *app_context.AppContext) error {
	err := ConnectToMariadb(appCtx) // 連接至 Mariadb Server
	if err != nil {
		appCtx.Log.Error("ConnectToMariadb 錯誤:")
		return err
	}

	// 檢查是否連線成功
	err = appCtx.Db.Ping()
	if err != nil {
		appCtx.Log.Error("Mariadb Server Ping 失敗:")
		return err
	}

	// 執行同一目錄下的腳本 SQLcommend.sql，建立資料庫以及相關 table
	sqlContent, err := os.ReadFile("./sqls/SQLcommend.sql")
	if err != nil {
		appCtx.Log.Error("無法讀取 SQLcommend.sql 檔案:")
		return err
	}
	_, err = appCtx.Db.Exec(string(sqlContent))
	if err != nil {
		appCtx.Log.Error("執行 SQLcommend.sql 檔案錯誤:")
		return err
	}

	_, err = appCtx.Db.Exec("USE StockLongData")
	if err != nil {
		appCtx.Log.Error("建立 StockLongData 資料庫失敗:")
		return err
	}

	appCtx.Log.Info("資料庫與對應 table 建立完成")

	// 初始化時使用 InitDBBackMonths 回補較長區間；完成後再以一般 UpdataDatebase 補最新。
	if appCtx.Cfg.InitDBBackMonths > appCtx.Cfg.MaxBackMonths {
		err = updateDatabaseWithMonths(appCtx, appCtx.Cfg.InitDBBackMonths)
		if err != nil {
			appCtx.Log.Error("initial UpdataDatebase 錯誤:", err)
			return err
		}
	} else {
		err = UpdataDatebase(appCtx)
		if err != nil {
			appCtx.Log.Error("UpdataDatebase 錯誤:")
			return err
		}
	}

	return nil
}

// updateDatabaseWithMonths 跟 UpdataDatebase 功能相同，但可指定回補月數 (用於初始化時)。
//
// 與 UpdataDatebase 不同處:會先檢查 DB 中該股票已存在哪些月份,只對「缺資料」的月份打 TWSE API。
// 當月 (YYYY-MM == today's month) 例外,一律重抓以涵蓋當月最新交易日。
// 這樣在 init_db_back_months 設大 (例如 60) 但 DB 已建立過的情境下,可大幅減少 API 呼叫與冷啟時間。
func updateDatabaseWithMonths(appCtx *app_context.AppContext, months int) error {
	err := ConnectToMariadb(appCtx)
	if err != nil {
		return err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return err
	}

	currentDate := time.Now().Format("20060102")
	Dates := monthlyBackfillDates(currentDate, months)
	appCtx.Log.Info("Init Dates: ", Dates)

	currentMonth := time.Now().Format("2006-01")

	for _, stockID := range appCtx.Cfg.TrackStocks {
		existingMonths, err := getExistingStockMonths(appCtx, stockID)
		if err != nil {
			return fmt.Errorf("getExistingStockMonths(%s) 失敗: %w", stockID, err)
		}

		for _, date := range Dates {
			ym, err := dateToYearMonth(date)
			if err != nil {
				appCtx.Log.Error("dateToYearMonth 錯誤: ", err)
				continue
			}
			if ym != currentMonth && existingMonths[ym] {
				appCtx.Log.Infof("%s 月份 %s 已存在於 DB,跳過 TWSE API 呼叫", stockID, ym)
				continue
			}

			datas, stockName, err := TWSEapi(date, stockID, appCtx)
			if err != nil {
				appCtx.Log.Error("TWSEapi 發生錯誤", err)
				break
			}
			INSERT_FLAG := 0
			for _, data := range datas {
				AD_formatDate, err := helper.ROCToAD(data[0])
				if err != nil {
					appCtx.Log.Error("helper.ROCToAD 錯誤: ", err)
					INSERT_FLAG = 1
					break
				}
				query := `INSERT IGNORE INTO StockHistory (stock_id, stock_name, date, volume, value, open_price, high_price, low_price, close_price, price_change, transactions) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
				_, err = appCtx.Db.Exec(query, stockID, stockName, AD_formatDate, data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8])
				if err != nil {
					appCtx.Log.Error("資料庫新增資料錯誤, err: ", err)
					INSERT_FLAG = 1
					break
				}
			}
			if INSERT_FLAG == 1 {
				break
			}
			time.Sleep(3 * time.Second)
		}
	}
	return nil
}

// getExistingStockMonths 回傳 DB 中該股票已存在資料的所有 YYYY-MM 月份集合。
// StockHistory.date 為 VARCHAR (格式 "YYYY-MM-DD"),故用 LEFT(date, 7) 取月份。
func getExistingStockMonths(appCtx *app_context.AppContext, stockID string) (map[string]bool, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return nil, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return nil, err
	}

	rows, err := appCtx.Db.Query("SELECT DISTINCT LEFT(date, 7) AS ym FROM StockHistory WHERE stock_id = ?;", stockID)
	if err != nil {
		return nil, fmt.Errorf("query existing months: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var ym string
		if err := rows.Scan(&ym); err != nil {
			return nil, fmt.Errorf("scan ym: %w", err)
		}
		existing[ym] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return existing, nil
}

// dateToYearMonth 將 "YYYYMMDD" 轉成 "YYYY-MM"。
func dateToYearMonth(date string) (string, error) {
	t, err := time.Parse("20060102", date)
	if err != nil {
		return "", fmt.Errorf("parse date %s: %w", date, err)
	}
	return t.Format("2006-01"), nil
}

func LastBuyTime(appCtx *app_context.AppContext, stockID string, today string) (string, error) {
	err := ConnectToMariadb(appCtx) // 連接至 Mariadb Server
	if err != nil {
		appCtx.Log.Error("ConnectToMariadb 錯誤:")
		return "", err
	}

	err = ConnectToDatabase(appCtx, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		appCtx.Log.Error("ConnectToDatabase 錯誤:")
		return "", err
	}

	SQL_cmd := "SELECT transaction_date FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ( SELECT MAX(transaction_date) FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ?);"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, stockID, today)
	if err != nil {
		appCtx.Log.Error("appCtx.Db.Query 錯誤:")
		return "", err
	}
	defer rows.Close()

	var transactionDate string
	for rows.Next() {
		err := rows.Scan(&transactionDate)
		if err != nil {
			appCtx.Log.Error("rows.Scan 錯誤:")
			return "", err
		}
	}

	return transactionDate, nil
}

func LastSellTime(appCtx *app_context.AppContext, stockID string, today string) (string, error) {
	err := ConnectToMariadb(appCtx) // 連接至 Mariadb Server
	if err != nil {
		appCtx.Log.Error("ConnectToMariadb 錯誤:")
		return "", err
	}

	err = ConnectToDatabase(appCtx, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		appCtx.Log.Error("ConnectToDatabase 錯誤:")
		return "", err
	}

	SQL_cmd := "SELECT sell_date FROM RealizedGainsLosses WHERE stock_id = ? AND sell_date = ( SELECT MAX(sell_date) FROM RealizedGainsLosses WHERE stock_id = ? AND sell_date <= ?);"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, stockID, today)
	if err != nil {
		appCtx.Log.Error("appCtx.Db.Query 錯誤:")
		return "", err
	}
	defer rows.Close()

	var transactionDate string
	for rows.Next() {
		err := rows.Scan(&transactionDate)
		if err != nil {
			appCtx.Log.Error("rows.Scan 錯誤:")
			return "", err
		}
	}

	return transactionDate, nil
}

func LowerPointDays(appCtx *app_context.AppContext, stockID string, today string) int {
	err := ConnectToMariadb(appCtx)
	if err != nil {
		appCtx.Log.Error("ConnectToMariadb 錯誤:")
		return -1
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		appCtx.Log.Error("ConnectToDatabase 錯誤:")
		return -1
	}

	SQL_cmd := "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, today)
	if err != nil {
		appCtx.Log.Error("appCtx.Db.Query 錯誤:")
		return -1
	}
	defer rows.Close()

	stockHistoryPriceRecords := make([]float64, 0)
	for rows.Next() {
		var record float64
		if err := rows.Scan(&record); err != nil {
			appCtx.Log.Error("rows.Scan 錯誤:")
			return -1
		}
		stockHistoryPriceRecords = append(stockHistoryPriceRecords, record)
	}

	if len(stockHistoryPriceRecords) == 0 {
		return 0
	}
	todayPrice := stockHistoryPriceRecords[0]
	for i, price := range stockHistoryPriceRecords {
		if price < todayPrice {
			return i
		}
	}
	return 36500
}

func UpperPointDays(appCtx *app_context.AppContext, stockID string, today string) int {
	err := ConnectToMariadb(appCtx)
	if err != nil {
		appCtx.Log.Error("ConnectToMariadb 錯誤:")
		return -1
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		appCtx.Log.Error("ConnectToDatabase 錯誤:")
		return -1
	}

	SQL_cmd := "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, today)
	if err != nil {
		appCtx.Log.Error("appCtx.Db.Query 錯誤:")
		return -1
	}
	defer rows.Close()

	stockHistoryPriceRecords := make([]float64, 0)
	for rows.Next() {
		var record float64
		if err := rows.Scan(&record); err != nil {
			appCtx.Log.Error("rows.Scan 錯誤:")
			return -1
		}
		stockHistoryPriceRecords = append(stockHistoryPriceRecords, record)
	}

	if len(stockHistoryPriceRecords) == 0 {
		return 0
	}
	todayPrice := stockHistoryPriceRecords[0]
	for i, price := range stockHistoryPriceRecords {
		if price > todayPrice {
			return i
		}
	}
	return 36500
}

func GetStockName(appCtx *app_context.AppContext, stockID string) (string, error) {
	err := ConnectToMariadb(appCtx)
	if err != nil {
		return "", err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return "", err
	}

	SQL_cmd := "SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var stockName string
	for rows.Next() {
		if err := rows.Scan(&stockName); err != nil {
			return "", err
		}
	}
	return stockName, nil
}

func GetAverageStockPrice(appCtx *app_context.AppContext, stockID string, today string, days int) (float64, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return -1, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return -1, err
	}

	SQL_cmd := "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT ?;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, today, days)
	if err != nil {
		return -1, err
	}
	defer rows.Close()

	stockHistoryPriceRecords := make([]float64, 0, days)
	for rows.Next() {
		var record float64
		if err := rows.Scan(&record); err != nil {
			return -1, err
		}
		stockHistoryPriceRecords = append(stockHistoryPriceRecords, record)
	}

	if len(stockHistoryPriceRecords) < days {
		return -1, fmt.Errorf("資料不足，無法計算均價")
	}

	var sum float64
	for _, price := range stockHistoryPriceRecords {
		sum += price
	}
	return sum / float64(days), nil
}

func GetTodayStockPrice(appCtx *app_context.AppContext, stockID string, today string, priceType string) (float64, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return -1, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return -1, err
	}

	SQL_cmd := "SELECT " + priceType + " FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT 1;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, today)
	if err != nil {
		return -1, err
	}
	defer rows.Close()

	var stockPrice float64
	for rows.Next() {
		if err := rows.Scan(&stockPrice); err != nil {
			return -1, err
		}
	}
	return stockPrice, nil
}

// SQLBuyStock 以股數為單位寫入一筆買入紀錄。
// purchasePrice = 買入當天收盤價 (亦是 DB 中 StockHistory 的 close_price)。
// investmentCost = shares * purchasePrice。
func SQLBuyStock(appCtx *app_context.AppContext, stockID string, today string, shares int) error {
	if shares <= 0 {
		return fmt.Errorf("shares 必須大於 0, got %d", shares)
	}

	if err := ConnectToMariadb(appCtx); err != nil {
		return err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return err
	}

	todayPrice, err := GetTodayStockPrice(appCtx, stockID, today, "close_price")
	if err != nil {
		return err
	}
	investmentCost := todayPrice * float64(shares)

	SQL_cmd := `INSERT INTO UnrealizedGainsLosses (transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares) VALUES (?, ?, (SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1), ?, ?, ?);`
	if _, err := appCtx.Db.Exec(SQL_cmd, today, stockID, stockID, todayPrice, investmentCost, shares); err != nil {
		appCtx.Log.Error("appCtx.Db.Exec 錯誤:", err)
		return err
	}
	return nil
}

// SQLSellStock 以股數為目標賣出，從成本最低的未實現紀錄開始賣出，
// 直到累計賣出股數達到 targetShares 為止。
func SQLSellStock(appCtx *app_context.AppContext, stockID string, today string, targetShares int) error {
	if targetShares <= 0 {
		return nil
	}

	if err := ConnectToMariadb(appCtx); err != nil {
		return err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return err
	}

	todayClosePrice, err := GetTodayStockPrice(appCtx, stockID, today, "close_price")
	if err != nil {
		return err
	}

	remaining := targetShares
	for remaining > 0 {
		record, err := GetLowestUnrealizedGainsLossesRecord(appCtx, stockID, today)
		if err != nil {
			appCtx.Log.Warn("賣出時找不到持倉:", err)
			return nil // 無庫存可賣，視為 no-op
		}

		lotShares := record["shares"].(int)
		lotCost := record["investment_cost"].(float64)
		txPrice := record["transaction_price"].(float64)
		txDate := record["transaction_date"].(string)
		stockName := record["stock_name"].(string)

		if lotShares <= 0 {
			// 舊資料 shares=0，無法以股數為單位處理，直接刪除避免死迴圈。
			if err := DeleteLowestUnrealizedGainsLossesRecord(appCtx, stockID, txDate); err != nil {
				return err
			}
			continue
		}

		if lotShares <= remaining {
			// 整筆 lot 賣掉
			soldShares := lotShares
			revenue := todayClosePrice * float64(soldShares)
			profitLoss := revenue - lotCost
			profitRate := 0.0
			if lotCost > 0 {
				profitRate = (profitLoss / lotCost) * 100
			}
			if err := DeleteLowestUnrealizedGainsLossesRecord(appCtx, stockID, txDate); err != nil {
				return err
			}
			if err := InsertToRealizedGainsLosses(appCtx, txDate, today, stockID, stockName, txPrice, todayClosePrice, lotCost, revenue, profitLoss, profitRate, soldShares); err != nil {
				return err
			}
			remaining -= soldShares
		} else {
			// 只賣 lot 的一部分
			soldShares := remaining
			revenue := todayClosePrice * float64(soldShares)
			soldCost := txPrice * float64(soldShares)
			profitLoss := revenue - soldCost
			profitRate := 0.0
			if soldCost > 0 {
				profitRate = (profitLoss / soldCost) * 100
			}
			newShares := lotShares - soldShares
			newCost := lotCost - soldCost
			if err := UpdateLowestUnrealizedGainsLossesRecord(appCtx, stockID, newCost, newShares, txDate); err != nil {
				return err
			}
			if err := InsertToRealizedGainsLosses(appCtx, txDate, today, stockID, stockName, txPrice, todayClosePrice, soldCost, revenue, profitLoss, profitRate, soldShares); err != nil {
				return err
			}
			remaining = 0
		}
	}
	return nil
}

func InsertToRealizedGainsLosses(appCtx *app_context.AppContext, buy_date string, sell_date string, stock_id string, stock_name string, purchase_price float64, sell_price float64, investment_cost float64, revenue float64, profit_loss float64, profit_rate float64, shares int) error {
	if err := ConnectToMariadb(appCtx); err != nil {
		return err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return err
	}

	SQL_cmd := "INSERT INTO RealizedGainsLosses (buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
	if _, err := appCtx.Db.Exec(SQL_cmd, buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares); err != nil {
		appCtx.Log.Error("appCtx.Db.Exec 錯誤:", err)
		return err
	}
	return nil
}

// GetLowestUnrealizedGainsLossesRecord 回傳該股票中，交易價格最低的一筆未實現損益紀錄。
// 回傳 map 包含 shares(int) 欄位。
func GetLowestUnrealizedGainsLossesRecord(appCtx *app_context.AppContext, stockID string, today string) (map[string]interface{}, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return nil, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return nil, err
	}

	SQL_cmd := "SELECT transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ? ORDER BY transaction_price ASC, transaction_date ASC LIMIT 1;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockID, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transaction_date := ""
	stock_id := ""
	stock_name := ""
	transaction_price := 0.0
	investment_cost := 0.0
	shares := 0
	found := false
	for rows.Next() {
		if err := rows.Scan(&transaction_date, &stock_id, &stock_name, &transaction_price, &investment_cost, &shares); err != nil {
			return nil, err
		}
		found = true
	}

	if !found {
		return nil, fmt.Errorf("在未實現損益紀錄中找不到 stock_id: %s 的資料", stockID)
	}

	return map[string]interface{}{
		"transaction_date":  transaction_date,
		"stock_id":          stock_id,
		"stock_name":        stock_name,
		"transaction_price": transaction_price,
		"investment_cost":   investment_cost,
		"shares":            shares,
	}, nil
}

func DeleteLowestUnrealizedGainsLossesRecord(appCtx *app_context.AppContext, stockID string, transaction_date string) error {
	if err := ConnectToMariadb(appCtx); err != nil {
		return err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return err
	}
	SQL_cmd := "DELETE FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ?;"
	if _, err := appCtx.Db.Exec(SQL_cmd, stockID, transaction_date); err != nil {
		appCtx.Log.Error("appCtx.Db.Exec 錯誤:", err)
		return err
	}
	return nil
}

func UpdateLowestUnrealizedGainsLossesRecord(appCtx *app_context.AppContext, stockID string, investment_cost float64, shares int, transaction_date string) error {
	if err := ConnectToMariadb(appCtx); err != nil {
		return err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return err
	}
	SQL_cmd := "UPDATE UnrealizedGainsLosses SET investment_cost = ?, shares = ? WHERE stock_id = ? AND transaction_date = ?;"
	if _, err := appCtx.Db.Exec(SQL_cmd, investment_cost, shares, stockID, transaction_date); err != nil {
		appCtx.Log.Error("appCtx.Db.Exec 錯誤:", err)
		return err
	}
	return nil
}

func GetAllUnrealizedGainsLosses(appCtx *app_context.AppContext) ([]map[string]interface{}, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return nil, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return nil, err
	}

	SQL_cmd := "SELECT transaction_date, stock_id, stock_name, transaction_price, investment_cost, shares FROM UnrealizedGainsLosses ORDER BY transaction_date DESC LIMIT 500;"
	rows, err := appCtx.Db.Query(SQL_cmd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	returnValue := make([]map[string]interface{}, 0)
	for rows.Next() {
		transaction_date := ""
		stock_id := ""
		stock_name := ""
		transaction_price := 0.0
		investment_cost := 0.0
		shares := 0
		if err := rows.Scan(&transaction_date, &stock_id, &stock_name, &transaction_price, &investment_cost, &shares); err != nil {
			return nil, err
		}

		todayClosePrice, err := GetTodayStockPrice(appCtx, stock_id, time.Now().Format("2006-01-02"), "close_price")
		if err != nil {
			todayClosePrice = 0
		}
		now_value := todayClosePrice * float64(shares)
		if shares == 0 && transaction_price > 0 { // 相容舊資料 (未記錄股數者)
			now_value = (todayClosePrice / transaction_price) * investment_cost
		}
		predict_profit_loss := now_value - investment_cost
		predict_profit_rate := 0.0
		if investment_cost > 0 {
			predict_profit_rate = (predict_profit_loss / investment_cost) * 100
		}

		returnValue = append(returnValue, map[string]interface{}{
			"transaction_date":    transaction_date,
			"stock_id":            stock_id,
			"stock_name":          stock_name,
			"transaction_price":   transaction_price,
			"investment_cost":     investment_cost,
			"shares":              shares,
			"todayClosePrice":     todayClosePrice,
			"now_value":           math.Round(now_value*100) / 100,
			"predict_profit_loss": math.Round(predict_profit_loss*100) / 100,
			"predict_profit_rate": math.Round(predict_profit_rate*100) / 100,
		})
	}
	return returnValue, nil
}

func GetAllRealizedGainsLosses(appCtx *app_context.AppContext) ([]map[string]interface{}, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return nil, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return nil, err
	}

	SQL_cmd := "SELECT buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate, shares FROM RealizedGainsLosses ORDER BY sell_date DESC LIMIT 500;"
	rows, err := appCtx.Db.Query(SQL_cmd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	returnValue := make([]map[string]interface{}, 0)
	for rows.Next() {
		buy_date := ""
		sell_date := ""
		stock_id := ""
		stock_name := ""
		purchase_price := 0.0
		sell_price := 0.0
		investment_cost := 0.0
		revenue := 0.0
		profit_loss := 0.0
		profit_rate := 0.0
		shares := 0
		if err := rows.Scan(&buy_date, &sell_date, &stock_id, &stock_name, &purchase_price, &sell_price, &investment_cost, &revenue, &profit_loss, &profit_rate, &shares); err != nil {
			return nil, err
		}

		returnValue = append(returnValue, map[string]interface{}{
			"buy_date":        buy_date,
			"sell_date":       sell_date,
			"stock_id":        stock_id,
			"stock_name":      stock_name,
			"purchase_price":  purchase_price,
			"sell_price":      sell_price,
			"investment_cost": investment_cost,
			"revenue":         math.Round(revenue*100) / 100,
			"profit_loss":     math.Round(profit_loss*100) / 100,
			"profit_rate":     math.Round(profit_rate*100) / 100,
			"shares":          shares,
		})
	}
	return returnValue, nil
}

func GetStockStatisticData(appCtx *app_context.AppContext) ([]map[string]interface{}, error) {
	returnValue := make([]map[string]interface{}, 0)
	for _, stockID := range appCtx.Cfg.TrackStocks {
		stockName, err := GetStockName(appCtx, stockID)
		if err != nil {
			return nil, err
		}

		todayPrice, err := GetTodayStockPrice(appCtx, stockID, time.Now().Format("2006-01-02"), "close_price")
		if err != nil {
			return nil, err
		}

		upperPointDays := UpperPointDays(appCtx, stockID, time.Now().Format("2006-01-02"))
		if upperPointDays == -1 {
			return nil, nil
		}
		lowerPointDays := LowerPointDays(appCtx, stockID, time.Now().Format("2006-01-02"))
		if lowerPointDays == -1 {
			return nil, nil
		}

		returnValue = append(returnValue, map[string]interface{}{
			"stock_id":         stockID,
			"stock_name":       stockName,
			"today_price":      todayPrice,
			"lower_point_days": lowerPointDays,
			"upper_point_days": upperPointDays,
		})
	}
	return returnValue, nil
}

func GetStockHistoryData(appCtx *app_context.AppContext, stockId string) ([]map[string]interface{}, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return nil, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return nil, err
	}

	SQL_cmd := "SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;"
	rows, err := appCtx.Db.Query(SQL_cmd, stockId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	returnValue := make([]map[string]interface{}, 0)
	for rows.Next() {
		date := ""
		price := 0.0
		if err := rows.Scan(&date, &price); err != nil {
			return nil, err
		}
		returnValue = append(returnValue, map[string]interface{}{
			"date":  date,
			"price": price,
		})
	}
	return returnValue, nil
}

// GetTransactionPriceOfUnrealizedGainsLosses 取得未實現損益中，某支股票的最高或最低交易價格，
// 若無資料則回傳 -1。
func GetTransactionPriceOfUnrealizedGainsLosses(appCtx *app_context.AppContext, stockID string, today string, types string) (float64, error) {
	if err := ConnectToMariadb(appCtx); err != nil {
		return -1, err
	}
	if err := ConnectToDatabase(appCtx, "StockLongData"); err != nil {
		return -1, err
	}

	var SQL_cmd string
	if types == "Highest" {
		SQL_cmd = "SELECT MAX(transaction_price) FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ?;"
	} else {
		SQL_cmd = "SELECT MIN(transaction_price) FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ?;"
	}
	row := appCtx.Db.QueryRow(SQL_cmd, stockID, today)
	var price sql.NullFloat64
	if err := row.Scan(&price); err != nil {
		return -1, err
	}
	if !price.Valid {
		return -1, nil
	}
	return price.Float64, nil
}
