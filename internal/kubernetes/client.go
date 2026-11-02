package kubernetes

import (
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients holds the Kubernetes API clients needed by the watcher.
type Clients struct {
	// Typed is the standard Kubernetes clientset for core API resources.
	Typed kubernetes.Interface

	// Dynamic is the dynamic client for unstructured/CRD resources.
	Dynamic dynamic.Interface

	// Discovery is the discovery client for API group detection.
	Discovery discovery.DiscoveryInterface

	// restConfig is the underlying REST configuration (kept for potential reuse).
	restConfig *rest.Config
}

// NewClients creates Kubernetes API clients from the given configuration.
//
// If kubeconfig is empty, it attempts in-cluster configuration using the
// pod's ServiceAccount token. Otherwise, it loads the kubeconfig file
// from the specified path.
func NewClients(kubeconfig string) (*Clients, error) {
	cfg, err := buildRESTConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("building kubernetes REST config: %w", err)
	}

	typedClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic kubernetes client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}

	return &Clients{
		Typed:      typedClient,
		Dynamic:    dynamicClient,
		Discovery:  discoveryClient,
		restConfig: cfg,
	}, nil
}

// buildRESTConfig creates a Kubernetes REST config from kubeconfig path
// or falls back to in-cluster config.
func buildRESTConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w (set DNSWEAVER_K8S_KUBECONFIG for out-of-cluster)", err)
		}
		return cfg, nil
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig %q: %w", kubeconfig, err)
	}
	return cfg, nil
}

// HasAPIGroup checks whether a specific API group is available in the cluster.
// This is used for CRD auto-detection (e.g., checking if traefik.io is installed).
func HasAPIGroup(disc discovery.DiscoveryInterface, group string) (bool, error) {
	groups, err := disc.ServerGroups()
	if err != nil {
		return false, fmt.Errorf("discovering kubernetes API groups: %w", err)
	}
	for _, g := range groups.Groups {
		if g.Name == group {
			return true, nil
		}
	}
	return false, nil
}
