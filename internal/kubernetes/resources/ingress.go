package resources

import (
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// ConvertIngress converts a networking.k8s.io/v1 Ingress to a workload.Workload.
//
// Hostnames are extracted from .spec.rules[].host. Empty host values (wildcard
// catch-all rules) are skipped since they don't represent specific DNS names.
func ConvertIngress(ing *networkingv1.Ingress) workload.Workload {
	var hostnames []string
	for _, rule := range ing.Spec.Rules {
		if rule.Host != "" {
			hostnames = append(hostnames, rule.Host)
		}
	}

	// Also extract from TLS section as a fallback (hosts declared in TLS
	// but not in rules may still need DNS records).
	seen := make(map[string]struct{}, len(hostnames))
	for _, h := range hostnames {
		seen[h] = struct{}{}
	}
	for _, tls := range ing.Spec.TLS {
		for _, h := range tls.Hosts {
			if _, exists := seen[h]; !exists {
				hostnames = append(hostnames, h)
				seen[h] = struct{}{}
			}
		}
	}

	return workload.Workload{
		ID:          string(ing.UID),
		Name:        ing.Namespace + "/" + ing.Name,
		Namespace:   ing.Namespace,
		Labels:      copyMap(ing.Labels),
		Annotations: copyMap(ing.Annotations),
		Platform:    workload.PlatformKubernetes,
		Kind:        workload.KindIngress,
		Hostnames:   hostnames,
		Metadata: map[string]string{
			metaKeyResourceVersion: ing.ResourceVersion,
		},
	}
}

// copyMap returns a shallow copy of a string map.
// Returns nil if the input is nil.
func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
