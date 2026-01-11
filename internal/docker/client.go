// Package docker provides a client for interacting with Docker in both standalone and Swarm modes.
//
// The client abstracts away the differences between Docker Swarm services and standalone
// containers, providing a unified Workload type that the reconciler can use without
// knowing which mode Docker is running in.
//
// Mode Detection:
//   - ModeAuto: Auto-detect based on Docker daemon state (default)
//   - ModeSwarm: Force Swarm mode (fails if not in Swarm)
//   - ModeStandalone: Force standalone mode
//
// Example usage:
//
//	client, err := docker.NewClient(ctx, docker.WithHost("unix:///var/run/docker.sock"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	workloads, err := client.ListWorkloads(ctx)
//	for _, w := range workloads {
//	    log.Printf("%s: %s (%s)", w.Type, w.Name, w.ID)
//	}
package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// Mode represents the Docker operation mode.
type Mode string

const (
	// ModeAuto auto-detects whether Docker is in Swarm or standalone mode.
	ModeAuto Mode = "auto"
	// ModeSwarm forces Swarm mode operation.
	ModeSwarm Mode = "swarm"
	// ModeStandalone forces standalone container mode operation.
	ModeStandalone Mode = "standalone"
)

// String returns the string representation of the mode.
func (m Mode) String() string {
	return string(m)
}

var (
	// ErrNotSwarmMode is returned when a Swarm-only operation is attempted in standalone mode.
	ErrNotSwarmMode = errors.New("operation requires Docker Swarm mode")
	// ErrNotStandaloneMode is returned when a standalone-only operation is attempted in Swarm mode.
	ErrNotStandaloneMode = errors.New("operation requires Docker standalone mode")
	// ErrNotManager is returned when the node is in Swarm mode but not a manager.
	ErrNotManager = errors.New("swarm mode detected but this node is not a manager")
	// ErrSwarmNotActive is returned when Swarm mode is forced but Swarm is not active.
	ErrSwarmNotActive = errors.New("swarm mode forced but swarm is not active")
)

// Client wraps the Docker SDK client with DNSWeaver-specific functionality.
//
// The client automatically detects whether Docker is running in Swarm or
// standalone mode and provides appropriate methods for each. Use ListWorkloads()
// for mode-agnostic workload listing.
type Client struct {
	docker        *client.Client
	mode          Mode
	detectedMode  Mode
	logger        *slog.Logger
	host          string
	cleanupOnStop bool // If true, only list running containers; if false, include stopped
}

// NewClient creates a new Docker client with the given options.
//
// By default, the client:
//   - Uses DOCKER_HOST environment variable or the default socket
//   - Auto-detects Swarm vs standalone mode
//   - Uses slog.Default() for logging
//
// Options can be provided to customize behavior:
//
//	client, err := docker.NewClient(ctx,
//	    docker.WithHost("tcp://docker.example.com:2375"),
//	    docker.WithMode(docker.ModeSwarm),
//	    docker.WithLogger(myLogger),
//	)
func NewClient(ctx context.Context, opts ...Option) (*Client, error) {
	c := &Client{
		mode:          ModeAuto,
		logger:        slog.Default(),
		cleanupOnStop: true, // Default: only list running containers
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	// Build Docker client options
	var dockerOpts []client.Opt
	dockerOpts = append(dockerOpts, client.FromEnv)
	dockerOpts = append(dockerOpts, client.WithAPIVersionNegotiation())

	if c.host != "" {
		dockerOpts = append(dockerOpts, client.WithHost(c.host))
	}

	// Create Docker client
	dockerClient, err := client.NewClientWithOpts(dockerOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	c.docker = dockerClient

	// Detect or verify mode
	if err := c.initializeMode(ctx); err != nil {
		dockerClient.Close()
		return nil, err
	}

	c.logger.Info("docker client initialized",
		slog.String("mode", c.detectedMode.String()),
		slog.String("configured_mode", c.mode.String()),
	)

	return c, nil
}

// initializeMode detects or verifies the Docker mode based on configuration.
func (c *Client) initializeMode(ctx context.Context) error {
	info, err := c.docker.Info(ctx)
	if err != nil {
		return fmt.Errorf("getting docker info: %w", err)
	}

	isSwarmActive := info.Swarm.LocalNodeState == swarm.LocalNodeStateActive
	isManager := info.Swarm.ControlAvailable

	c.logger.Debug("docker info retrieved",
		slog.String("swarm_state", string(info.Swarm.LocalNodeState)),
		slog.Bool("control_available", isManager),
		slog.String("node_id", info.Swarm.NodeID),
	)

	switch c.mode {
	case ModeAuto:
		if isSwarmActive {
			if !isManager {
				return ErrNotManager
			}
			c.detectedMode = ModeSwarm
		} else {
			c.detectedMode = ModeStandalone
		}

	case ModeSwarm:
		if !isSwarmActive {
			return ErrSwarmNotActive
		}
		if !isManager {
			return ErrNotManager
		}
		c.detectedMode = ModeSwarm

	case ModeStandalone:
		c.detectedMode = ModeStandalone
	}

	return nil
}

// Mode returns the detected Docker mode.
// This reflects the actual operating mode after initialization.
func (c *Client) Mode() Mode {
	return c.detectedMode
}

// IsSwarm returns true if the client is operating in Swarm mode.
func (c *Client) IsSwarm() bool {
	return c.detectedMode == ModeSwarm
}

// Close closes the underlying Docker client connection.
func (c *Client) Close() error {
	if c.docker != nil {
		return c.docker.Close()
	}
	return nil
}

// Ping verifies connectivity to the Docker daemon.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.docker.Ping(ctx)
	if err != nil {
		return fmt.Errorf("pinging docker: %w", err)
	}
	return nil
}

// RawClient returns the underlying Docker SDK client for advanced operations.
// Use with caution — prefer using the wrapped methods when possible.
func (c *Client) RawClient() *client.Client {
	return c.docker
}

// Service represents a Docker Swarm service with relevant fields for DNS management.
type Service struct {
	ID     string
	Name   string
	Labels map[string]string
}

// Container represents a Docker container with relevant fields for DNS management.
type Container struct {
	ID     string
	Name   string
	Labels map[string]string
}

// ListServices returns all Swarm services with their labels.
// Returns ErrNotSwarmMode if not in Swarm mode.
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	if c.detectedMode != ModeSwarm {
		return nil, ErrNotSwarmMode
	}

	services, err := c.docker.ServiceList(ctx, swarm.ServiceListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}

	result := make([]Service, 0, len(services))
	for _, svc := range services {
		result = append(result, Service{
			ID:     svc.ID,
			Name:   svc.Spec.Name,
			Labels: svc.Spec.Labels,
		})
	}

	c.logger.Debug("listed swarm services",
		slog.Int("count", len(result)),
	)

	return result, nil
}

// ListContainers returns containers with their labels.
// If cleanupOnStop is true (default), only running containers are returned.
// If cleanupOnStop is false, both running and stopped containers are returned,
// allowing DNS records to persist through stop/restart cycles.
// Returns ErrNotStandaloneMode if in Swarm mode.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	if c.detectedMode != ModeStandalone {
		return nil, ErrNotStandaloneMode
	}

	listOpts := container.ListOptions{}
	if c.cleanupOnStop {
		// Only list running containers (stopped containers = orphans)
		listOpts.Filters = filters.NewArgs(
			filters.Arg("status", "running"),
		)
	} else {
		// Include both running and stopped containers
		// Only truly removed containers will be orphans
		listOpts.All = true
		listOpts.Filters = filters.NewArgs(
			filters.Arg("status", "running"),
			filters.Arg("status", "paused"),
			filters.Arg("status", "exited"),
			filters.Arg("status", "created"),
		)
	}

	containers, err := c.docker.ContainerList(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	result := make([]Container, 0, len(containers))
	for _, ctr := range containers {
		name := normalizeContainerName(ctr.Names)

		result = append(result, Container{
			ID:     ctr.ID,
			Name:   name,
			Labels: ctr.Labels,
		})
	}

	c.logger.Debug("listed containers",
		slog.Int("count", len(result)),
		slog.Bool("include_stopped", !c.cleanupOnStop),
	)

	return result, nil
}

// normalizeContainerName extracts a clean container name from Docker's name list.
// Container names from Docker start with "/" which we strip.
func normalizeContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	name := names[0]
	if len(name) > 0 && name[0] == '/' {
		return name[1:]
	}
	return name
}

// GetServiceLabels returns the labels for a specific Swarm service by ID.
func (c *Client) GetServiceLabels(ctx context.Context, serviceID string) (map[string]string, error) {
	if c.detectedMode != ModeSwarm {
		return nil, ErrNotSwarmMode
	}

	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspecting service %s: %w", serviceID, err)
	}

	return svc.Spec.Labels, nil
}

// GetContainerLabels returns the labels for a specific container by ID.
func (c *Client) GetContainerLabels(ctx context.Context, containerID string) (map[string]string, error) {
	if c.detectedMode != ModeStandalone {
		return nil, ErrNotStandaloneMode
	}

	ctr, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspecting container %s: %w", containerID, err)
	}

	return ctr.Config.Labels, nil
}

// ListWorkloads returns all workloads (services in Swarm mode, containers in standalone).
// This provides a unified interface regardless of Docker mode.
func (c *Client) ListWorkloads(ctx context.Context) ([]Workload, error) {
	if c.detectedMode == ModeSwarm {
		services, err := c.ListServices(ctx)
		if err != nil {
			return nil, err
		}

		workloads := make([]Workload, 0, len(services))
		for _, svc := range services {
			workloads = append(workloads, Workload{
				ID:     svc.ID,
				Name:   svc.Name,
				Labels: svc.Labels,
				Type:   WorkloadTypeService,
			})
		}
		return workloads, nil
	}

	containers, err := c.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	workloads := make([]Workload, 0, len(containers))
	for _, ctr := range containers {
		workloads = append(workloads, Workload{
			ID:     ctr.ID,
			Name:   ctr.Name,
			Labels: ctr.Labels,
			Type:   WorkloadTypeContainer,
		})
	}
	return workloads, nil
}

// GetWorkloadLabels returns the labels for a specific workload by ID.
// Automatically uses the correct method based on current mode.
func (c *Client) GetWorkloadLabels(ctx context.Context, workloadID string) (map[string]string, error) {
	if c.detectedMode == ModeSwarm {
		return c.GetServiceLabels(ctx, workloadID)
	}
	return c.GetContainerLabels(ctx, workloadID)
}
