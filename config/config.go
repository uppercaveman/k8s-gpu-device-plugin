package config

import (
	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"

	"github.com/spf13/viper"
)

type Config struct {
	WebListenAddress string       `yaml:"webListenAddress"`
	MigStrategy      string       `yaml:"migStrategy"`
	Benchmark        bool         `yaml:"benchmark"`
	Log              *l.LogConfig `yaml:"log"`
}

func SetDefaultConfig() {
	viper.SetDefault("webListenAddress", "9002")
	viper.SetDefault("migStrategy", "none")
	viper.SetDefault("benchmark", false)
	viper.SetDefault("log.level", "debug")
	viper.SetDefault("log.filename", "./logs/log.log")
}
