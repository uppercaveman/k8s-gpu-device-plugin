package router

import (
	"github.com/labstack/echo/v4"
)

type RouterRegister func(*echo.Echo)

var Routers = []RouterRegister{}

func RegistRouter(routers RouterRegister) {
	Routers = append(Routers, routers)
}

func StartRouter(e *echo.Echo) {
	for _, v := range Routers {
		v(e)
	}
}
