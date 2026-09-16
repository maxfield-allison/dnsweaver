package source

import (
	"fmt"
	"slices"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// workloadInstances accepts the same selector on Docker labels and Kubernetes
// annotations. If both exist they must describe the same set; neither silently
// overrides a contradictory selection on the other surface.
func workloadInstances(w workload.Workload) ([]string, error) {
	var selected []string
	for _, entry := range []struct {
		key    string
		values map[string]string
	}{
		{"dnsweaver.instances", w.Labels},
		{"dnsweaver.dev/instances", w.Annotations},
	} {
		value, present := entry.values[entry.key]
		if !present {
			continue
		}
		var names []string
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				return nil, fmt.Errorf("%s contains an empty provider name", entry.key)
			}
			names = append(names, name)
		}
		slices.Sort(names)
		names = slices.Compact(names)
		if selected != nil && !slices.Equal(selected, names) {
			return nil, fmt.Errorf("workload provider label and annotation disagree")
		}
		selected = names
	}
	return selected, nil
}

func applyWorkloadInstances(hostnames Hostnames, instances []string) {
	for i := range hostnames {
		hostnames[i].Instances = slices.Clone(instances)
	}
}
