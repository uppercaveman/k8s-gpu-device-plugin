package router

import (
	"net/http"

	"github.com/uppercaveman/k8s-gpu-device-plugin/modules/util"
	"github.com/uppercaveman/k8s-gpu-device-plugin/modules/version"
	"github.com/uppercaveman/k8s-gpu-device-plugin/plugin"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// API :
type API struct {
	pluginManager *plugin.PluginManager
}

// NewAPI : new api
func NewAPI(pluginManager *plugin.PluginManager) *API {
	return &API{
		pluginManager: pluginManager,
	}
}

// Router : Router
func (a *API) RegistApiRouter(e *echo.Echo) {
	root := e.Group("")
	// Version
	root.GET("/", a.Version)
	// 监控指标
	root.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	// 服务健康检查
	root.GET("/health", a.Health)
	// 重启服务
	root.GET("/restart", a.Restart)
}

// Version : 版本信息
func (a *API) Version(c echo.Context) error {
	return c.JSON(http.StatusOK, util.Success("version : "+version.Version))
}

// Health : 健康检查
func (a *API) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, util.Success("ok"))
}

// Restart : 重启服务
func (a *API) Restart(c echo.Context) error {
	// 重启服务
	a.pluginManager.Restart()
	return c.JSON(http.StatusOK, util.Success("ok"))
}
