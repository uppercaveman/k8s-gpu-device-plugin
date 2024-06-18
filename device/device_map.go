package device

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uppercaveman/k8s-gpu-device-plugin/resource"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type deviceMapBuilder struct {
	device.Interface
	migStrategy string
	resources   []*resource.Resource
}

// DeviceMap 存储每个资源名称的设备集
type DeviceMap map[string]Devices

// NewDeviceMap 为指定的 NVML 库和配置创建设备映射
func NewDeviceMap(nvmllib nvml.Interface, resources []*resource.Resource, migStrategy string) (DeviceMap, error) {
	b := deviceMapBuilder{
		Interface:   device.New(nvmllib),
		resources:   resources,
		migStrategy: migStrategy,
	}
	return b.build()
}

// 资源名称与设备的映射
func (b *deviceMapBuilder) build() (DeviceMap, error) {
	switch b.migStrategy {
	case resource.MigStrategyNone:
		return b.buildGPUDeviceMap()
	case resource.MigStrategySingle:
		return b.buildGPUDeviceMap()
	case resource.MigStrategyMixed:
		return b.buildMigDeviceMap()
	default:
		return nil, fmt.Errorf("invalid MIG strategy: %v", b.migStrategy)
	}
}

// 构建资源名称到 GPU 设备的映射
func (b *deviceMapBuilder) buildGPUDeviceMap() (DeviceMap, error) {
	devices := make(DeviceMap)
	err := b.VisitDevices(func(i int, gpu device.Device) error {
		name, ret := gpu.GetName()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting product name for GPU: %v", ret)
		}
		migEnabled, err := gpu.IsMigEnabled()
		if err != nil {
			return fmt.Errorf("error checking if MIG is enabled on GPU: %v", err)
		}
		if migEnabled && b.migStrategy != resource.MigStrategyNone {
			return nil
		}
		for _, resource := range b.resources {
			b, err := regexp.MatchString(wildCardToRegexp(string(resource.Pattern)), name)
			if err != nil {
				return fmt.Errorf("error matching resource pattern: %v", err)
			}
			if b {
				index, info := newGPUDevice(i, gpu)
				return devices.setEntry(resource.Name, index, info)
			}
		}
		return fmt.Errorf("GPU name '%v' does not match any resource patterns", name)
	})
	return devices, err
}

// 构建资源名称到 MIG 设备的映射
func (b *deviceMapBuilder) buildMigDeviceMap() (DeviceMap, error) {
	devices := make(DeviceMap)
	err := b.VisitMigDevices(func(i int, d device.Device, j int, mig device.MigDevice) error {
		migProfile, err := mig.GetProfile()
		if err != nil {
			return fmt.Errorf("error getting MIG profile for MIG device at index '(%v, %v)': %v", i, j, err)
		}
		for _, resource := range b.resources {
			b, err := regexp.MatchString(wildCardToRegexp(string(resource.Pattern)), migProfile.String())
			if err != nil {
				return fmt.Errorf("error matching resource pattern: %v", err)
			}
			if b {
				index, info := newMigDevice(i, j, mig)
				return devices.setEntry(resource.Name, index, info)
			}
		}
		return fmt.Errorf("MIG profile '%v' does not match any resource patterns", migProfile)
	})
	return devices, err
}

// 设置 DeviceMap
func (d DeviceMap) setEntry(name resource.ResourceName, index string, device deviceInfo) error {
	dev, err := BuildDevice(index, device)
	if err != nil {
		return fmt.Errorf("error building Device: %v", err)
	}
	if d[string(name)] == nil {
		d[string(name)] = make(Devices)
	}
	d[string(name)][dev.ID] = dev
	return nil
}

// 将通配符模式转换为正则表达式形式
func wildCardToRegexp(pattern string) string {
	var result strings.Builder
	for i, literal := range strings.Split(pattern, "*") {
		// 将 * 替换为 .*
		if i > 0 {
			result.WriteString(".*")
		}
		// 在文本中引用任何正则表达式字符
		result.WriteString(regexp.QuoteMeta(literal))
	}
	return result.String()
}
