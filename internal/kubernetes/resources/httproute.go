package resources

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// ConvertHTTPRoute converts an unstructured gateway.networking.k8s.io/v1 HTTPRoute
// to a workload.Workload.
//
// Hostnames are extracted from .spec.hostnames[], which is a simple string array
// in the Gateway API spec.
func ConvertHTTPRoute(obj *unstructured.Unstructured) workload.Workload {
	var hostnames []string
	seen := make(map[string]struct{})

	// Navigate: .spec.hostnames[]
	hostnameSlice, found, _ := unstructured.NestedStringSlice(obj.Object, "spec", "hostnames")
	if found {
		for _, h := range hostnameSlice {
			if h != "" {
				if _, exists := seen[h]; !exists {
					hostnames = append(hostnames, h)
					seen[h] = struct{}{}
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
		Kind:        workload.KindHTTPRoute,
		Hostnames:   hostnames,
		Metadata: map[string]string{
			metaKeyResourceVersion: obj.GetResourceVersion(),
		},
	}
}
