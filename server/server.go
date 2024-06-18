package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	selfmiddleware "github.com/uppercaveman/k8s-gpu-device-plugin/middleware"
	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"
	"github.com/uppercaveman/k8s-gpu-device-plugin/plugin"
	"github.com/uppercaveman/k8s-gpu-device-plugin/router"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server : http Server
type Server struct {
	pluginManager *plugin.PluginManager
	listenAddress string
	quitCh        chan struct{}
}

// New : new Server
func New(listenAddress string, pluginManager *plugin.PluginManager) *Server {
	return &Server{
		pluginManager: pluginManager,
		listenAddress: listenAddress,
		quitCh:        make(chan struct{}),
	}
}

// Run : 启动http服务
func (s *Server) Run(ctx context.Context) error {
	a := router.NewAPI(s.pluginManager)
	router.RegistRouter(a.RegistApiRouter)

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(Cros())
	e.Use(middleware.Logger())
	e.Use(selfmiddleware.MetricsMiddleware())

	router.StartRouter(e)
	e.Server.ReadTimeout = 30 * time.Second
	//打印路由列表
	routeList := e.Routes()
	for _, v := range routeList {
		if v.Method == "echo_route_not_found" {
			continue
		}
		fmt.Printf("%s  %s \n", v.Method, v.Path)
	}
	errCh := make(chan error)
	go func() {
		l.Logger.Info("web server started")
		errCh <- e.Start(s.listenAddress)
	}()

	select {
	case e := <-errCh:
		return e
	case <-ctx.Done():
		e.Shutdown(ctx)
		l.Logger.Info("web server stoped")
		return nil
	}
}

// Quit :
func (s *Server) Quit() <-chan struct{} {
	return s.quitCh
}

// Cros 跨域处理
func Cros() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			origin := c.Request().Header.Get("Origin")
			if len(origin) > 0 {
				c.Response().Writer.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				c.Response().Writer.Header().Set("Access-Control-Allow-Origin", "*")
			}
			c.Response().Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Response().Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PATCH, PUT, DELETE")
			c.Response().Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, Authorization, Origin")

			if c.Request().Method == "OPTIONS" {
				return echo.NewHTTPError(http.StatusOK)
			}
			return next(c)
		}
	}
}
