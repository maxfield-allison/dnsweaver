package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ResolveHealthPort returns the port used by the health and metrics server.
// It follows the same precedence as Load: environment, config file, defaults.
// Only the server port is resolved so container health checks do not need to
// read provider credentials or validate unrelated configuration on every run.
func ResolveHealthPort(configPath string) (int, error) {
	port := DefaultHealthPort

	if configPath != "" {
		filePort, err := healthPortFromFile(configPath)
		if err != nil {
			return 0, err
		}
		port = filePort
	}

	port, configErr := healthPortFromEnvironment(port)
	if configErr != nil {
		return 0, configErr
	}

	return port, nil
}

func healthPortFromFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading config file: %w", err)
	}

	var fileCfg struct {
		Server *FileServerConfig `yaml:"server,omitempty"`
	}
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return 0, fmt.Errorf("parsing YAML config: %w", err)
	}

	if fileCfg.Server != nil && fileCfg.Server.Port > 0 && fileCfg.Server.Port <= 65535 {
		return fileCfg.Server.Port, nil
	}
	return DefaultHealthPort, nil
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
