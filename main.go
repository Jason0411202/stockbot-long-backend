package main

import (
	"database/sql"
	"main/kernals"
	"main/logs"
	"main/sqls"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger
var db *sql.DB

func Init() {
	log = logs.InitLogger()      // 初始化 log
	err := godotenv.Load(".env") // 環境變數 .env 檔案相對於程式的路徑
	if err != nil {
		log.Fatal("無法載入 .env 檔案")
	}

	err = sqls.InitDatabase(log) // 初始化資料庫
	if err != nil {
		log.Fatal("初始化資料庫錯誤:", err)
	}
}

func main() {
	Init()
	kernals.DailyCheck(log)
}
