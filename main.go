package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	bmk "github.com/uppercaveman/k8s-gpu-device-plugin/benchmark"
	"github.com/uppercaveman/k8s-gpu-device-plugin/config"
	l "github.com/uppercaveman/k8s-gpu-device-plugin/modules/log"
	"github.com/uppercaveman/k8s-gpu-device-plugin/modules/util"
	"github.com/uppercaveman/k8s-gpu-device-plugin/plugin"
	"github.com/uppercaveman/k8s-gpu-device-plugin/server"

	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func init() {
	prometheus.MustRegister(collectors.NewBuildInfoCollector())
}

func main() {
	pflag.String("configFile", "config", "name of config file (without extension)")

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	// 默认配置
	config.SetDefaultConfig()

	viper.AddConfigPath(".")
	viper.SetConfigName(viper.GetString("configFile"))
	viper.SetConfigType("yml")
	err := viper.ReadInConfig()
	if err != nil {
		log.Printf("fatal error config file: %s \n", err.Error())
	}

	cfg := new(config.Config)
	err = viper.Unmarshal(cfg)
	if err != nil {
		log.Panic("fatal unmarshal config", err.Error())
		return
	}

	// log
	err = l.InitLogger(*cfg.Log, "k8s-gpu-device-plugin")
	if err != nil {
		log.Panic("init logger failed", err.Error())
		return
	}
	l.Logger.Info("Starting k8s-gpu-device-plugin Server...")

	// plugin manager Ready
	pluginReady := &util.CloseOnce{
		C: make(chan struct{}),
	}

	pluginReady.Close = func() {
		pluginReady.Once.Do(func() {
			close(pluginReady.C)
		})
	}

	// plugin manager
	pluginManager := plugin.NewPluginManager(cfg.MigStrategy, pluginReady)

	// web server
	webServer := server.New(cfg.WebListenAddress, pluginManager)
	ctxWeb, cancelWeb := context.WithCancel(context.Background())
	var g run.Group
	{
		// Termination handler.
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
		cancel := make(chan struct{})
		g.Add(
			func() error {
				select {
				case sig := <-term:
					switch sig {
					case syscall.SIGINT:
						log.Println("messaged SIGINT, exiting gracefully...")
					case syscall.SIGTERM:
						log.Println("messaged SIGTERM, exiting gracefully...")
					case syscall.SIGHUP:
						log.Println("messaged SIGHUP, exiting gracefully...")
					case syscall.SIGQUIT:
						log.Println("messaged SIGQUIT, exiting gracefully...")
					default:
						log.Printf("messaged %s, exiting gracefully...", sig.String())
					}
				case <-cancel:
					pluginReady.Close()
					log.Println("canceled, exiting gracefully...")
				}
				return nil
			},
			func(err error) {
				close(cancel)
			},
		)
	}
	{
		// Plugin Manager.
		g.Add(
			func() error {
				pluginManager.Start()
				return nil
			},
			func(err error) {
				pluginManager.Stop()
			},
		)
	}
	{
		// Web Server.
		g.Add(
			func() error {
				<-pluginReady.C
				if err := webServer.Run(ctxWeb); err != nil {
					return fmt.Errorf("error starting web server : %s", err)
				}
				return nil
			},
			func(err error) {
				cancelWeb()
			},
		)
	}

	// Benchmark.
	if cfg.Benchmark {
		// benchmark
		bench, err := bmk.NewBenchmark(l.Logger.With(zap.String("component", "benchmark")), "")
		if err != nil {
			log.Fatal("new benchmark err : ", err.Error())
			os.Exit(1)
		}

		if err := bench.Run(); err != nil {
			log.Fatal(err.Error())
			os.Exit(1)
		}
		defer bench.Stop()
	}

	if err := g.Run(); err != nil {
		log.Fatal(err.Error())
		os.Exit(1)
	}

	log.Println("see you next time!")
}
