package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/maxfield-allison/dnsweaver/internal/config"
	"github.com/maxfield-allison/dnsweaver/internal/health"
)

const (
	healthcheckTimeout        = 4 * time.Second
	processOneCommandLinePath = "/proc/1/cmdline"
)

func runHealthcheck() error {
	return runManagementProbe("/health")
}

func runReadinessCheck() error {
	return runManagementProbe("/ready")
}

func runManagementProbe(path string) error {
	configPath := config.GetConfigFilePath()
	if configPath == "" {
		configPath = processOneConfigPath()
	}

	address, port, err := config.ResolveHealthEndpoint(configPath)
	if err != nil {
		return fmt.Errorf("resolving health listener: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	return health.ProbeAddressPath(ctx, address, port, path)
}

// processOneConfigPath recovers a --config argument used to launch the main
// container process. Docker healthcheck commands are separate processes, so
// they do not otherwise inherit the main process's command-line flags.
func processOneConfigPath() string {
	commandLine, err := os.ReadFile(processOneCommandLinePath)
	if err != nil {
		return ""
	}

	return configPathFromArgs(strings.Split(string(commandLine), "\x00"))
}

func configPathFromArgs(args []string) string {
	for i, arg := range args {
		switch arg {
		case "--config", "-config":
			if i+1 < len(args) {
				return args[i+1]
			}
		default:
			for _, prefix := range []string{"--config=", "-config="} {
				if path, ok := strings.CutPrefix(arg, prefix); ok {
					return path
				}
			}
		}
	}

	return ""
}
