package proxmox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

const (
	resourceTypeLXC  = "lxc"
	resourceTypeQEMU = "qemu"
	tagLabelValue    = "true"

	stateRunning  = "running"
	ipVersionIPv4 = "ipv4"
)

// Metadata/log field keys shared between logging and the toWorkload metadata map.
const (
	metaKeyNode   = "node"
	metaKeyVMID   = "vmid"
	metaKeyTags   = "tags"
	metaKeyStatus = "status"
)

// AdapterConfig holds filtering options for the WorkloadListerAdapter.
type AdapterConfig struct {
	// NodeFilter restricts listing to a specific Proxmox node name.
	// Empty string means all nodes.
	NodeFilter string

	// TagFilter restricts listing to resources that have at least one tag
	// with the given prefix (e.g., "dnsweaver"). Matching is case-sensitive.
	// Empty string means all resources regardless of tags.
	TagFilter string

	// StateFilter restricts listing to resources in the given state.
	// Typical values: "running", "stopped". Defaults to "running" if empty.
	StateFilter string

	// InterfacePreference selects a specific guest interface name to use for IP resolution.
	// Empty string means no preference and the adapter will use the allow-list below.
	InterfacePreference string

	// AllowedInterfaces is a list of interface names that are allowed for IP resolution.
	// If provided, the resolver will only consider those interfaces in order.
	AllowedInterfaces []string
}

// WorkloadListerAdapter wraps a Proxmox Client to implement the workload.Lister interface.
// It fetches VMs and LXC containers from the Proxmox cluster, applies configured
// filters, resolves IP addresses, and converts each resource to a platform-agnostic
// workload.Workload.
type WorkloadListerAdapter struct {
	client *Client
	cfg    AdapterConfig
	logger *slog.Logger
}

// NewWorkloadListerAdapter creates a new adapter that wraps a Proxmox Client.
func NewWorkloadListerAdapter(c *Client, cfg AdapterConfig, logger *slog.Logger) *WorkloadListerAdapter {
	stateFilter := cfg.StateFilter
	if stateFilter == "" {
		stateFilter = stateRunning
	}
	return &WorkloadListerAdapter{
		client: c,
		cfg: AdapterConfig{
			NodeFilter:          cfg.NodeFilter,
			TagFilter:           cfg.TagFilter,
			StateFilter:         stateFilter,
			InterfacePreference: cfg.InterfacePreference,
			AllowedInterfaces:   cfg.AllowedInterfaces,
		},
		logger: logger,
	}
}

// ListWorkloads returns Proxmox VMs and LXC containers as platform-agnostic workloads.
// Applies node, tag, and state filters. Resolves IP addresses via LXC config parsing
// or the qemu-guest-agent.
func (a *WorkloadListerAdapter) ListWorkloads(ctx context.Context) ([]workload.Workload, error) {
	resources, err := a.client.ListClusterResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing proxmox cluster resources: %w", err)
	}

	var result []workload.Workload

	for _, r := range resources {
		if !a.matchesFilters(r) {
			continue
		}

		interfacePreference := parseInterfacePreferenceFromTags(r.Tags, a.cfg.InterfacePreference)
		ip, err := ResolveIPWithInterfacePreferences(ctx, a.client, r, a.logger, interfacePreference, a.cfg.AllowedInterfaces)
		if err != nil {
			a.logger.Warn("could not resolve IP for proxmox resource",
				slog.String(metaKeyNode, r.Node),
				slog.Int(metaKeyVMID, r.VMID),
				slog.String("name", r.Name),
				slog.String("error", err.Error()),
			)
			// Continue processing remaining resources; a single IP resolution
			// failure should not abort the entire listing.
			continue
		}

		w := toWorkload(r, ip)
		result = append(result, w)
	}

	return result, nil
}

// Platform returns PlatformProxmox, identifying this adapter as a Proxmox source.
func (a *WorkloadListerAdapter) Platform() workload.Platform {
	return workload.PlatformProxmox
}

// matchesFilters returns true if the given resource passes all configured filters.
func (a *WorkloadListerAdapter) matchesFilters(r ClusterResource) bool {
	if a.cfg.StateFilter != "" && r.Status != a.cfg.StateFilter {
		return false
	}
	if a.cfg.NodeFilter != "" && r.Node != a.cfg.NodeFilter {
		return false
	}
	if a.cfg.TagFilter != "" && !hasTagWithPrefix(r.Tags, a.cfg.TagFilter) {
		return false
	}
	return true
}

// toWorkload converts a ClusterResource and its resolved IP into a platform-agnostic Workload.
func toWorkload(r ClusterResource, ip string) workload.Workload {
	kind := workload.KindVM
	if r.Type == resourceTypeLXC {
		kind = workload.KindLXC
	}

	meta := map[string]string{
		metaKeyNode:   r.Node,
		metaKeyVMID:   fmt.Sprintf("%d", r.VMID),
		metaKeyTags:   r.Tags,
		metaKeyStatus: r.Status,
	}
	if ip != "" {
		meta["ip"] = ip
	}

	// Parse PVE tags into workload labels so sources can act on them.
	// Proxmox tags are semicolon-delimited. We expose them as
	// "proxmox.tag/<tagvalue>" = "true" labels.
	labels := parseTags(r.Tags)

	return workload.Workload{
		ID:       fmt.Sprintf("%s/%d", r.Node, r.VMID),
		Name:     r.Name,
		Labels:   labels,
		Platform: workload.PlatformProxmox,
		Kind:     kind,
		Metadata: meta,
	}
}

// hasTagWithPrefix returns true if the semicolon-delimited tags string contains
// at least one tag that starts with the given prefix.
func hasTagWithPrefix(tags, prefix string) bool {
	if tags == "" {
		return false
	}
	for _, tag := range strings.Split(tags, ";") {
		tag = strings.TrimSpace(tag)
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

// parseTags converts a semicolon-delimited PVE tags string into a labels map.
// Each tag becomes a "proxmox.tag/<value>" = "true" entry.
func parseTags(tags string) map[string]string {
	labels := make(map[string]string)
	if tags == "" {
		return labels
	}
	for _, tag := range strings.Split(tags, ";") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		labels["proxmox.tag/"+tag] = tagLabelValue
	}
	return labels
}
