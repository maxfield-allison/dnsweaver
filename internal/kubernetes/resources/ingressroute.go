package resources

import (
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// hostMatcherRegex matches Host(`hostname`) patterns in Traefik match expressions.
// This is the same syntax used in Traefik router rules and IngressRoute .spec.routes[].match.
var hostMatcherRegex = regexp.MustCompile(`Host\(` + "`" + `([^` + "`" + `]+)` + "`" + `\)`)

// ConvertIngressRoute converts an unstructured traefik.io/v1alpha1 IngressRoute
// to a workload.Workload.
//
// Hostnames are extracted from .spec.routes[].match by parsing Host(`...`)
// matchers. Multiple hosts per route and multiple routes are supported.
//
// Example IngressRoute match expressions:
//
//	Host(`example.com`)
//	Host(`a.example.com`) && PathPrefix(`/api`)
//	Host(`a.example.com`) || Host(`b.example.com`)
func ConvertIngressRoute(obj *unstructured.Unstructured) workload.Workload {
	var hostnames []string
	seen := make(map[string]struct{})

	// Navigate: .spec.routes[].match
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	if spec != nil {
		routes, ok := spec["routes"].([]interface{})
		if ok {
			for _, r := range routes {
				route, ok := r.(map[string]interface{})
				if !ok {
					continue
				}
				match, ok := route["match"].(string)
				if !ok || match == "" {
					continue
				}
				for _, host := range extractHostsFromMatch(match) {
					if _, exists := seen[host]; !exists {
						hostnames = append(hostnames, host)
						seen[host] = struct{}{}
					}
				}
			}
		}
	}

	labels := extractStringMap(obj.GetLabels())
	annotations := extractStringMap(obj.GetAnnotations())

	return workload.Workload{
		ID:          string(obj.GetUID()),
		Name:        obj.GetNamespace() + "/" + obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Labels:      labels,
		Annotations: annotations,
		Platform:    workload.PlatformKubernetes,
		Kind:        workload.KindIngressRoute,
		Hostnames:   hostnames,
		Metadata: map[string]string{
			metaKeyResourceVersion: obj.GetResourceVersion(),
		},
	}
}

// extractHostsFromMatch extracts hostnames from a Traefik match expression.
// Parses all Host(`...`) patterns and returns deduplicated hostnames.
func extractHostsFromMatch(match string) []string {
	var hosts []string
	matches := hostMatcherRegex.FindAllStringSubmatch(match, -1)
	for _, m := range matches {
		if len(m) >= 2 {
			host := strings.TrimSpace(m[1])
			if host != "" {
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
}

// extractStringMap returns a copy of the map, or nil if empty.
func extractStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
