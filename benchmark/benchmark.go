package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"

	"go.uber.org/zap"
)

// Benchmark :
type Benchmark struct {
	outPath   string
	cpuprof   *os.File
	memprof   *os.File
	blockprof *os.File
	mtxprof   *os.File
	logger    *zap.Logger
}

// NewBenchmark :
func NewBenchmark(logger *zap.Logger, outPath string) (*Benchmark, error) {
	if outPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp(cwd, "temp_bench")
		if err != nil {
			return nil, err
		}
		outPath = dir
	}

	logger.Debug("out Path", zap.String("out_path", outPath))

	if err := os.RemoveAll(outPath); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(outPath, 0777); err != nil {
		return nil, err
	}

	return &Benchmark{
		logger:  logger,
		outPath: outPath,
	}, nil
}

// Run :
func (b *Benchmark) Run() error {
	var err error

	// Start CPU profiling.
	b.cpuprof, err = os.Create(filepath.Join(b.outPath, "cpu.prof"))
	if err != nil {
		return fmt.Errorf("bench: could not create cpu profile: %v", err)
	}
	if err := pprof.StartCPUProfile(b.cpuprof); err != nil {
		return fmt.Errorf("bench: could not start CPU profile: %v", err)
	}

	// Start memory profiling.
	b.memprof, err = os.Create(filepath.Join(b.outPath, "mem.prof"))
	if err != nil {
		return fmt.Errorf("bench: could not create memory profile: %v", err)
	}
	runtime.MemProfileRate = 64 * 1024

	// Start fatal profiling.
	b.blockprof, err = os.Create(filepath.Join(b.outPath, "block.prof"))
	if err != nil {
		return fmt.Errorf("bench: could not create block profile: %v", err)
	}
	runtime.SetBlockProfileRate(20)

	// Start mutex profiling.
	b.mtxprof, err = os.Create(filepath.Join(b.outPath, "mutex.prof"))
	if err != nil {
		return fmt.Errorf("bench: could not create mutex profile: %v", err)
	}
	runtime.SetMutexProfileFraction(20)

	b.logger.Info("Benchmark started")
	return nil
}

// Stop :
func (b *Benchmark) Stop() error {
	if b.cpuprof != nil {
		pprof.StopCPUProfile()
		b.cpuprof.Close()
		b.cpuprof = nil
	}
	if b.memprof != nil {
		if err := pprof.Lookup("heap").WriteTo(b.memprof, 0); err != nil {
			return fmt.Errorf("error writing mem profile: %v", err)
		}
		b.memprof.Close()
		b.memprof = nil
	}
	if b.blockprof != nil {
		if err := pprof.Lookup("block").WriteTo(b.blockprof, 0); err != nil {
			return fmt.Errorf("error writing block profile: %v", err)
		}
		b.blockprof.Close()
		b.blockprof = nil
		runtime.SetBlockProfileRate(0)
	}
	if b.mtxprof != nil {
		if err := pprof.Lookup("mutex").WriteTo(b.mtxprof, 0); err != nil {
			return fmt.Errorf("error writing mutex profile: %v", err)
		}
		b.mtxprof.Close()
		b.mtxprof = nil
		runtime.SetMutexProfileFraction(0)
	}

	b.logger.Info("Benchmark stopped")
	return nil
}
