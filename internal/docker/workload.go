package docker

// WorkloadType identifies whether a workload is a Swarm service or a standalone container.
type WorkloadType string

const (
	// WorkloadTypeService represents a Docker Swarm service.
	WorkloadTypeService WorkloadType = "service"
	// WorkloadTypeContainer represents a standalone Docker container.
	WorkloadTypeContainer WorkloadType = "container"
)

// String returns the string representation of the workload type.
func (t WorkloadType) String() string {
	return string(t)
}

// Workload represents either a Docker Swarm service or a standalone container.
// This unified type allows the reconciler to work without knowing the underlying
// Docker mode.
//
// The workload abstraction is central to DNSWeaver's mode-agnostic design:
//   - In Swarm mode, each service becomes a Workload
//   - In standalone mode, each container becomes a Workload
//   - The reconciler only sees Workloads, never services or containers directly
type Workload struct {
	// ID is the unique identifier (service ID or container ID).
	ID string

	// Name is the human-readable name (service name or container name).
	Name string

	// Labels contains all labels from the service or container.
	// These are used by source extractors (Traefik, Caddy, etc.) to find hostnames.
	Labels map[string]string

	// Type indicates whether this is a service or container.
	Type WorkloadType
}

// String returns a human-readable representation of the workload.
func (w Workload) String() string {
	return w.Type.String() + ":" + w.Name
}

// IsService returns true if this workload is a Swarm service.
func (w Workload) IsService() bool {
	return w.Type == WorkloadTypeService
}

// IsContainer returns true if this workload is a standalone container.
func (w Workload) IsContainer() bool {
	return w.Type == WorkloadTypeContainer
}

// HasLabel returns true if the workload has the specified label.
func (w Workload) HasLabel(key string) bool {
	_, ok := w.Labels[key]
	return ok
}

// GetLabel returns the value of the specified label, or empty string if not found.
func (w Workload) GetLabel(key string) string {
	return w.Labels[key]
}

// GetLabelOr returns the value of the specified label, or the default if not found.
func (w Workload) GetLabelOr(key, defaultValue string) string {
	if v, ok := w.Labels[key]; ok {
		return v
	}
	return defaultValue
}

// Workloads is a slice of Workload with helper methods.
type Workloads []Workload

// IDs returns all workload IDs.
func (ws Workloads) IDs() []string {
	ids := make([]string, len(ws))
	for i, w := range ws {
		ids[i] = w.ID
	}
	return ids
}

// Names returns all workload names.
func (ws Workloads) Names() []string {
	names := make([]string, len(ws))
	for i, w := range ws {
		names[i] = w.Name
	}
	return names
}

// Filter returns a new slice containing only workloads where the predicate returns true.
func (ws Workloads) Filter(predicate func(Workload) bool) Workloads {
	result := make(Workloads, 0)
	for _, w := range ws {
		if predicate(w) {
			result = append(result, w)
		}
	}
	return result
}

// WithLabel returns workloads that have the specified label (any value).
func (ws Workloads) WithLabel(key string) Workloads {
	return ws.Filter(func(w Workload) bool {
		return w.HasLabel(key)
	})
}

// WithLabelValue returns workloads that have the specified label with the specified value.
func (ws Workloads) WithLabelValue(key, value string) Workloads {
	return ws.Filter(func(w Workload) bool {
		return w.GetLabel(key) == value
	})
}

// Services returns only workloads that are Swarm services.
func (ws Workloads) Services() Workloads {
	return ws.Filter(func(w Workload) bool {
		return w.IsService()
	})
}

// Containers returns only workloads that are standalone containers.
func (ws Workloads) Containers() Workloads {
	return ws.Filter(func(w Workload) bool {
		return w.IsContainer()
	})
}
