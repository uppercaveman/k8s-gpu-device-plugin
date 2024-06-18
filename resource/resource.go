package resource

import (
	"strings"
)

// 资源名称相关的常量
const (
	ResourceNamePrefix              = "nvidia.com"
	DefaultSharedResourceNameSuffix = ".shared"
	MaxResourceNameLength           = 63
)

// MIG 策略的常量
const (
	MigStrategyNone   = "none"
	MigStrategySingle = "single"
	MigStrategyMixed  = "mixed"
)

// ResourcePattern 用于将资源名称匹配到特定模式
type ResourcePattern string

// ResourceName 表示 Kubernetes 中的有效资源名称
type ResourceName string

type Resource struct {
	Pattern ResourcePattern
	Name    ResourceName
}

func NewResource(pattern, name string) *Resource {
	if !strings.HasPrefix(name, ResourceNamePrefix+"/") {
		name = ResourceNamePrefix + "/" + name
	}
	return &Resource{
		Pattern: ResourcePattern(pattern),
		Name:    ResourceName(name),
	}
}

// 获取资源名称
func (rm ResourceName) GetResourceName() string {
	_, name := rm.Split()
	return name
}

// 获取资源名称前缀
func (rm ResourceName) GetResourceNamePrefix() string {
	prefix, _ := rm.Split()
	return prefix
}

// 将完整的资源名称拆分为前缀和名称
func (rm ResourceName) Split() (string, string) {
	split := strings.SplitN(string(rm), "/", 2)
	if len(split) != 2 {
		return "", string(rm)
	}
	return split[0], split[1]
}

// 获取共享此资源时应用的默认重命名
func (rm ResourceName) DefaultSharedRename() string {
	return string(rm) + DefaultSharedResourceNameSuffix
}
