package device

import (
	"bufio"
	"fmt"
	"os"

	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"
	"go.uber.org/zap"
)

// NVIDIA-CAPS 相关的常量
const (
	nvcapsProcDriverPath = "/proc/driver/nvidia-caps"
	nvcapsMigMinorsPath  = nvcapsProcDriverPath + "/mig-minors"
	nvcapsDevicePath     = "/dev/nvidia-caps"
)

// GetMigCapabilityDevicePaths 获取 MIG 功能路径到设备节点路径的映射
func GetMigCapabilityDevicePaths() (map[string]string, error) {
	// 翻译：打开 nvcapsMigMinorsPath 进行遍历。
	// 如果 nvcapsMigMinorsPath 不存在，则我们不在支持MIG的机器上，就什么也不做。
	// 此文件的格式在以下文档中讨论：
	//     https://docs.nvidia.com/datacenter/tesla/mig-user-guide/index.html#unique_1576522674
	minorsFile, err := os.Open(nvcapsMigMinorsPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error opening MIG minors file: %v", err)
	}
	defer minorsFile.Close()

	// 定义一个函数处理 nvcapsMigMinorsPath 的每一行数据
	processLine := func(line string) (string, int, error) {
		var gpu, gi, ci, migMinor int

		// 查找 CI 访问文件
		n, _ := fmt.Sscanf(line, "gpu%d/gi%d/ci%d/access %d", &gpu, &gi, &ci, &migMinor)
		if n == 4 {
			capPath := fmt.Sprintf(nvidiaCapabilitiesPath+"/gpu%d/mig/gi%d/ci%d/access", gpu, gi, ci)
			return capPath, migMinor, nil
		}

		// 查找 GI 访问文件
		n, _ = fmt.Sscanf(line, "gpu%d/gi%d/access %d", &gpu, &gi, &migMinor)
		if n == 3 {
			capPath := fmt.Sprintf(nvidiaCapabilitiesPath+"/gpu%d/mig/gi%d/access", gpu, gi)
			return capPath, migMinor, nil
		}

		// 查找 MIG 配置文件
		n, _ = fmt.Sscanf(line, "config %d", &migMinor)
		if n == 1 {
			capPath := fmt.Sprintf(nvidiaCapabilitiesPath + "/mig/config")
			return capPath, migMinor, nil
		}

		// 查找 MIG 监控文件
		n, _ = fmt.Sscanf(line, "monitor %d", &migMinor)
		if n == 1 {
			capPath := fmt.Sprintf(nvidiaCapabilitiesPath + "/mig/monitor")
			return capPath, migMinor, nil
		}

		return "", 0, fmt.Errorf("unparsable line: %v", line)
	}

	// 遍历nvcapsMigMinorsPath的每一行，并为该功能构建一个nvidia功能路径到device minor的映射
	capsDevicePaths := make(map[string]string)
	scanner := bufio.NewScanner(minorsFile)
	for scanner.Scan() {
		capPath, migMinor, err := processLine(scanner.Text())
		if err != nil {
			l.Logger.Error("Skipping line in MIG minors file", zap.Error(err))
			continue
		}
		capsDevicePaths[capPath] = fmt.Sprintf(nvcapsDevicePath+"/nvidia-cap%d", migMinor)
	}
	return capsDevicePaths, nil
}
