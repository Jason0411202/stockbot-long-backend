package main

import (
	"main/app_context"
	"main/discord"
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

	err = discord.InitDiscord(appCtx) // 初始化 Discord
	if err != nil {
		appCtx.Log.Error("初始化 Discord 錯誤:", err)
	}
	err = discord.SendEmbedDiscordMessage(appCtx, "📢 SYSTEM", "長線股票模擬交易系統 Discord bot 順利啟動", 0x00ff00) // 發送成功訊息
	if err != nil {
		appCtx.Log.Error("發送 Discord 訊息失敗:", err)
	}

	go echoframework.EchoInit(appCtx)
}

func main() {
	appCtx := app_context.NewAppContext()

	Init(appCtx)
	kernals.DailyCheck(appCtx)
}
