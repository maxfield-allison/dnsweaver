// Package source provides hostname discovery from various sources.
package source

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DiscoveryCallback is called when file discovery finds hostnames.
type DiscoveryCallback func(sourceName string, hostnames []Hostname)

// FileWatcher polls discoverable sources for hostname changes.
type FileWatcher struct {
	registry     *Registry
	callback     DiscoveryCallback
	pollInterval time.Duration
	logger       *slog.Logger

	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
	lastSeen map[string]map[string]struct{} // source -> hostname set
}

// FileWatcherOption configures a FileWatcher.
type FileWatcherOption func(*FileWatcher)

// WithPollInterval sets the polling interval.
func WithPollInterval(d time.Duration) FileWatcherOption {
	return func(w *FileWatcher) {
		w.pollInterval = d
	}
}

// WithWatcherLogger sets the logger for the watcher.
func WithWatcherLogger(logger *slog.Logger) FileWatcherOption {
	return func(w *FileWatcher) {
		w.logger = logger
	}
}

// NewFileWatcher creates a new file watcher for discoverable sources.
func NewFileWatcher(registry *Registry, callback DiscoveryCallback, opts ...FileWatcherOption) *FileWatcher {
	w := &FileWatcher{
		registry:     registry,
		callback:     callback,
		pollInterval: 60 * time.Second,
		logger:       slog.Default(),
		lastSeen:     make(map[string]map[string]struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// Start begins polling discoverable sources.
func (w *FileWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	w.mu.Unlock()

	// Initial discovery
	w.pollAll(ctx)

	// Start polling loop
	go w.pollLoop(ctx)

	return nil
}

// Stop halts the file watcher.
func (w *FileWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.running = false
}

// IsRunning returns whether the watcher is currently running.
func (w *FileWatcher) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// PollNow triggers an immediate poll of all discoverable sources.
func (w *FileWatcher) PollNow(ctx context.Context) {
	w.pollAll(ctx)
}

func (w *FileWatcher) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
			return
		case <-ticker.C:
			w.pollAll(ctx)
		}
	}
}

func (w *FileWatcher) pollAll(ctx context.Context) {
	sources := w.registry.DiscoverableSources()
	if len(sources) == 0 {
		return
	}

	w.logger.Debug("polling discoverable sources", "count", len(sources))

	for _, src := range sources {
		name := src.Name()

		hostnames, err := src.Discover(ctx)
		if err != nil {
			w.logger.Warn("discovery failed",
				"source", name,
				"error", err,
			)
			continue
		}

		// Check if hostnames changed
		if w.hasChanged(name, hostnames) {
			w.logger.Info("discovered hostnames changed",
				"source", name,
				"count", len(hostnames),
			)
			w.updateLastSeen(name, hostnames)
			w.callback(name, hostnames)
		}
	}
}

func (w *FileWatcher) hasChanged(sourceName string, hostnames []Hostname) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	current := w.lastSeen[sourceName]
	if current == nil {
		return len(hostnames) > 0
	}

	// Build new set
	newSet := make(map[string]struct{}, len(hostnames))
	for _, h := range hostnames {
		newSet[h.Name] = struct{}{}
	}

	// Check if sets are equal
	if len(current) != len(newSet) {
		return true
	}

	for name := range current {
		if _, ok := newSet[name]; !ok {
			return true
		}
	}

	return false
}

func (w *FileWatcher) updateLastSeen(sourceName string, hostnames []Hostname) {
	w.mu.Lock()
	defer w.mu.Unlock()

	newSet := make(map[string]struct{}, len(hostnames))
	for _, h := range hostnames {
		newSet[h.Name] = struct{}{}
	}
	w.lastSeen[sourceName] = newSet
}
