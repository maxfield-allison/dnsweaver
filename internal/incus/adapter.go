package incus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

const (
	instanceTypeContainer = "container"
	instanceTypeVM        = "virtual-machine"
)

// Metadata/log field keys shared between logging and the toWorkload metadata map.
const (
	metaKeyProject = "project"
	metaKeyType    = "type"
	metaKeyStatus  = "status"

	// defaultProject is used when an instance has no explicit project set.
	defaultProject = "default"
)

// composeLabelPrefix is the config-key prefix that incus-compose
// (https://github.com/lxc/incus-compose) uses to store Compose `labels:`
// entries on an instance. For example, a Compose label
// "traefik.http.routers.api.rule" is stored as the instance config key
// "user.label.traefik.http.routers.api.rule". Stripping this prefix lets the
// existing label-based sources (traefik, caddy, nginx-proxy, dnsweaver) match
// incus-compose instances unchanged.
const composeLabelPrefix = "user.label."

// AdapterConfig holds filtering options for the WorkloadListerAdapter.
type AdapterConfig struct {
	// StateFilter restricts listing to instances in the given state. Typical
	// values: "running", "stopped". Matching is case-insensitive. Defaults to
	// "running" if empty.
	StateFilter string
}

// WorkloadListerAdapter wraps an Incus Client to implement the workload.Lister
// interface. It lists instances (containers and VMs), applies configured
// filters, resolves IP addresses from the instance state, and converts each
// instance to a platform-agnostic workload.Workload.
type WorkloadListerAdapter struct {
	client *Client
	cfg    AdapterConfig
	logger *slog.Logger
}

// NewWorkloadListerAdapter creates a new adapter that wraps an Incus Client.
func NewWorkloadListerAdapter(c *Client, cfg AdapterConfig, logger *slog.Logger) *WorkloadListerAdapter {
	stateFilter := cfg.StateFilter
	if stateFilter == "" {
		stateFilter = "running"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkloadListerAdapter{
		client: c,
		cfg:    AdapterConfig{StateFilter: stateFilter},
		logger: logger,
	}
}

// ListWorkloads returns Incus instances as platform-agnostic workloads. Applies
// the state filter and resolves each instance's IP address from its runtime
// network state.
func (a *WorkloadListerAdapter) ListWorkloads(ctx context.Context) ([]workload.Workload, error) {
	instances, err := a.client.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing incus instances: %w", err)
	}

	var result []workload.Workload

	for _, inst := range instances {
		if !a.matchesFilters(inst) {
			continue
		}

		ip := ResolveIP(inst)
		if ip == "" {
			a.logger.Debug("no routable IPv4 resolved for incus instance",
				slog.String("name", inst.Name),
				slog.String(metaKeyProject, inst.Project),
				slog.String(metaKeyType, inst.Type),
			)
		}

		result = append(result, toWorkload(inst, ip))
	}

	return result, nil
}

// Platform returns PlatformIncus, identifying this adapter as an Incus source.
func (a *WorkloadListerAdapter) Platform() workload.Platform {
	return workload.PlatformIncus
}

// matchesFilters returns true if the given instance passes all configured filters.
func (a *WorkloadListerAdapter) matchesFilters(inst Instance) bool {
	if a.cfg.StateFilter != "" && !strings.EqualFold(inst.Status, a.cfg.StateFilter) {
		return false
	}
	return true
}

// toWorkload converts an Instance and its resolved IP into a platform-agnostic
// Workload. Instance config keys (including user.* keys) are exposed as labels
// so sources can act on them.
func toWorkload(inst Instance, ip string) workload.Workload {
	kind := workload.KindIncusVM
	if inst.Type == instanceTypeContainer {
		kind = workload.KindIncusContainer
	}

	project := inst.Project
	if project == "" {
		project = defaultProject
	}

	meta := map[string]string{
		metaKeyProject: project,
		metaKeyType:    inst.Type,
		metaKeyStatus:  inst.Status,
	}
	if ip != "" {
		meta["ip"] = ip
	}

	// Expose instance config keys directly as labels so sources can read
	// operator-supplied keys such as "user.dnsweaver.hostname".
	labels := make(map[string]string, len(inst.Config))
	for k, v := range inst.Config {
		labels[k] = v
	}

	// Additionally surface incus-compose labels under their stripped form so the
	// existing label-based sources match them. A key like
	// "user.label.dnsweaver.hostname" is also exposed as "dnsweaver.hostname".
	// The raw "user.label.*" key is retained for transparency, and the stripped
	// alias never overwrites a label that is already present.
	for k, v := range inst.Config {
		stripped, ok := strings.CutPrefix(k, composeLabelPrefix)
		if !ok || stripped == "" {
			continue
		}
		if _, exists := labels[stripped]; !exists {
			labels[stripped] = v
		}
	}

	return workload.Workload{
		ID:       fmt.Sprintf("%s/%s", project, inst.Name),
		Name:     inst.Name,
		Labels:   labels,
		Platform: workload.PlatformIncus,
		Kind:     kind,
		Metadata: meta,
	}
}
