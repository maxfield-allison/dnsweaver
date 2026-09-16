package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolveHealthEndpoint returns the address and port used by the health and
// metrics server. It follows the same precedence as Load: environment, config
// file, defaults. Only server settings are resolved so container health checks
// do not need to read provider credentials or validate unrelated configuration.
func ResolveHealthEndpoint(configPath string) (string, int, error) {
	address := DefaultHealthAddress
	allowNetwork := DefaultHealthAllowNetwork
	port := DefaultHealthPort

	if configPath != "" {
		fileAddress, fileAllowNetwork, filePort, err := healthEndpointFromFile(configPath)
		if err != nil {
			return "", 0, err
		}
		address = fileAddress
		allowNetwork = fileAllowNetwork
		port = filePort
	}

	resolvedAddress, _, listenerErrs := healthListenerFromEnvironment(address, allowNetwork)
	if len(listenerErrs) > 0 {
		return "", 0, listenerErrs[0]
	}
	address = resolvedAddress

	port, configErr := healthPortFromEnvironment(port)
	if configErr != nil {
		return "", 0, configErr
	}

	return address, port, nil
}

// ResolveHealthPort returns the port used by the health and metrics server.
// It remains available for callers that only need the configured port.
func ResolveHealthPort(configPath string) (int, error) {
	_, port, err := ResolveHealthEndpoint(configPath)
	return port, err
}

func healthEndpointFromFile(path string) (string, bool, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, 0, fmt.Errorf("reading config file: %w", err)
	}

	var fileCfg struct {
		Server *FileServerConfig `yaml:"server,omitempty"`
	}
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return "", false, 0, fmt.Errorf("parsing YAML config: %w", err)
	}

	address := DefaultHealthAddress
	allowNetwork := DefaultHealthAllowNetwork
	port := DefaultHealthPort
	if fileCfg.Server == nil {
		return address, allowNetwork, port, nil
	}
	if fileCfg.Server.Address != "" {
		address = strings.TrimSpace(InterpolateEnvVars(fileCfg.Server.Address))
	}
	if fileCfg.Server.AllowNetwork != nil {
		allowNetwork = *fileCfg.Server.AllowNetwork
	}
	if fileCfg.Server != nil && fileCfg.Server.Port > 0 && fileCfg.Server.Port <= 65535 {
		port = fileCfg.Server.Port
	}
	return address, allowNetwork, port, nil
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
