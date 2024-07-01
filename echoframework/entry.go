package echoframework

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"
)

func EchoInit(log *logrus.Logger) {
	e := echo.New() //建立一個 Echo 物件

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{ // 設定 CORS
		AllowOrigins: []string{"*"},                                        // 允許所有來源
		AllowMethods: []string{echo.GET, echo.PUT, echo.POST, echo.DELETE}, // 允許的 HTTP 方法
	}))

	EchoRouting(e) // 設定 routing 規則

	log.Fatal(e.Start(":8000")) // 啟動伺服器
}
