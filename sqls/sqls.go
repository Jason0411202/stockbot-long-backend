package sqls

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"main/helper"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// 向 TWSE API 發送請求，取得股票歷史資料
func TWSEapi(date string, stockID string, log *logrus.Logger) (finalData [][]string, stockName string, err error) {
	apiurl := fmt.Sprintf("https://www.twse.com.tw/exchangeReport/STOCK_DAY?response=json&date=%s&stockNo=%s", date, stockID) // 準備好 api url
	log.Info("apiurl: ", apiurl)

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
			log.Error("json.NewDecoder.Decode 發生錯誤")
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
		log.Error("HTTP Status Code 不為 200")
		return nil, "", fmt.Errorf("HTTP Status Code: %d", response.StatusCode)
	}
}

// 將資料庫中的 StockHistory table 更新至最新
func UpdataDatebase(log *logrus.Logger) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// 生成從現在開始，往前推 1 年，每次間隔一個月的日期，格式類似 "20240501"
	now := time.Now()                     //取得現在時間
	currentDate := now.Format("20060102") // 格式化為 YYYYMMDD
	log.Info("currentDate: ", currentDate)

	// 將 currentDate 每隔一個月回推，共回推 72 次 (六年)，存成 Dates 陣列，例如 [20240627 20240501 ... 20230701]
	currentYear, _ := strconv.Atoi(currentDate[:4])
	currentMonth, _ := strconv.Atoi(currentDate[4:6])

	Dates := make([]string, 0)
	Dates = append(Dates, currentDate)
	for i := 0; i < 71; i++ {
		currentMonth--
		if currentMonth == 0 {
			currentMonth = 12
			currentYear--
		}
		date := fmt.Sprintf("%d%02d%02d", currentYear, currentMonth, 1)
		Dates = append(Dates, date)
	}
	log.Info("Dates: ", Dates)

	trackStocks_market_array := strings.Split(os.Getenv("TrackStocks_Market"), "&")
	trackStocks_highDividend_array := strings.Split(os.Getenv("TrackStocks_HighDividend"), "&")
	trackStocksArray := append(trackStocks_market_array, trackStocks_highDividend_array...)
	log.Info("TrackStocksArray: ", trackStocksArray)
	for _, stockID := range trackStocksArray { // 依序取出每一個股票 id
		for _, date := range Dates { // 依序取出每一個日期
			datas, stockName, err := TWSEapi(date, stockID, log) // 呼叫 TWSEapi 函式，取得資料 (股票資料，股票名稱，錯誤訊息)
			if err != nil {
				log.Error("TWSEapi 發生錯誤", err)
				break
			}

			INSERT_FLAG := 0 // 如果某支股票在某個日期的資料新增失敗，則將 INSERT_FLAG 設為 1，換下一支股票
			for _, data := range datas {
				log.Info("data: ", data)
				AD_formatDate, err := helper.ROCToAD(data[0]) // 將 "113/01/01" 這種格式，轉換成 "2024-01-01"
				if err != nil {
					log.Error("helper.ROCToAD 錯誤: ", err)
					INSERT_FLAG = 1
					break
				}

				query := `INSERT INTO StockHistory (stock_id, stock_name, date, volume, value, open_price, high_price, low_price, close_price, price_change, transactions) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
				_, err = db.Exec(query, stockID, stockName, AD_formatDate, data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8])
				log.Info("quary: ", fmt.Sprintf(query, stockID, stockName, AD_formatDate, data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8]))
				if err != nil {
					log.Error("資料庫新增資料錯誤, err: ", err)
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

// 連接至 Mariadb Server，回傳 *sql.DB
func ConnectToMariadb(log *logrus.Logger) (*sql.DB, error) {
	// 連接至 Mariadb Server
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/?multiStatements=true", os.Getenv("MariadbUser"),
		os.Getenv("MariadbPassword"), os.Getenv("MariadbHost"), os.Getenv("MariadbPort")))
	if err != nil {
		return nil, err
	}

	return db, nil
}

func ConnectToDatabase(db *sql.DB, log *logrus.Logger, databaseName string) error {
	_, err := db.Exec("USE " + databaseName) // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Warn(databaseName+" 資料庫不存在:", err)
		return err
	}
	return nil
}

// 程式剛啟動時需要執行的函式，負責處理資料庫的初始化
func InitDatabase(log *logrus.Logger) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	// 檢查是否連線成功
	err = db.Ping()
	if err != nil {
		log.Error("Mariadb Server Ping 失敗:")
		return err
	}

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Warn("StockLongData 資料庫不存在:", err)
		log.Info("重新建立資料庫與對應 table 中...")

		// 若資料庫 "StockLongData" 不存在，則執行同一目錄下的腳本 SQLcommend.sql，建立資料庫以及相關 table
		sqlContent, err := os.ReadFile("./sqls/SQLcommend.sql")
		if err != nil {
			log.Error("無法讀取 SQLcommend.sql 檔案:")
			return err
		}
		_, err = db.Exec(string(sqlContent))
		if err != nil {
			log.Error("執行 SQLcommend.sql 檔案錯誤:")
			return err
		}

		_, err = db.Exec("USE StockLongData")
		if err != nil {
			log.Error("建立 StockLongData 資料庫失敗:")
			return err
		}

		log.Info("資料庫與對應 table 建立完成")
	}

	err = UpdataDatebase(log)
	if err != nil {
		log.Error("UpdataDatebase 錯誤:")
		return err
	}

	return nil
}

func LastBuyTime(log *logrus.Logger, stockID string, today string) (string, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return "", err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return "", err
	}

	SQL_cmd := "SELECT transaction_date FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ( SELECT MAX(transaction_date) FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ?);"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, stockID, today))
	rows, err := db.Query(SQL_cmd, stockID, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return "", err
	}
	defer rows.Close()

	var transactionDate string
	for rows.Next() {
		err := rows.Scan(&transactionDate)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return "", err
		}
	}

	return transactionDate, nil
}

func LastSellTime(log *logrus.Logger, stockID string, today string) (string, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return "", err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return "", err
	}

	SQL_cmd := "SELECT sell_date FROM RealizedGainsLosses WHERE stock_id = ? AND sell_date = ( SELECT MAX(sell_date) FROM RealizedGainsLosses WHERE stock_id = ? AND sell_date <= ?);"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, stockID, today))
	rows, err := db.Query(SQL_cmd, stockID, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return "", err
	}
	defer rows.Close()

	var transactionDate string
	for rows.Next() {
		err := rows.Scan(&transactionDate)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return "", err
		}
	}

	return transactionDate, nil

}

func LowerPointDays(log *logrus.Logger, stockID string, today string) int {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return -1
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return -1
	}

	// 準備 SQL 指令
	// 查詢 StockHistory table 中，stock_id 為 stockID 的"當天收盤價"，並依照 date 由新到舊排序
	SQL_cmd := "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, today))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return -1
	}
	defer rows.Close()

	stockHistoryPriceRecords := make([]float64, 0)
	for rows.Next() { // 將查詢結果存入 stockHistoryPriceRecords 陣列
		var record float64
		err := rows.Scan(&record)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return -1
		}
		stockHistoryPriceRecords = append(stockHistoryPriceRecords, record)
	}

	todayPrice := stockHistoryPriceRecords[0]        // 取得當天價格
	for i, price := range stockHistoryPriceRecords { // 比較當天價格與過去價格
		if price < todayPrice { // 如果過去價格比當天價格低
			return i // 回傳當天價格是近 i 天的低點
		}
	}

	return 36500
}

func UpperPointDays(log *logrus.Logger, stockID string, today string) int {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return -1
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return -1
	}

	// 準備 SQL 指令
	// 查詢 StockHistory table 中，stock_id 為 stockID 的"當天收盤價"，並依照 date 由新到舊排序
	SQL_cmd := "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, today))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return -1
	}
	defer rows.Close()

	stockHistoryPriceRecords := make([]float64, 0)
	for rows.Next() { // 將查詢結果存入 stockHistoryPriceRecords 陣列
		var record float64
		err := rows.Scan(&record)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return -1
		}
		stockHistoryPriceRecords = append(stockHistoryPriceRecords, record)
	}

	todayPrice := stockHistoryPriceRecords[0]        // 取得當天價格
	for i, price := range stockHistoryPriceRecords { // 比較當天價格與過去價格
		if price > todayPrice { // 如果過去價格比當天價格高
			return i // 回傳當天價格是近 i 天的高點
		}
	}

	return 36500

}

func GetStockName(log *logrus.Logger, stockID string) (string, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return "", err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return "", err
	}

	// 準備 SQL 指令
	SQL_cmd := "SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return "", err
	}
	defer rows.Close()

	var stockName string
	for rows.Next() {
		err := rows.Scan(&stockName)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return "", err
		}
	}

	return stockName, nil

}

func GetAverageStockPrice(log *logrus.Logger, stockID string, today string, days int) (float64, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return -1, err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return -1, err
	}

	// 準備 SQL 指令
	// 查詢 StockHistory table 中，stock_id 為 stockID 的"當天最低價"，並依照 date 由新到舊排序
	SQL_cmd := "SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT ?;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, today, days))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID, today, days)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return -1, err
	}
	defer rows.Close()

	stockHistoryPriceRecords := make([]float64, 0)
	for rows.Next() { // 將查詢結果存入 stockHistoryPriceRecords 陣列
		var record float64
		err := rows.Scan(&record)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
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

	finalData := sum / float64(days)
	log.Info(stockID, " 過去 ", days, " 天的平均股價: ", finalData)

	return finalData, nil
}

func GetTodayStockPrice(log *logrus.Logger, stockID string, today string, priceType string) (float64, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return -1, err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return -1, err
	}

	// 準備 SQL 指令
	// 查詢 StockHistory table 中，stock_id 為 stockID 的"當天最低價"，並依照 date 由新到舊排序
	SQL_cmd := "SELECT " + priceType + " FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT 1;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, today))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return -1, err
	}
	defer rows.Close()

	var stockPrice float64
	for rows.Next() {
		err := rows.Scan(&stockPrice)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return -1, err
		}
	}
	log.Info(stockID, " 當天的 ", priceType, ": ", stockPrice)

	return stockPrice, nil
}

func SQLBuyStock(log *logrus.Logger, stockID string, today string, buyAmount float64) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// 準備 SQL 指令
	SQL_cmd := "INSERT INTO UnrealizedGainsLosses (transaction_date, stock_id, stock_name, transaction_price, investment_cost) VALUES (?, ?, (SELECT stock_name FROM StockHistory WHERE stock_id = ? ORDER BY date DESC LIMIT 1), (SELECT close_price FROM StockHistory WHERE stock_id = ? AND date <= ? ORDER BY date DESC LIMIT 1), ?);"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, today, stockID, stockID, stockID, today, buyAmount))

	// 執行 SQL 指令
	_, err = db.Exec(SQL_cmd, today, stockID, stockID, stockID, today, buyAmount)
	if err != nil {
		log.Error("db.Exec 錯誤:")
		return err
	}

	return nil
}

func SQLSellStock(log *logrus.Logger, stockID string, today string, sellAmount float64) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// 取得關於該股票 transaction_date 最新的一筆未實現損益紀錄
	// 若 revenue = sellAmount，則刪除該筆未實現損益紀錄，並離開迴圈
	// 若 revenue < sellAmount，則刪除該筆未實現損益紀錄
	// 若 revenue > sellAmount，則更新該筆未實現損益紀錄的 investment_cost = investment_cost - sellAmount*transaction_price/todayPrice，並離開迴圈
	for {
		record, err := GetLowestUnrealizedGainsLossesRecord(log, stockID, today) // 取得關於該股票 transaction_price 最低的一筆未實現損益紀錄
		if err != nil {
			log.Error("GetLowestUnrealizedGainsLossesRecord 錯誤:")
			return err
		}
		log.Info("record: ", record)

		todayClosePrice, err := GetTodayStockPrice(log, stockID, today, "close_price") // 取得當天收盤價
		if err != nil {
			log.Error("GetTodayStockPrice 錯誤:")
			return err
		}
		revenue := (todayClosePrice / record["transaction_price"].(float64)) * record["investment_cost"].(float64) // 計算這筆未實現損益紀錄全賣掉的總收益
		log.Info("revenue: ", revenue)

		if revenue == float64(sellAmount) { // 如果 revenue 剛好等於要賣的金額
			log.Info("revenue == sellAmount")
			err := DeleteLowestUnrealizedGainsLossesRecord(log, stockID, record["transaction_date"].(string)) // 刪除該筆未實現損益紀錄 (全賣掉)
			if err != nil {
				log.Error("DeleteLowestUnrealizedGainsLossesRecord 錯誤:")
				return err
			}

			profit_loss := revenue - record["investment_cost"].(float64)                               // 計算損益
			profit_rate := (float64(profit_loss) / float64(record["investment_cost"].(float64))) * 100 // 計算損益率
			log.Info("profit_loss: ", profit_loss)
			log.Info("profit_rate: ", profit_rate)

			// 將新的一筆已實現損益加入 RealizedGainsLosses table
			err = InsertToRealizedGainsLosses(log, record["transaction_date"].(string), today, stockID, record["stock_name"].(string), record["transaction_price"].(float64), todayClosePrice, record["investment_cost"].(float64), revenue, profit_loss, profit_rate)
			if err != nil {
				log.Error("InsertToRealizedGainsLosses 錯誤:")
				return err
			}
			log.Info("真實賣出金額: ", revenue)
			break // 離開迴圈
		} else if revenue < float64(sellAmount) { // 如果 revenue 小於要賣的金額
			log.Info("revenue < sellAmount")
			err := DeleteLowestUnrealizedGainsLossesRecord(log, stockID, record["transaction_date"].(string)) // 刪除該筆未實現損益紀錄 (全賣掉)
			if err != nil {
				log.Error("DeleteLowestUnrealizedGainsLossesRecord 錯誤:")
				return err
			}
			profit_loss := revenue - record["investment_cost"].(float64)                               // 計算損益
			profit_rate := (float64(profit_loss) / float64(record["investment_cost"].(float64))) * 100 // 計算損益率
			log.Info("profit_loss: ", profit_loss)
			log.Info("profit_rate: ", profit_rate)

			// 將新的一筆已實現損益加入 RealizedGainsLosses table
			err = InsertToRealizedGainsLosses(log, record["transaction_date"].(string), today, stockID, record["stock_name"].(string), record["transaction_price"].(float64), todayClosePrice, record["investment_cost"].(float64), revenue, profit_loss, profit_rate)
			if err != nil {
				log.Error("InsertToRealizedGainsLosses 錯誤:")
				return err
			}

			sellAmount -= float64(revenue) // 更新 sellAmount
			log.Info("真實賣出金額: ", revenue)

		} else if revenue > float64(sellAmount) { // 如果 revenue 大於要賣的金額
			log.Info("revenue > sellAmount")
			really_investment_cost := float64(float64(sellAmount) * record["transaction_price"].(float64) / todayClosePrice)                                                       // 計算 "其實只要投資多少，便能賣出 sellAmount 的價值"
			err := UpdateLowestUnrealizedGainsLossesRecord(log, stockID, record["investment_cost"].(float64)-float64(really_investment_cost), record["transaction_date"].(string)) // 更新該筆未實現損益紀錄的 investment_cost
			if err != nil {
				log.Error("UpdateLowestUnrealizedGainsLossesRecord 錯誤:")
				return err
			}

			profit_loss := float64(sellAmount) - float64(really_investment_cost)          // 計算損益
			profit_rate := (float64(profit_loss) / float64(really_investment_cost)) * 100 // 計算損益率
			log.Info("profit_loss: ", profit_loss)
			log.Info("profit_rate: ", profit_rate)

			// 將新的一筆已實現損益加入 RealizedGainsLosses table
			err = InsertToRealizedGainsLosses(log, record["transaction_date"].(string), today, stockID, record["stock_name"].(string), record["transaction_price"].(float64), todayClosePrice, float64(really_investment_cost), float64(sellAmount), profit_loss, profit_rate)
			if err != nil {
				log.Error("InsertToRealizedGainsLosses 錯誤:")
				return err
			}
			log.Info("真實賣出金額: ", sellAmount)
			break // 離開迴圈
		}
	}

	return nil
}

func InsertToRealizedGainsLosses(log *logrus.Logger, buy_date string, sell_date string, stock_id string, stock_name string, purchase_price float64, sell_price float64, investment_cost float64, revenue float64, profit_loss float64, profit_rate float64) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// 準備 SQL 指令
	SQL_cmd := "INSERT INTO RealizedGainsLosses (buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate))

	// 執行 SQL 指令
	_, err = db.Exec(SQL_cmd, buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate)
	if err != nil {
		log.Error("db.Exec 錯誤:")
		return err
	}

	return nil

}

func GetLowestUnrealizedGainsLossesRecord(log *logrus.Logger, stockID string, today string) (map[string]interface{}, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return nil, err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return nil, err
	}

	// 準備 SQL 指令
	SQL_cmd := "SELECT transaction_date, stock_id, stock_name, transaction_price, investment_cost FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ? AND transaction_price = ( SELECT MIN(transaction_price) FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ?) ORDER BY transaction_date ASC LIMIT 1;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, today, stockID, today))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID, today, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return nil, err
	}
	defer rows.Close()

	transaction_date := ""
	stock_id := ""
	stock_name := ""
	transaction_price := 0.0
	investment_cost := 0.0
	for rows.Next() {
		err := rows.Scan(&transaction_date, &stock_id, &stock_name, &transaction_price, &investment_cost)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return nil, err
		}
	}

	if transaction_date == "" {
		log.Warn("GetLowestUnrealizedGainsLossesRecord Query 回傳結果為空")
		return nil, fmt.Errorf("在未實現損益紀錄中找不到 stock_id: %s 的資料", stockID)
	}

	returnValue := map[string]interface{}{
		"transaction_date":  transaction_date,
		"stock_id":          stock_id,
		"stock_name":        stock_name,
		"transaction_price": transaction_price,
		"investment_cost":   investment_cost,
	}

	return returnValue, nil
}

func DeleteLowestUnrealizedGainsLossesRecord(log *logrus.Logger, stockID string, transaction_date string) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// 準備 SQL 指令
	SQL_cmd := "DELETE FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date = ?;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, transaction_date))

	// 執行 SQL 指令
	_, err = db.Exec(SQL_cmd, stockID, transaction_date)
	if err != nil {
		log.Error("db.Exec 錯誤:")
		return err
	}

	return nil
}

func UpdateLowestUnrealizedGainsLossesRecord(log *logrus.Logger, stockID string, investment_cost float64, transaction_date string) error {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return err
	}

	// 準備 SQL 指令
	SQL_cmd := "UPDATE UnrealizedGainsLosses SET investment_cost = ? WHERE stock_id = ? AND transaction_date = ?;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, investment_cost, stockID, transaction_date))

	// 執行 SQL 指令
	_, err = db.Exec(SQL_cmd, investment_cost, stockID, transaction_date)
	if err != nil {
		log.Error("db.Exec 錯誤:")
		return err
	}

	return nil
}

func GetAllUnrealizedGainsLosses(log *logrus.Logger) ([]map[string]interface{}, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return nil, err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return nil, err
	}

	// 準備 SQL 指令
	SQL_cmd := "SELECT transaction_date, stock_id, stock_name, transaction_price, investment_cost FROM UnrealizedGainsLosses ORDER BY transaction_date DESC LIMIT 500;"
	log.Info("SQL_cmd: ", SQL_cmd)

	// 執行查詢
	rows, err := db.Query(SQL_cmd)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return nil, err
	}
	defer rows.Close()

	returnValue := make([]map[string]interface{}, 0)
	for rows.Next() {
		transaction_date := ""   // 交易日期
		stock_id := ""           // 股票代號
		stock_name := ""         // 股票名稱
		transaction_price := 0.0 // 買入價格
		investment_cost := 0.0   // 投資成本
		err := rows.Scan(&transaction_date, &stock_id, &stock_name, &transaction_price, &investment_cost)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return nil, err
		}

		todayClosePrice, err := GetTodayStockPrice(log, stock_id, time.Now().Format("2006-01-02"), "close_price") // 現價
		now_value := (todayClosePrice / transaction_price) * investment_cost                                      // 現值
		predict_profit_loss := now_value - investment_cost                                                        // 預估損益
		predict_profit_rate := (predict_profit_loss / investment_cost) * 100                                      // 預估損益率

		returnValue = append(returnValue, map[string]interface{}{
			"transaction_date":    transaction_date,
			"stock_id":            stock_id,
			"stock_name":          stock_name,
			"transaction_price":   transaction_price,
			"investment_cost":     investment_cost,
			"todayClosePrice":     todayClosePrice,
			"now_value":           math.Round(now_value*100) / 100,
			"predict_profit_loss": math.Round(predict_profit_loss*100) / 100,
			"predict_profit_rate": math.Round(predict_profit_rate*100) / 100,
		})
	}

	return returnValue, nil
}

func GetAllRealizedGainsLosses(log *logrus.Logger) ([]map[string]interface{}, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return nil, err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return nil, err
	}

	// 準備 SQL 指令
	SQL_cmd := "SELECT buy_date, sell_date, stock_id, stock_name, purchase_price, sell_price, investment_cost, revenue, profit_loss, profit_rate FROM RealizedGainsLosses ORDER BY sell_date DESC LIMIT 500;"
	log.Info("SQL_cmd: ", SQL_cmd)

	// 執行查詢
	rows, err := db.Query(SQL_cmd)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return nil, err
	}
	defer rows.Close()

	returnValue := make([]map[string]interface{}, 0)
	for rows.Next() {
		buy_date := ""         // 買入日期
		sell_date := ""        // 賣出日期
		stock_id := ""         // 股票代號
		stock_name := ""       // 股票名稱
		purchase_price := 0.0  // 買入價格
		sell_price := 0.0      // 賣出價格
		investment_cost := 0.0 // 投資成本
		revenue := 0.0         // 總收益
		profit_loss := 0.0     // 損益
		profit_rate := 0.0     // 損益率
		err := rows.Scan(&buy_date, &sell_date, &stock_id, &stock_name, &purchase_price, &sell_price, &investment_cost, &revenue, &profit_loss, &profit_rate)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
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
		})
	}

	return returnValue, nil
}

func GetStockStatisticData(log *logrus.Logger) ([]map[string]interface{}, error) {
	trackStocks_market_array := strings.Split(os.Getenv("TrackStocks_Market"), "&")
	trackStocks_highDividend_array := strings.Split(os.Getenv("TrackStocks_HighDividend"), "&")
	trackStocksArray := append(trackStocks_market_array, trackStocks_highDividend_array...)

	returnValue := make([]map[string]interface{}, 0)
	for _, stockID := range trackStocksArray {
		stockName, err := GetStockName(log, stockID)
		if err != nil {
			log.Error("GetStockName 錯誤:")
			return nil, err
		}

		// 取得當天收盤價
		todayPrice, err := GetTodayStockPrice(log, stockID, time.Now().Format("2006-01-02"), "close_price")
		if err != nil {
			log.Error("GetTodayStockPrice 錯誤:")
			return nil, err
		}

		// 取得當天價格是近 i 天的高點
		upperPointDays := UpperPointDays(log, stockID, time.Now().Format("2006-01-02"))
		if upperPointDays == -1 {
			log.Error("UpperPointDays 錯誤:")
			return nil, nil
		}

		// 取得當天價格是近 i 天的低點
		lowerPointDays := LowerPointDays(log, stockID, time.Now().Format("2006-01-02"))
		if lowerPointDays == -1 {
			log.Error("LowerPointDays 錯誤:")
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

// 取得未實現損益中，某支股票的最高或最低交易價格，若無資料則回傳 -1
func GetTransactionPriceOfUnrealizedGainsLosses(log *logrus.Logger, stockID string, today string, types string) (float64, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return -1, err
	}
	defer db.Close()

	err = ConnectToDatabase(db, log, "StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Error("ConnectToDatabase 錯誤:")
		return -1, err
	}

	// 準備 SQL 指令
	SQL_cmd := "SELECT transaction_price FROM UnrealizedGainsLosses WHERE stock_id = ? AND transaction_date <= ?;"
	log.Info("SQL_cmd: ", fmt.Sprintf(SQL_cmd, stockID, today))

	// 執行查詢
	rows, err := db.Query(SQL_cmd, stockID, today)
	if err != nil {
		log.Error("db.Query 錯誤:")
		return -1, err
	}
	defer rows.Close()

	UnrealizedGainsLosses := make([]map[string]interface{}, 0)
	for rows.Next() {
		transaction_price := 0.0
		err := rows.Scan(&transaction_price)
		if err != nil {
			log.Error("rows.Scan 錯誤:")
			return -1, err
		}
		UnrealizedGainsLosses = append(UnrealizedGainsLosses, map[string]interface{}{
			"transaction_price": transaction_price,
		})
	}

	returnValue := 0.0
	if types == "Highest" {
		returnValue = -1
	} else if types == "Lowest" {
		returnValue = 1000000000
	}

	for _, record := range UnrealizedGainsLosses {
		if types == "Highest" {
			returnValue = max(returnValue, record["transaction_price"].(float64))
		} else if types == "Lowest" {
			returnValue = min(returnValue, record["transaction_price"].(float64))
		}
	}

	if returnValue == 1000000000 {
		returnValue = -1
	}

	return returnValue, nil
}
