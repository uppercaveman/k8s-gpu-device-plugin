package plugin

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uppercaveman/k8s-gpu-device-plugin/device"
	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"
	"github.com/uppercaveman/k8s-gpu-device-plugin/resource"
	"go.uber.org/zap"

	"github.com/NVIDIA/go-gpuallocator/gpuallocator"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type Interface interface {
	Devices() device.Devices
	Start() error
	Stop() error
}

// NvidiaDevicePlugin k8s设备插件管理
type NvidiaDevicePlugin struct {
	resourceName resource.ResourceName
	devices      device.Devices
	nvmllib      nvml.Interface
	socket       string
	server       *grpc.Server
	health       chan *device.Device
	stop         chan interface{}
}

// NewNvidiaDevicePlugin 创建Nvidia设备插件管理
func NewNvidiaDevicePlugin(resourceName resource.ResourceName, devices device.Devices) (*NvidiaDevicePlugin, error) {
	pluginName := "nvidia-" + resourceName.GetResourceName()
	pluginPath := filepath.Join(pluginapi.DevicePluginPath, pluginName)
	plugin := NvidiaDevicePlugin{
		resourceName: resourceName,
		devices:      devices,
		socket:       pluginPath + ".sock",
		server:       grpc.NewServer([]grpc.ServerOption{}...),
		health:       make(chan *device.Device),
		stop:         make(chan interface{}),
	}
	return &plugin, nil
}

func (plugin *NvidiaDevicePlugin) cleanup() {
	close(plugin.stop)
}

func (plugin *NvidiaDevicePlugin) Devices() device.Devices {
	return plugin.devices
}

// 启动设备插件
func (plugin *NvidiaDevicePlugin) Start() error {
	err := plugin.Serve()
	if err != nil {
		l.Logger.Info("Could not start device plugin", zap.String("resourceName", string(plugin.resourceName)), zap.Error(err))
		plugin.cleanup()
		return err
	}
	l.Logger.Info("Starting to serve", zap.String("resourceName", string(plugin.resourceName)), zap.String("socket", plugin.socket))
	err = plugin.Register()
	if err != nil {
		l.Logger.Info("Could not register device plugin", zap.String("resourceName", string(plugin.resourceName)), zap.Error(err))
		return errors.Join(err, plugin.Stop())
	}
	l.Logger.Info("Registered device plugin for", zap.String("resourceName", string(plugin.resourceName)))
	return nil
}

// 停止设备插件
func (plugin *NvidiaDevicePlugin) Stop() error {
	if plugin == nil || plugin.server == nil {
		return nil
	}
	l.Logger.Info("Stopping to serve", zap.String("resourceName", string(plugin.resourceName)), zap.String("socket", plugin.socket))
	plugin.server.Stop()
	if err := os.Remove(plugin.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	plugin.cleanup()
	return nil
}

// 启动设备插件的gRPC服务器
func (plugin *NvidiaDevicePlugin) Serve() error {
	os.Remove(plugin.socket)
	sock, err := net.Listen("unix", plugin.socket)
	if err != nil {
		return err
	}
	pluginapi.RegisterDevicePluginServer(plugin.server, plugin)
	go func() {
		lastCrashTime := time.Now()
		restartCount := 0
		for {
			if restartCount > 5 {
				l.Logger.Fatal("GRPC server for '%s' has repeatedly crashed recently. Quitting", zap.String("resourceName", string(plugin.resourceName)))
			}
			l.Logger.Info("Starting GRPC server for '%s'", zap.String("resourceName", string(plugin.resourceName)))
			err := plugin.server.Serve(sock)
			if err == nil {
				break
			}
			l.Logger.Error("GRPC server for '%s' crashed with error: %v", zap.String("resourceName", string(plugin.resourceName)), zap.Error(err))

			timeSinceLastCrash := time.Since(lastCrashTime).Seconds()
			lastCrashTime = time.Now()
			if timeSinceLastCrash > 3600 {
				restartCount = 0
			} else {
				restartCount++
			}
		}
	}()
	conn, err := plugin.dial(plugin.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// 注册设备插件
func (plugin *NvidiaDevicePlugin) Register() error {
	conn, err := plugin.dial(pluginapi.KubeletSocket, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(plugin.socket),
		ResourceName: string(plugin.resourceName),
		Options: &pluginapi.DevicePluginOptions{
			GetPreferredAllocationAvailable: true,
		},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// 插件的可选设置值
func (plugin *NvidiaDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	options := &pluginapi.DevicePluginOptions{
		GetPreferredAllocationAvailable: true,
	}
	return options, nil
}

// 更新设备列表
func (plugin *NvidiaDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	if err := s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.Devices().GetPluginDevices()}); err != nil {
		return err
	}
	for {
		select {
		case <-plugin.stop:
			return nil
		case d := <-plugin.health:
			d.Health = pluginapi.Unhealthy
			l.Logger.Info("'%s' device marked unhealthy: %s", zap.String("resourceName", string(plugin.resourceName)), zap.String("deviceID", d.ID))
			if err := s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.Devices().GetPluginDevices()}); err != nil {
				return nil
			}
		}
	}
}

// 指定的设备集的首选分配
func (plugin *NvidiaDevicePlugin) GetPreferredAllocation(ctx context.Context, r *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	response := &pluginapi.PreferredAllocationResponse{}
	for _, req := range r.ContainerRequests {
		devices, err := plugin.getPreferredAllocation(req.AvailableDeviceIDs, req.MustIncludeDeviceIDs, int(req.AllocationSize))
		if err != nil {
			return nil, fmt.Errorf("error getting list of preferred allocation devices: %v", err)
		}

		resp := &pluginapi.ContainerPreferredAllocationResponse{
			DeviceIDs: devices,
		}

		response.ContainerResponses = append(response.ContainerResponses, resp)
	}
	return response, nil
}

// 返回设备列表
func (plugin *NvidiaDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	responses := pluginapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		b := plugin.devices.Contains(req.DevicesIDs...)
		if !b {
			return nil, fmt.Errorf("invalid allocation request for %s", plugin.resourceName)
		}
		response := pluginapi.ContainerAllocateResponse{
			Envs: map[string]string{
				"NVIDIA_VISIBLE_DEVICES": strings.Join(req.DevicesIDs, ","),
			},
		}
		responses.ContainerResponses = append(responses.ContainerResponses, &response)
	}
	return &responses, nil
}

func (plugin *NvidiaDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (plugin *NvidiaDevicePlugin) dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	c, err := grpc.DialContext(ctx, unixSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}),
	)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (plugin *NvidiaDevicePlugin) getPreferredAllocation(availableDeviceIDs []string, mustIncludeDeviceIDs []string, allocationSize int) ([]string, error) {
	if plugin.devices.AlignedAllocationSupported() && !device.AnnotatedIDs(availableDeviceIDs).AnyHasAnnotations() {
		return plugin.alignedAlloc(availableDeviceIDs, mustIncludeDeviceIDs, allocationSize)
	}
	// 将它们均匀分配到所有复制的GPU上
	return plugin.distributedAlloc(availableDeviceIDs, mustIncludeDeviceIDs, allocationSize)
}

func (plugin *NvidiaDevicePlugin) alignedAlloc(available, required []string, size int) ([]string, error) {
	var devices []string

	linkedDevices, err := gpuallocator.NewDevices(
		gpuallocator.WithNvmlLib(plugin.nvmllib),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get device link information: %w", err)
	}

	availableDevices, err := linkedDevices.Filter(available)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve list of available devices: %v", err)
	}

	requiredDevices, err := linkedDevices.Filter(required)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve list of required devices: %v", err)
	}

	allocatedDevices := gpuallocator.NewBestEffortPolicy().Allocate(availableDevices, requiredDevices, size)
	for _, device := range allocatedDevices {
		devices = append(devices, device.UUID)
	}

	return devices, nil
}

func (plugin *NvidiaDevicePlugin) distributedAlloc(available, required []string, size int) ([]string, error) {
	candidates := plugin.devices.Subset(available).Difference(plugin.devices.Subset(required)).GetIDs()
	needed := size - len(required)

	if len(candidates) < needed {
		return nil, fmt.Errorf("not enough available devices to satisfy allocation")
	}

	replicas := make(map[string]*struct{ total, available int })
	for _, c := range candidates {
		id := device.AnnotatedID(c).GetID()
		if _, exists := replicas[id]; !exists {
			replicas[id] = &struct{ total, available int }{}
		}
		replicas[id].available++
	}
	for d := range plugin.devices {
		id := device.AnnotatedID(d).GetID()
		if _, exists := replicas[id]; !exists {
			continue
		}
		replicas[id].total++
	}

	var devices []string
	for i := 0; i < needed; i++ {
		sort.Slice(candidates, func(i, j int) bool {
			iid := device.AnnotatedID(candidates[i]).GetID()
			jid := device.AnnotatedID(candidates[j]).GetID()
			idiff := replicas[iid].total - replicas[iid].available
			jdiff := replicas[jid].total - replicas[jid].available
			return idiff < jdiff
		})
		id := device.AnnotatedID(candidates[0]).GetID()
		replicas[id].available--
		devices = append(devices, candidates[0])
		candidates = candidates[1:]
	}

	devices = append(required, devices...)

	return devices, nil
}
