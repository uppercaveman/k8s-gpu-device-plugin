package resource

import (
	"strings"

	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"go.uber.org/zap"
)

// 获取资源
func NewResources(nvmllib nvml.Interface, migStrategy string) []*Resource {
	resources := make([]*Resource, 0)
	switch migStrategy {
	case MigStrategyNone:
		resources = append(resources, NewResource("GPU", "nvidia.com/gpu"))
	case MigStrategySingle:
		resources = append(resources, NewResource("GPU", "nvidia.com/gpu"))
	case MigStrategyMixed:
		hasNVML, reason := info.New().HasNvml()
		if !hasNVML {
			l.Logger.Warn("mig-strategy is only supported with NVML", zap.String("migStrategy", MigStrategyMixed), zap.String("reason", reason))
			return nil
		}
		// 初始化NVML
		ret := nvmllib.Init()
		if ret != nvml.SUCCESS {
			l.Logger.Warn("failed to initialize NVML", zap.Error(ret))
			return nil
		}
		defer func() {
			ret := nvmllib.Shutdown()
			if ret != nvml.SUCCESS {
				l.Logger.Error("failed to shutting down NVML", zap.Error(ret))
			}
		}()
		// 初始化设备库
		devicelib := device.New(nvmllib)
		// 遍历MIG配置文件
		devicelib.VisitMigProfiles(func(mp device.MigProfile) error {
			info := mp.GetInfo()
			if info.C != info.G {
				return nil
			}
			resourceName := strings.ReplaceAll("mig-"+mp.String(), "+", ".")
			resources = append(resources, NewResource(mp.String(), resourceName))
			return nil
		})
	}
	return resources
}
