package sqls

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
func UpdataDatebase(db *sql.DB, log *logrus.Logger) error {
	// 生成從現在開始，往前推 1 年，每次間隔一個月的日期，格式類似 "20240501"
	now := time.Now()                     //取得現在時間
	currentDate := now.Format("20060102") // 格式化為 YYYYMMDD
	log.Info("currentDate: ", currentDate)

	// 將 currentDate 每隔一個月回推，共回推 12 次，存成 Dates 陣列，例如 [20240627 20240501 ... 20230701]
	currentYear, _ := strconv.Atoi(currentDate[:4])
	currentMonth, _ := strconv.Atoi(currentDate[4:6])

	Dates := make([]string, 0)
	Dates = append(Dates, currentDate)
	for i := 0; i < 11; i++ {
		currentMonth--
		if currentMonth == 0 {
			currentMonth = 12
			currentYear--
		}
		date := fmt.Sprintf("%d%02d%02d", currentYear, currentMonth, 1)
		Dates = append(Dates, date)
	}
	log.Info("Dates: ", Dates)

	TrackStocks := os.Getenv("TrackStocks")             //ex: 取得追蹤的股票 id 資訊，ex: 0050&0056
	TrackStocksArray := strings.Split(TrackStocks, "&") // 根據 & 切割字串
	log.Info("TrackStocksArray: ", TrackStocksArray)
	for _, stockID := range TrackStocksArray { // 依序取出每一個股票 id
		for _, date := range Dates { // 依序取出每一個日期
			datas, stockName, err := TWSEapi(date, stockID, log) // 呼叫 TWSEapi 函式，取得資料 (股票資料，股票名稱，錯誤訊息)
			if err != nil {
				log.Error("TWSEapi 發生錯誤: ")
				return err
			}

			for _, data := range datas {
				log.Info("data: ", data)
				query := `INSERT INTO StockHistory (stock_id, stock_name, date, volume, value, open_price, high_price, low_price, close_price, price_change, transactions) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
				_, err := db.Exec(query, stockID, stockName, data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8])
				log.Info("quary: ", fmt.Sprintf(query, stockID, stockName, data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8]))
				if err != nil {
					log.Error("資料庫新增資料錯誤, err:")
					return err
				}
			}

			time.Sleep(5 * time.Second) // 每次執行完 TWSEapi 函式後，休息 5 秒
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

// 程式剛啟動時需要執行的函式，負責處理資料庫的初始化
func InitDatabase(log *logrus.Logger) (*sql.DB, error) {
	db, err := ConnectToMariadb(log) // 連接至 Mariadb Server
	if err != nil {
		log.Error("ConnectToMariadb 錯誤:")
		return nil, err
	}
	defer db.Close()

	// 檢查是否連線成功
	err = db.Ping()
	if err != nil {
		log.Error("Mariadb Server Ping 失敗:")
		return nil, err
	}

	_, err = db.Exec("USE StockLongData") // 嘗試使用資料庫 "StockLongData"
	if err != nil {
		log.Warn("StockLongData 資料庫不存在:", err)
		log.Info("重新建立資料庫與對應 table 中...")

		// 若資料庫 "StockLongData" 不存在，則執行同一目錄下的腳本 SQLcommend.sql，建立資料庫以及相關 table
		sqlContent, err := os.ReadFile("./sqls/SQLcommend.sql")
		if err != nil {
			log.Error("無法讀取 SQLcommend.sql 檔案:")
			return nil, err
		}
		_, err = db.Exec(string(sqlContent))
		if err != nil {
			log.Error("執行 SQLcommend.sql 檔案錯誤:")
			return nil, err
		}

		_, err = db.Exec("USE StockLongData")
		if err != nil {
			log.Error("建立 StockLongData 資料庫失敗:")
			return nil, err
		}

		log.Info("資料庫與對應 table 建立完成")
	}

	UpdataDatebase(db, log)

	return db, nil
}
