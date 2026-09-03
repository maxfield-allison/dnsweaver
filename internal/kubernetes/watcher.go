package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	networkinginformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/tools/cache"

	corev1 "k8s.io/api/core/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"

	"github.com/maxfield-allison/dnsweaver/internal/kubernetes/resources"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// ReconcileFunc is called when changes are detected that require reconciliation.
type ReconcileFunc func()

// CRD GroupVersionResource definitions for dynamic informers.
var (
	// IngressRouteGVR is the GVR for Traefik IngressRoute CRD.
	IngressRouteGVR = schema.GroupVersionResource{
		Group:    "traefik.io",
		Version:  "v1alpha1",
		Resource: "ingressroutes",
	}

	// HTTPRouteGVR is the GVR for Gateway API HTTPRoute CRD.
	HTTPRouteGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
)

// Option is a functional option for configuring the Watcher.
type Option func(*Watcher)

// WithConfig sets the watcher configuration.
func WithConfig(cfg Config) Option {
	return func(w *Watcher) {
		w.config = cfg
	}
}

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(w *Watcher) {
		if logger != nil {
			w.logger = logger
		}
	}
}

// Watcher monitors Kubernetes resources using client-go informers and
// triggers reconciliation when changes are detected.
//
// It implements workload.Lister, allowing the reconciler to list all
// Kubernetes workloads from the informer caches without additional API calls.
type Watcher struct {
	clients     *Clients
	config      Config
	logger      *slog.Logger
	onReconcile ReconcileFunc

	// One informer factory per configured namespace. A single all-namespace
	// factory is used when no namespace filter is configured.
	typedFactories []informers.SharedInformerFactory

	// Dynamic informer factories for CRDs (IngressRoute, HTTPRoute), aligned
	// by index with typedFactories when CRD watching is enabled.
	dynamicFactories []dynamicinformer.DynamicSharedInformerFactory

	// Typed informers (empty if not watching that resource type).
	ingressInformers []networkinginformers.IngressInformer
	serviceInformers []coreinformers.ServiceInformer

	// CRD availability (detected at start time).
	hasIngressRoute bool
	hasHTTPRoute    bool

	// State management.
	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
	debounce *time.Timer
}

// New creates a new Kubernetes resource watcher.
//
// The watcher monitors resources via informers and triggers onReconcile
// when changes are detected (with debouncing to absorb rapid events).
// Call SetReconcileFunc to set the reconcile callback before calling Start.
func New(clients *Clients, opts ...Option) *Watcher {
	w := &Watcher{
		clients: clients,
		config:  DefaultConfig(),
		logger:  slog.Default(),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// SetReconcileFunc sets the function called when reconciliation should occur.
// Must be called before Start.
func (w *Watcher) SetReconcileFunc(fn ReconcileFunc) {
	w.onReconcile = fn
}

// Start begins watching Kubernetes resources.
// This method is non-blocking — it starts informers and returns immediately.
// Call Stop() to halt watching and clean up resources.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	w.mu.Unlock()

	if !w.config.WatchesAnything() {
		w.logger.Warn("kubernetes watcher started but no resource types enabled")
		return nil
	}

	// Auto-detect CRDs.
	w.detectCRDs()

	// Create informer factories.
	w.createFactories()

	// Set up informers for enabled resource types.
	if err := w.setupInformers(); err != nil {
		return fmt.Errorf("setting up kubernetes informers: %w", err)
	}

	// Start factories.
	for _, factory := range w.typedFactories {
		factory.Start(ctx.Done())
	}
	for _, factory := range w.dynamicFactories {
		factory.Start(ctx.Done())
	}

	// Wait for initial cache sync.
	w.logger.Info("waiting for kubernetes informer cache sync")
	for _, factory := range w.typedFactories {
		factory.WaitForCacheSync(ctx.Done())
	}
	for _, factory := range w.dynamicFactories {
		factory.WaitForCacheSync(ctx.Done())
	}

	w.logger.Info("kubernetes watcher started",
		slog.Bool("ingress", w.config.WatchIngress),
		slog.Bool("ingressroute", w.hasIngressRoute),
		slog.Bool("httproute", w.hasHTTPRoute),
		slog.Bool("services", w.config.WatchServices),
		slog.Duration("debounce", w.config.DebounceInterval),
	)

	return nil
}

// Stop halts the Kubernetes watcher and cleans up resources.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}

	if w.debounce != nil {
		w.debounce.Stop()
		w.debounce = nil
	}

	w.running = false
	w.logger.Info("kubernetes watcher stopped")
}

// IsRunning returns whether the watcher is currently running.
func (w *Watcher) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// ListWorkloads returns all Kubernetes workloads from the informer caches,
// converted to platform-agnostic workload.Workload values.
//
// This implements the workload.Lister interface. It reads from the informer
// cache (no API calls) and applies namespace/annotation filtering.
func (w *Watcher) ListWorkloads(ctx context.Context) ([]workload.Workload, error) {
	var result []workload.Workload

	selector := labels.Everything()
	if w.config.LabelSelector != "" {
		var err error
		selector, err = labels.Parse(w.config.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("parsing label selector %q: %w", w.config.LabelSelector, err)
		}
	}

	// List Ingress resources.
	for _, informer := range w.ingressInformers {
		ingresses, err := informer.Lister().List(selector)
		if err != nil {
			return nil, fmt.Errorf("listing ingresses: %w", err)
		}
		for _, ing := range ingresses {
			if w.matchesFilters(ing.Namespace, ing.Annotations) {
				result = append(result, resources.ConvertIngress(ing))
			}
		}
	}

	// List Service resources.
	for _, informer := range w.serviceInformers {
		services, err := informer.Lister().List(selector)
		if err != nil {
			return nil, fmt.Errorf("listing services: %w", err)
		}
		for _, svc := range services {
			if w.matchesFilters(svc.Namespace, svc.Annotations) {
				result = append(result, resources.ConvertService(svc))
			}
		}
	}

	// List IngressRoute resources (dynamic/unstructured).
	for _, factory := range w.dynamicFactories {
		if !w.hasIngressRoute {
			break
		}
		lister := factory.ForResource(IngressRouteGVR).Lister()
		items, err := lister.List(selector)
		if err != nil {
			return nil, fmt.Errorf("listing ingressroutes: %w", err)
		}
		for _, item := range items {
			obj, ok := item.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			if w.matchesFilters(obj.GetNamespace(), obj.GetAnnotations()) {
				result = append(result, resources.ConvertIngressRoute(obj))
			}
		}
	}

	// List HTTPRoute resources (dynamic/unstructured).
	for _, factory := range w.dynamicFactories {
		if !w.hasHTTPRoute {
			break
		}
		lister := factory.ForResource(HTTPRouteGVR).Lister()
		items, err := lister.List(selector)
		if err != nil {
			return nil, fmt.Errorf("listing httproutes: %w", err)
		}
		for _, item := range items {
			obj, ok := item.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			if w.matchesFilters(obj.GetNamespace(), obj.GetAnnotations()) {
				result = append(result, resources.ConvertHTTPRoute(obj))
			}
		}
	}

	return result, nil
}

// Platform returns kubernetes, since this watcher produces Kubernetes workloads.
func (w *Watcher) Platform() workload.Platform {
	return workload.PlatformKubernetes
}

// TriggerNow immediately triggers reconciliation, bypassing debounce.
func (w *Watcher) TriggerNow() {
	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
		w.debounce = nil
	}
	w.mu.Unlock()

	w.triggerReconcile()
}

// detectCRDs checks which CRDs are available in the cluster.
func (w *Watcher) detectCRDs() {
	if w.config.WatchIngressRoute {
		w.hasIngressRoute = HasAPIGroup(w.clients.Discovery, IngressRouteGVR.Group)
		if w.hasIngressRoute {
			w.logger.Info("CRD detected: Traefik IngressRoute")
		} else {
			w.logger.Info("CRD not found: Traefik IngressRoute (traefik.io group not available)")
		}
	}

	if w.config.WatchHTTPRoute {
		w.hasHTTPRoute = HasAPIGroup(w.clients.Discovery, HTTPRouteGVR.Group)
		if w.hasHTTPRoute {
			w.logger.Info("CRD detected: Gateway API HTTPRoute")
		} else {
			w.logger.Info("CRD not found: Gateway API HTTPRoute (gateway.networking.k8s.io group not available)")
		}
	}
}

// createFactories initializes the informer factories.
func (w *Watcher) createFactories() {
	// Start may be called again after Stop. Informers themselves cannot be
	// restarted, so build a fresh set of factories instead of appending to the
	// stopped set from the previous run.
	w.typedFactories = nil
	w.dynamicFactories = nil
	w.ingressInformers = nil
	w.serviceInformers = nil

	for _, namespace := range w.factoryNamespaces() {
		w.typedFactories = append(w.typedFactories,
			informers.NewSharedInformerFactoryWithOptions(
				w.clients.Typed, DefaultResyncInterval,
				informers.WithNamespace(namespace),
				informers.WithTransform(stripManagedFields),
			),
		)

		// Only create dynamic factories if CRDs are available.
		if w.hasIngressRoute || w.hasHTTPRoute {
			w.dynamicFactories = append(w.dynamicFactories,
				dynamicinformer.NewFilteredDynamicSharedInformerFactory(
					w.clients.Dynamic, DefaultResyncInterval, namespace, nil,
				),
			)
		}
	}
}

// factoryNamespaces returns the API namespaces each informer factory should
// watch. Scoping the list/watch itself is required for namespace-only RBAC;
// filtering a cluster-wide cache after the fact is both broader and noisier.
func (w *Watcher) factoryNamespaces() []string {
	if !w.config.HasNamespaceFilter() {
		return []string{metav1.NamespaceAll}
	}

	seen := make(map[string]struct{}, len(w.config.Namespaces))
	namespaces := make([]string, 0, len(w.config.Namespaces))
	for _, configured := range w.config.Namespaces {
		namespace := strings.TrimSpace(configured)
		if namespace == "" {
			continue
		}
		if _, exists := seen[namespace]; exists {
			continue
		}
		seen[namespace] = struct{}{}
		namespaces = append(namespaces, namespace)
	}
	if len(namespaces) == 0 {
		return []string{metav1.NamespaceAll}
	}
	return namespaces
}

// setupInformers creates and registers event handlers for each resource type.
// Returns an error if any event handler fails to register, since missing handlers
// means events for that resource type will be silently dropped.
func (w *Watcher) setupInformers() error {
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { w.handleEvent("add") },
		UpdateFunc: func(_, _ interface{}) { w.handleEvent("update") },
		DeleteFunc: func(_ interface{}) { w.handleEvent("delete") },
	}

	for _, factory := range w.typedFactories {
		if w.config.WatchIngress {
			informer := factory.Networking().V1().Ingresses()
			if _, err := informer.Informer().AddEventHandler(handler); err != nil {
				return fmt.Errorf("adding ingress event handler: %w", err)
			}
			w.ingressInformers = append(w.ingressInformers, informer)
		}

		if w.config.WatchServices {
			informer := factory.Core().V1().Services()
			if _, err := informer.Informer().AddEventHandler(handler); err != nil {
				return fmt.Errorf("adding service event handler: %w", err)
			}
			w.serviceInformers = append(w.serviceInformers, informer)
		}
	}

	for _, factory := range w.dynamicFactories {
		if w.hasIngressRoute {
			informer := factory.ForResource(IngressRouteGVR).Informer()
			if _, err := informer.AddEventHandler(handler); err != nil {
				return fmt.Errorf("adding ingressroute event handler: %w", err)
			}
		}

		if w.hasHTTPRoute {
			informer := factory.ForResource(HTTPRouteGVR).Informer()
			if _, err := informer.AddEventHandler(handler); err != nil {
				return fmt.Errorf("adding httproute event handler: %w", err)
			}
		}
	}

	return nil
}

// handleEvent processes an informer event with debouncing.
func (w *Watcher) handleEvent(action string) {
	w.logger.Debug("received kubernetes event", slog.String("action", action))

	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(w.config.DebounceInterval, func() {
		w.triggerReconcile()
	})
	w.mu.Unlock()
}

// triggerReconcile invokes the reconciliation callback.
func (w *Watcher) triggerReconcile() {
	w.logger.Info("triggering reconciliation due to kubernetes event")
	if w.onReconcile != nil {
		w.onReconcile()
	}
}

// matchesFilters checks if a resource passes namespace and annotation filters.
func (w *Watcher) matchesFilters(namespace string, annotations map[string]string) bool {
	// Namespace filter.
	if w.config.HasNamespaceFilter() {
		found := false
		for _, ns := range w.config.Namespaces {
			if strings.TrimSpace(ns) == namespace {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Annotation filter.
	if w.config.AnnotationFilter != "" {
		return matchAnnotationFilter(w.config.AnnotationFilter, annotations)
	}

	return true
}

// matchAnnotationFilter checks if annotations contain the required key=value pair.
func matchAnnotationFilter(filter string, annotations map[string]string) bool {
	if annotations == nil {
		return false
	}

	// Parse "key=value" format.
	parts := splitAnnotationFilter(filter)
	if parts == nil {
		return false
	}

	key, value := parts[0], parts[1]
	if actual, ok := annotations[key]; ok {
		return actual == value
	}
	return false
}

// splitAnnotationFilter splits a "key=value" string into [key, value].
// Returns nil if the format is invalid.
func splitAnnotationFilter(filter string) []string {
	idx := indexByte(filter, '=')
	if idx < 1 {
		return nil
	}
	return []string{filter[:idx], filter[idx+1:]}
}

// indexByte returns the index of the first instance of c in s, or -1.
func indexByte(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// stripManagedFields is a transform function that removes managedFields
// from objects to reduce memory usage in the informer cache.
func stripManagedFields(obj interface{}) (interface{}, error) {
	// Handle typed objects.
	if ing, ok := obj.(*networkingv1.Ingress); ok {
		ing.ManagedFields = nil
		return ing, nil
	}
	if svc, ok := obj.(*corev1.Service); ok {
		svc.ManagedFields = nil
		return svc, nil
	}
	return obj, nil
}

// Compile-time interface check.
var _ workload.Lister = (*Watcher)(nil)
