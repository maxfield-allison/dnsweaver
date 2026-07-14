package resources

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// Well-known annotations for hostname extraction from Services.
const (
	// AnnotationExternalDNSHostname is the external-dns compatible annotation.
	AnnotationExternalDNSHostname = "external-dns.alpha.kubernetes.io/hostname"

	// AnnotationDNSWeaverHostname is the dnsweaver native annotation (single hostname).
	AnnotationDNSWeaverHostname = "dnsweaver.dev/hostname"

	// AnnotationDNSWeaverHostnames is the dnsweaver native annotation (comma-separated).
	AnnotationDNSWeaverHostnames = "dnsweaver.dev/hostnames"
)

// metaKeyResourceVersion is the Workload.Metadata key used to record the
// Kubernetes resourceVersion of the source object.
const metaKeyResourceVersion = "resourceVersion"

// ConvertService converts a core/v1 Service to a workload.Workload.
//
// Hostnames are extracted from well-known annotations:
//   - external-dns.alpha.kubernetes.io/hostname (comma-separated)
//   - dnsweaver.dev/hostname (single)
//   - dnsweaver.dev/hostnames (comma-separated)
//
// Services without any of these annotations still produce a Workload, allowing
// sources to inspect other annotations for hostname intent.
func ConvertService(svc *corev1.Service) workload.Workload {
	var hostnames []string
	seen := make(map[string]struct{})

	// Extract from external-dns annotation (widely adopted convention).
	if h := svc.Annotations[AnnotationExternalDNSHostname]; h != "" {
		for _, name := range splitCSV(h) {
			if _, exists := seen[name]; !exists {
				hostnames = append(hostnames, name)
				seen[name] = struct{}{}
			}
		}
	}

	// Extract from dnsweaver native annotations.
	if h := svc.Annotations[AnnotationDNSWeaverHostname]; h != "" {
		if _, exists := seen[h]; !exists {
			hostnames = append(hostnames, h)
			seen[h] = struct{}{}
		}
	}
	if h := svc.Annotations[AnnotationDNSWeaverHostnames]; h != "" {
		for _, name := range splitCSV(h) {
			if _, exists := seen[name]; !exists {
				hostnames = append(hostnames, name)
				seen[name] = struct{}{}
			}
		}
	}

	meta := map[string]string{
		metaKeyResourceVersion: svc.ResourceVersion,
		"type":                 string(svc.Spec.Type),
	}
	if svc.Spec.ClusterIP != "" {
		meta["clusterIP"] = svc.Spec.ClusterIP
	}

	return workload.Workload{
		ID:          string(svc.UID),
		Name:        svc.Namespace + "/" + svc.Name,
		Namespace:   svc.Namespace,
		Labels:      copyMap(svc.Labels),
		Annotations: copyMap(svc.Annotations),
		Platform:    workload.PlatformKubernetes,
		Kind:        workload.KindK8sService,
		Hostnames:   hostnames,
		Metadata:    meta,
	}
}
