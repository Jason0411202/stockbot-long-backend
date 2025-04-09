package echoframework

import (
	"main/app_context"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func EchoInit(appCtx *app_context.AppContext) {
	e := echo.New() //建立一個 Echo 物件

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{ // 設定 CORS
		AllowOrigins: []string{"*"},                                        // 允許所有來源
		AllowMethods: []string{echo.GET, echo.PUT, echo.POST, echo.DELETE}, // 允許的 HTTP 方法
	}))

	EchoRouting(e) // 設定 routing 規則

	// 啟動 https 伺服器
	appCtx.Log.Fatal(e.StartTLS(":8000", "cert.pem", "privkey.pem"))
}
