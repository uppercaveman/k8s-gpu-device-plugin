package plugin

import (
	"context"
	"time"

	"github.com/uppercaveman/k8s-gpu-device-plugin/device"
	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"
	"github.com/uppercaveman/k8s-gpu-device-plugin/modules/util"
	"github.com/uppercaveman/k8s-gpu-device-plugin/modules/watch"
	"github.com/uppercaveman/k8s-gpu-device-plugin/resource"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type PluginManager struct {
	server         *grpc.Server
	socket         string
	migStrategy    string
	devices        device.DeviceMap
	nvmllib        nvml.Interface
	resources      []*resource.Resource
	plugins        []Interface
	started        bool
	restart        bool
	restartTimeout <-chan time.Time
	ctx            context.Context
	cancel         context.CancelFunc
	ready          *util.CloseOnce
}

func NewPluginManager(migStrategy string, ready *util.CloseOnce) *PluginManager {
	ctx, cancel := context.WithCancel(context.Background())
	// 插件路径
	pluginPath := pluginapi.DevicePluginPath + "k8s-gpu-device-plugin.sock"
	// 创建插件管理器
	pm := new(PluginManager)
	pm.server = grpc.NewServer([]grpc.ServerOption{}...)
	pm.socket = pluginPath
	pm.nvmllib = nvml.New()
	pm.migStrategy = migStrategy
	pm.resources = resource.NewResources(pm.nvmllib, pm.migStrategy)
	pm.plugins = make([]Interface, 0)
	pm.started = false
	pm.restart = false
	pm.restartTimeout = nil
	pm.ctx = ctx
	pm.cancel = cancel
	return pm
}

func (p *PluginManager) Start() {
	l.Logger.Info("starting plugin server...")
	// 监听文件系统
	watcher, err := watch.Files(pluginapi.DevicePluginPath)
	if err != nil {
		l.Logger.Error("failed to create FS watcher", zap.String("DevicePluginPath", pluginapi.DevicePluginPath), zap.Error(err))
		return
	}
	// 加载插件
	err = p.loadPlugins()
	if err != nil {
		l.Logger.Error("failed to load plugins", zap.Error(err))
		return
	}
	// 启动插件
	p.startPlugins()
	p.ready.Close()
	for {
		select {
		// 报错重新启动插件
		case <-p.restartTimeout:
			p.startPlugins()
			p.restartTimeout = nil
		// 通过监听'kubelet.socket'文件来检测kubelet重新启动。当发生这种情况时，重新启动所有插件
		case event := <-watcher.Events:
			if event.Name == pluginapi.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				l.Logger.Info("restart plugins", zap.String("event", event.String()), zap.String("name", event.Name))
				p.restartPlugins()
			}
		// 记录监听事件错误
		case err := <-watcher.Errors:
			l.Logger.Error("fs error", zap.Error(err))
		// 退出
		case <-p.ctx.Done():
			l.Logger.Info("plugin server stopped")
			watcher.Close()
			p.stopPlugins()
		default:
			if p.restart {
				p.restartPlugins()
			}
		}
	}
}

// Stop : 停止服务
func (p *PluginManager) Stop() {
	l.Logger.Info("stopping plugin server...")
	p.cancel()
}

// Restart : 重启服务
func (p *PluginManager) Restart() {
	p.restart = true
}

// startPlugins : 启动插件
func (p *PluginManager) startPlugins() {
	// 如果插件已启动，则停止插件
	if p.started {
		p.stopPlugins()
	}
	p.started = true
	started := 0
	restart := false
	for _, p := range p.plugins {
		if len(p.Devices()) == 0 {
			continue
		}
		if err := p.Start(); err != nil {
			restart = true
			l.Logger.Error("Failed to start plugin", zap.Error(err))
			break
		}
		started++
	}
	if started == 0 {
		l.Logger.Info("No devices found. Waiting indefinitely.")
	}
	if restart {
		l.Logger.Info("Failed to start one or more plugins. Retrying in 30s...")
		p.restartTimeout = time.After(30 * time.Second)
	}
	l.Logger.Info("All plugins started.")
}

// stopPlugins : 停止插件
func (p *PluginManager) stopPlugins() {
	for _, p := range p.plugins {
		if len(p.Devices()) == 0 {
			continue
		}
		if err := p.Stop(); err != nil {
			l.Logger.Error("Failed to stop plugin", zap.Error(err))
			continue
		}
	}
}

// loadPlugins : 加载插件
func (p *PluginManager) loadPlugins() error {
	// 创建设备映射
	dmp, err := device.NewDeviceMap(p.nvmllib, p.resources, p.migStrategy)
	if err != nil {
		l.Logger.Error("failed to create device map", zap.Error(err))
		return err
	}
	p.devices = dmp
	// 创建插件
	for k, v := range p.devices {
		pl, err := NewNvidiaDevicePlugin(resource.ResourceName(k), v)
		if err != nil {
			l.Logger.Error("failed to create device plugin", zap.Error(err))
			return err
		}
		p.plugins = append(p.plugins, pl)
	}
	return nil
}

// restartPlugins : 重启插件
func (p *PluginManager) restartPlugins() error {
	// 如果插件已启动，则停止插件
	if p.started {
		p.stopPlugins()
	}
	p.devices = nil
	p.plugins = make([]Interface, 0)
	// 加载插件
	err := p.loadPlugins()
	if err != nil {
		l.Logger.Error("failed to load plugins", zap.Error(err))
		return err
	}
	// 启动插件
	p.startPlugins()
	p.restart = false
	return nil
}
