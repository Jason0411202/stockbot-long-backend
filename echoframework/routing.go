package echoframework

import (
	"github.com/labstack/echo/v4"
)

func EchoRouting(e *echo.Echo) {
	e.GET("/", home)
	e.GET("/api/get_unrealized_gains_losses", get_unrealized_gains_losses)
	e.GET("/api/get_realized_gains_losses", get_realized_gains_losses)
}
