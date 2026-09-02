package config

import (
	"fmt"
	"strconv"
)

// ResolveHealthPort returns the port used by the health and metrics server.
// It follows the same precedence as Load: environment, config file, defaults.
// Only the server port is resolved so container health checks do not need to
// read provider credentials or validate unrelated configuration on every run.
func ResolveHealthPort(configPath string) (int, error) {
	port := DefaultHealthPort

	if configPath != "" {
		fileCfg, err := LoadFile(configPath)
		if err != nil {
			return 0, err
		}
		port = fileCfg.ToGlobalConfig().HealthPort
	}

	port, configErr := healthPortFromEnvironment(port)
	if configErr != nil {
		return 0, configErr
	}

	return port, nil
}

func healthPortFromEnvironment(fallback int) (int, *ConfigError) {
	portStr := getEnv("DNSWEAVER_HEALTH_PORT")
	if portStr == "" {
		return fallback, nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fallback, configErrFull(
			"DNSWEAVER_HEALTH_PORT",
			fmt.Sprintf("invalid integer %q", portStr),
			"Must be a valid TCP port number",
			"DNSWEAVER_HEALTH_PORT=8080",
		)
	}
	if port < 1 || port > 65535 {
		return fallback, configErrFull(
			"DNSWEAVER_HEALTH_PORT",
			fmt.Sprintf("must be between 1 and 65535, got %d", port),
			"Choose an unprivileged port (1024-65535)",
			"DNSWEAVER_HEALTH_PORT=8080",
		)
	}

	return port, nil
}
