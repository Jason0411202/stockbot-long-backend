package main

import (
	"main/app_context"
	"main/echoframework"
	"main/kernals"
	"main/sqls"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

func Init(appCtx *app_context.AppContext) {
	err := godotenv.Load(".env") // 環境變數 .env 檔案相對於程式的路徑
	if err != nil {
		appCtx.Log.Error("無法載入 .env 檔案")
	}

	err = sqls.InitDatabase(appCtx) // 初始化資料庫
	if err != nil {
		appCtx.Log.Fatal("初始化資料庫錯誤:", err)
	}

	go echoframework.EchoInit(appCtx)
}

func main() {
	appCtx := app_context.NewAppContext()

	Init(appCtx)
	kernals.DailyCheck(appCtx)
}
