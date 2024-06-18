package device

import (
	"fmt"
	"strconv"
	"strings"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// deviceInfo 定义构造设备所需信息
type deviceInfo interface {
	GetUUID() (string, error)
	GetPaths() ([]string, error)
	GetNumaNode() (bool, int, error)
	GetTotalMemory() (uint64, error)
	GetComputeCapability() (string, error)
}

// Device 封装 pluginapi.Device 与额外的元数据和函数
type Device struct {
	pluginapi.Device
	Paths             []string
	Index             string
	TotalMemory       uint64
	ComputeCapability string
	// Replicas 存储此设备复制的总次数。如果这是 0 或 1，则设备不共享
	Replicas int
}

// Devices 包装了一个 map[string]*Device 与一些函数
type Devices map[string]*Device

// AnnotatedID 标记的设备ID，用于区分设备的副本
type AnnotatedID string

// AnnotatedIDs 包含 AnnotatedID 的切片
type AnnotatedIDs []string

// BuildDevice 根据索引和 deviceInfo 构建一个设备信息
func BuildDevice(index string, d deviceInfo) (*Device, error) {
	uuid, err := d.GetUUID()
	if err != nil {
		return nil, fmt.Errorf("error getting UUID device: %v", err)
	}

	paths, err := d.GetPaths()
	if err != nil {
		return nil, fmt.Errorf("error getting device paths: %v", err)
	}

	hasNuma, numa, err := d.GetNumaNode()
	if err != nil {
		return nil, fmt.Errorf("error getting device NUMA node: %v", err)
	}

	totalMemory, err := d.GetTotalMemory()
	if err != nil {
		return nil, fmt.Errorf("error getting device memory: %w", err)
	}

	computeCapability, err := d.GetComputeCapability()
	if err != nil {
		return nil, fmt.Errorf("error getting device compute capability: %w", err)
	}

	dev := Device{
		TotalMemory:       totalMemory,
		ComputeCapability: computeCapability,
	}
	dev.ID = uuid
	dev.Index = index
	dev.Paths = paths
	dev.Health = pluginapi.Healthy
	if hasNuma {
		dev.Topology = &pluginapi.TopologyInfo{
			Nodes: []*pluginapi.NUMANode{
				{
					ID: int64(numa),
				},
			},
		}
	}
	return &dev, nil
}

// Contains 检查所有设备是否匹配
func (ds Devices) Contains(ids ...string) bool {
	for _, id := range ids {
		if _, exists := ds[id]; !exists {
			return false
		}
	}
	return true
}

// GetByID 根据ID获取设备信息
func (ds Devices) GetByID(id string) *Device {
	return ds[id]
}

// GetByIndex 根据索引获取设备信息
func (ds Devices) GetByIndex(index string) *Device {
	for _, d := range ds {
		if d.Index == index {
			return d
		}
	}
	return nil
}

// Subset 根据ids获取设备的信息
func (ds Devices) Subset(ids []string) Devices {
	res := make(Devices)
	for _, id := range ids {
		if ds.Contains(id) {
			res[id] = ds[id]
		}
	}
	return res
}

// Difference 获取Devices 中包含但 ods 中不包含的设备集
func (ds Devices) Difference(ods Devices) Devices {
	res := make(Devices)
	for id := range ds {
		if !ods.Contains(id) {
			res[id] = ds[id]
		}
	}
	return res
}

// GetIDs 获取所有设备的ids
func (ds Devices) GetIDs() []string {
	var res []string
	for _, d := range ds {
		res = append(res, d.ID)
	}
	return res
}

// GetUUIDs 获取所有设备的uuids
func (ds Devices) GetUUIDs() []string {
	var res []string
	seen := make(map[string]bool)
	for _, d := range ds {
		uuid := d.GetUUID()
		if seen[uuid] {
			continue
		}
		seen[uuid] = true
		res = append(res, uuid)
	}
	return res
}

// GetPluginDevices 获取所有设备的pluginapi.Device
func (ds Devices) GetPluginDevices() []*pluginapi.Device {
	var res []*pluginapi.Device
	for _, device := range ds {
		d := device
		res = append(res, &d.Device)
	}
	return res
}

// GetIndices 获取 Devices 中所有设备的索引
func (ds Devices) GetIndices() []string {
	var res []string
	for _, d := range ds {
		res = append(res, d.Index)
	}
	return res
}

// GetPaths 获取所有设备的路径
func (ds Devices) GetPaths() []string {
	var res []string
	for _, d := range ds {
		res = append(res, d.Paths...)
	}
	return res
}

// AlignedAllocationSupported 检查所有设备是否支持对齐分配
func (ds Devices) AlignedAllocationSupported() bool {
	for _, d := range ds {
		if !d.AlignedAllocationSupported() {
			return false
		}
	}
	return true
}

// AlignedAllocationSupported 检查设备是否支持对齐分配
func (d Device) AlignedAllocationSupported() bool {
	if d.IsMigDevice() {
		return false
	}

	for _, p := range d.Paths {
		if p == "/dev/dxg" {
			return false
		}
	}

	return true
}

// IsMigDevice 设备是否是MIG设备
func (d Device) IsMigDevice() bool {
	return strings.Contains(d.Index, ":")
}

// GetUUID 获取设备uuid
func (d Device) GetUUID() string {
	return AnnotatedID(d.ID).GetID()
}

// NewAnnotatedID 根据ID和副本编号创建一个新的 AnnotatedID
func NewAnnotatedID(id string, replica int) AnnotatedID {
	return AnnotatedID(fmt.Sprintf("%s::%d", id, replica))
}

// HasAnnotations 检查 AnnotatedID 是否有任何标记
func (r AnnotatedID) HasAnnotations() bool {
	split := strings.SplitN(string(r), "::", 2)
	return len(split) == 2
}

// Split 获取ID和副本编号
func (r AnnotatedID) Split() (string, int) {
	split := strings.SplitN(string(r), "::", 2)
	if len(split) != 2 {
		return string(r), 0
	}
	replica, _ := strconv.ParseInt(split[1], 10, 0)
	return split[0], int(replica)
}

// GetID 获取ID
func (r AnnotatedID) GetID() string {
	id, _ := r.Split()
	return id
}

// AnyHasAnnotations 检查有任何ID是否有标记
func (rs AnnotatedIDs) AnyHasAnnotations() bool {
	for _, r := range rs {
		if AnnotatedID(r).HasAnnotations() {
			return true
		}
	}
	return false
}

// GetIDs 获取所有设备的ID
func (rs AnnotatedIDs) GetIDs() []string {
	res := make([]string, len(rs))
	for i, r := range rs {
		res[i] = AnnotatedID(r).GetID()
	}
	return res
}
