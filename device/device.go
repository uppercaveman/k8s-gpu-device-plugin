package device

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// NVIDIA 相关的常量
const (
	nvidiaProcDriverPath   = "/proc/driver/nvidia"
	nvidiaCapabilitiesPath = nvidiaProcDriverPath + "/capabilities"
)

// device wraps a nvml.Device to provide device specific functions.
type nvmlDevice struct {
	nvml.Device
}

// nvmlMigDevice wraps a nvml.Device to provide MIG specific functions.
type nvmlMigDevice nvmlDevice

func newGPUDevice(i int, gpu nvml.Device) (string, nvmlDevice) {
	return fmt.Sprintf("%v", i), nvmlDevice{gpu}
}

func newMigDevice(i int, j int, mig nvml.Device) (string, nvmlMigDevice) {
	return fmt.Sprintf("%v:%v", i, j), nvmlMigDevice{mig}
}

// GetUUID returns the UUID of the device
func (d nvmlDevice) GetUUID() (string, error) {
	uuid, ret := d.Device.GetUUID()
	if ret != nvml.SUCCESS {
		return "", ret
	}
	return uuid, nil
}

// GetPaths returns the paths for a GPU device
func (d nvmlDevice) GetPaths() ([]string, error) {
	isWsl, _ := info.New().HasDXCore()
	if isWsl {
		return []string{"/dev/dxg"}, nil
	}
	minor, ret := d.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device minor number: %v", ret)
	}
	path := fmt.Sprintf("/dev/nvidia%d", minor)
	return []string{path}, nil
}

// GetComputeCapability returns the CUDA Compute Capability for the device.
func (d nvmlDevice) GetComputeCapability() (string, error) {
	major, minor, ret := d.Device.GetCudaComputeCapability()
	if ret != nvml.SUCCESS {
		return "", ret
	}
	return fmt.Sprintf("%d.%d", major, minor), nil
}

// GetNumaNode returns the NUMA node associated with the GPU device
func (d nvmlDevice) GetNumaNode() (bool, int, error) {
	info, ret := d.GetPciInfo()
	if ret != nvml.SUCCESS {
		return false, 0, fmt.Errorf("error getting PCI Bus Info of device: %v", ret)
	}

	// Discard leading zeros.
	busID := strings.ToLower(strings.TrimPrefix(int8Slice(info.BusId[:]).String(), "0000"))

	b, err := os.ReadFile(fmt.Sprintf("/sys/bus/pci/devices/%s/numa_node", busID))
	if err != nil {
		return false, 0, nil
	}

	node, err := strconv.Atoi(string(bytes.TrimSpace(b)))
	if err != nil {
		return false, 0, fmt.Errorf("eror parsing value for NUMA node: %v", err)
	}

	if node < 0 {
		return false, 0, nil
	}

	return true, node, nil
}

// GetTotalMemory returns the total memory available on the device.
func (d nvmlDevice) GetTotalMemory() (uint64, error) {
	info, ret := d.Device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return 0, ret
	}
	return info.Total, nil
}

// GetUUID returns the UUID of the device
func (d nvmlMigDevice) GetUUID() (string, error) {
	return nvmlDevice(d).GetUUID()
}

// GetComputeCapability returns the CUDA Compute Capability for the device.
func (d nvmlMigDevice) GetComputeCapability() (string, error) {
	parent, ret := d.Device.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return "", fmt.Errorf("failed to get parent device: %w", ret)
	}
	return nvmlDevice{parent}.GetComputeCapability()
}

// GetNumaNode for a MIG device is the NUMA node of the parent device.
func (d nvmlMigDevice) GetNumaNode() (bool, int, error) {
	parent, ret := d.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return false, 0, fmt.Errorf("error getting parent GPU device from MIG device: %v", ret)
	}

	return nvmlDevice{parent}.GetNumaNode()
}

// GetPaths returns the paths for a MIG device
func (d nvmlMigDevice) GetPaths() ([]string, error) {
	capDevicePaths, err := GetMigCapabilityDevicePaths()
	if err != nil {
		return nil, fmt.Errorf("error getting MIG capability device paths: %v", err)
	}

	gi, ret := d.GetGpuInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU Instance ID: %v", ret)
	}

	ci, ret := d.GetComputeInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute Instance ID: %v", ret)
	}

	parent, ret := d.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting parent device: %v", ret)
	}
	minor, ret := parent.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device minor number: %v", ret)
	}
	parentPath := fmt.Sprintf("/dev/nvidia%d", minor)

	giCapPath := fmt.Sprintf(nvidiaCapabilitiesPath+"/gpu%d/mig/gi%d/access", minor, gi)
	if _, exists := capDevicePaths[giCapPath]; !exists {
		return nil, fmt.Errorf("missing MIG GPU instance capability path: %v", giCapPath)
	}

	ciCapPath := fmt.Sprintf(nvidiaCapabilitiesPath+"/gpu%d/mig/gi%d/ci%d/access", minor, gi, ci)
	if _, exists := capDevicePaths[ciCapPath]; !exists {
		return nil, fmt.Errorf("missing MIG GPU instance capability path: %v", giCapPath)
	}

	devicePaths := []string{
		parentPath,
		capDevicePaths[giCapPath],
		capDevicePaths[ciCapPath],
	}

	return devicePaths, nil
}

// GetTotalMemory returns the total memory available on the device.
func (d nvmlMigDevice) GetTotalMemory() (uint64, error) {
	info, ret := d.Device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return 0, ret
	}
	return info.Total, nil
}

// int8Slice wraps an []int8 with more functions.
type int8Slice []int8

// String turns a nil terminated int8Slice into a string
func (s int8Slice) String() string {
	var b []byte
	for _, c := range s {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
