// Package provider contains the provider manager for graceful provider initialization.
package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/internal/metrics"
)

// ManagerConfig holds configuration for the provider manager.
type ManagerConfig struct {
	// InitialRetryInterval is the initial interval between retry attempts for failed providers.
	// Default: 5 seconds.
	InitialRetryInterval time.Duration

	// MaxRetryInterval is the maximum interval between retry attempts (caps exponential backoff).
	// Default: 5 minutes.
	MaxRetryInterval time.Duration

	// RetryBackoffMultiplier is the multiplier for exponential backoff.
	// Default: 2.0.
	RetryBackoffMultiplier float64
}

// DefaultManagerConfig returns a ManagerConfig with sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		InitialRetryInterval:   5 * time.Second,
		MaxRetryInterval:       5 * time.Minute,
		RetryBackoffMultiplier: 2.0,
	}
}

// PendingProvider holds configuration and state for a provider that failed to initialize.
type PendingProvider struct {
	Config        ProviderInstanceConfig
	LastError     error
	LastAttempt   time.Time
	AttemptCount  int
	NextRetryAt   time.Time
	RetryInterval time.Duration
}

// Manager handles graceful provider initialization with retry logic.
// It wraps a Registry and provides:
//   - Non-fatal initialization: providers that fail to connect don't crash the app
//   - Background retry: failed providers are retried with exponential backoff
//   - Status reporting: tracks which providers are ready vs pending
type Manager struct {
	registry *Registry
	config   ManagerConfig
	logger   *slog.Logger

	mu      sync.RWMutex
	pending map[string]*PendingProvider // name -> pending config
	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool
}

// ManagerOption is a functional option for configuring the Manager.
type ManagerOption func(*Manager)

// WithManagerConfig sets the manager configuration.
func WithManagerConfig(cfg ManagerConfig) ManagerOption {
	return func(m *Manager) {
		m.config = cfg
	}
}

// WithManagerLogger sets a custom logger for the manager.
func WithManagerLogger(logger *slog.Logger) ManagerOption {
	return func(m *Manager) {
		m.logger = logger
	}
}

// NewManager creates a new provider manager wrapping the given registry.
func NewManager(registry *Registry, opts ...ManagerOption) *Manager {
	m := &Manager{
		registry: registry,
		config:   DefaultManagerConfig(),
		logger:   slog.Default(),
		pending:  make(map[string]*PendingProvider),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// InitializeProvider attempts to create a provider instance and verify connectivity.
// If initialization or connectivity check fails, the provider is added to the pending list for retry.
// Returns nil on success or if the provider is queued for retry.
// Only returns an error if the configuration itself is invalid.
func (m *Manager) InitializeProvider(cfg ProviderInstanceConfig) error {
	// Validate configuration first - invalid configs should fail immediately
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid provider config %q: %w", cfg.Name, err)
	}

	// Attempt to create the provider instance
	err := m.registry.CreateInstance(cfg)
	if err == nil {
		// Provider created successfully - verify connectivity with Ping
		if inst, ok := m.registry.Get(cfg.Name); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			pingErr := inst.Provider.Ping(ctx)
			cancel()

			if pingErr != nil {
				// Created but not reachable - remove from registry and queue for retry
				m.registry.Remove(cfg.Name)
				err = fmt.Errorf("connectivity check failed: %w", pingErr)
				m.logger.Warn("provider created but connectivity check failed",
					slog.String("provider", cfg.Name),
					slog.String("type", cfg.TypeName),
					slog.String("error", pingErr.Error()),
				)
			} else {
				// Fully initialized and reachable
				m.logger.Info("provider initialized and connected",
					slog.String("provider", cfg.Name),
					slog.String("type", cfg.TypeName),
				)
				// Record metrics
				metrics.ProviderAvailable.WithLabelValues(cfg.Name, cfg.TypeName).Set(1)
				m.updateCountMetrics()
				return nil
			}
		}
	}

	// Provider failed to initialize - add to pending list
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pending[cfg.Name] = &PendingProvider{
		Config:        cfg,
		LastError:     err,
		LastAttempt:   time.Now(),
		AttemptCount:  1,
		NextRetryAt:   time.Now().Add(m.config.InitialRetryInterval),
		RetryInterval: m.config.InitialRetryInterval,
	}

	// Record metrics
	metrics.ProviderAvailable.WithLabelValues(cfg.Name, cfg.TypeName).Set(0)
	metrics.ProviderInitRetries.WithLabelValues(cfg.Name, "failed").Inc()
	m.updateCountMetricsLocked()

	m.logger.Warn("provider initialization failed, will retry",
		slog.String("provider", cfg.Name),
		slog.String("type", cfg.TypeName),
		slog.String("error", err.Error()),
		slog.Duration("retry_in", m.config.InitialRetryInterval),
	)

	return nil
}

// Start begins the background retry loop for pending providers.
// Call this after initializing all providers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("provider manager already running")
	}
	m.running = true
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})
	m.mu.Unlock()

	go m.retryLoop(ctx)

	m.logger.Info("provider manager started",
		slog.Int("ready_providers", m.registry.Count()),
		slog.Int("pending_providers", m.PendingCount()),
	)

	return nil
}

// Stop gracefully shuts down the background retry loop.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopCh)
	m.mu.Unlock()

	// Wait for the retry loop to finish
	<-m.doneCh
	m.logger.Info("provider manager stopped")
}

// retryLoop is the background goroutine that retries failed providers.
func (m *Manager) retryLoop(ctx context.Context) {
	defer close(m.doneCh)

	// Check for pending providers every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.retryPendingProviders(ctx)
		}
	}
}

// retryPendingProviders checks and retries providers that are due for retry.
func (m *Manager) retryPendingProviders(ctx context.Context) {
	m.mu.Lock()
	// Get providers due for retry
	var toRetry []*PendingProvider
	now := time.Now()
	for _, pending := range m.pending {
		if now.After(pending.NextRetryAt) || now.Equal(pending.NextRetryAt) {
			toRetry = append(toRetry, pending)
		}
	}
	m.mu.Unlock()

	for _, pending := range toRetry {
		m.retryProvider(ctx, pending)
	}
}

// retryProvider attempts to initialize a single pending provider.
func (m *Manager) retryProvider(ctx context.Context, pending *PendingProvider) {
	cfg := pending.Config

	m.logger.Debug("retrying provider initialization",
		slog.String("provider", cfg.Name),
		slog.Int("attempt", pending.AttemptCount+1),
	)

	// Attempt to create the provider instance
	err := m.registry.CreateInstance(cfg)

	if err == nil {
		// Provider created - verify connectivity with Ping
		if inst, ok := m.registry.Get(cfg.Name); ok {
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			pingErr := inst.Provider.Ping(pingCtx)
			cancel()

			if pingErr != nil {
				// Created but ping failed - remove from registry and continue retry
				m.registry.Remove(cfg.Name)
				err = fmt.Errorf("connectivity check failed: %w", pingErr)
				m.logger.Debug("provider created but connectivity check failed during retry",
					slog.String("provider", cfg.Name),
					slog.String("error", pingErr.Error()),
				)
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err == nil {
		// Success! Remove from pending list
		delete(m.pending, cfg.Name)

		// Record metrics
		metrics.ProviderAvailable.WithLabelValues(cfg.Name, cfg.TypeName).Set(1)
		metrics.ProviderInitRetries.WithLabelValues(cfg.Name, "success").Inc()
		m.updateCountMetricsLocked()

		m.logger.Info("provider initialized and connected after retry",
			slog.String("provider", cfg.Name),
			slog.String("type", cfg.TypeName),
			slog.Int("attempts", pending.AttemptCount+1),
		)
		return
	}

	// Still failing - update retry state with exponential backoff
	pending.LastError = err
	pending.LastAttempt = time.Now()
	pending.AttemptCount++

	// Calculate next retry interval with exponential backoff
	newInterval := time.Duration(float64(pending.RetryInterval) * m.config.RetryBackoffMultiplier)
	if newInterval > m.config.MaxRetryInterval {
		newInterval = m.config.MaxRetryInterval
	}
	pending.RetryInterval = newInterval
	pending.NextRetryAt = time.Now().Add(newInterval)

	// Record failed retry metric
	metrics.ProviderInitRetries.WithLabelValues(cfg.Name, "failed").Inc()

	m.logger.Warn("provider retry failed",
		slog.String("provider", cfg.Name),
		slog.String("error", err.Error()),
		slog.Int("attempt", pending.AttemptCount),
		slog.Duration("next_retry_in", newInterval),
	)
}

// updateCountMetrics updates the providers_ready and providers_pending gauge metrics.
// Must not hold the lock when calling this method.
func (m *Manager) updateCountMetrics() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.updateCountMetricsLocked()
}

// updateCountMetricsLocked updates the providers_ready and providers_pending gauge metrics.
// Caller must hold at least a read lock.
func (m *Manager) updateCountMetricsLocked() {
	metrics.ProvidersReady.Set(float64(m.registry.Count()))
	metrics.ProvidersPending.Set(float64(len(m.pending)))
}

// Registry returns the underlying provider registry.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// PendingCount returns the number of providers pending initialization.
func (m *Manager) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// ReadyCount returns the number of ready (initialized) providers.
func (m *Manager) ReadyCount() int {
	return m.registry.Count()
}

// TotalCount returns the total number of configured providers (ready + pending).
func (m *Manager) TotalCount() int {
	return m.ReadyCount() + m.PendingCount()
}

// IsFullyReady returns true if all configured providers are initialized.
func (m *Manager) IsFullyReady() bool {
	return m.PendingCount() == 0
}

// PendingProviders returns information about providers pending initialization.
func (m *Manager) PendingProviders() []PendingProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PendingProviderStatus, 0, len(m.pending))
	for _, p := range m.pending {
		result = append(result, PendingProviderStatus{
			Name:         p.Config.Name,
			Type:         p.Config.TypeName,
			LastError:    p.LastError.Error(),
			LastAttempt:  p.LastAttempt,
			AttemptCount: p.AttemptCount,
			NextRetryAt:  p.NextRetryAt,
		})
	}

	return result
}

// PendingProviderStatus holds status information for a pending provider.
type PendingProviderStatus struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	LastError    string    `json:"last_error"`
	LastAttempt  time.Time `json:"last_attempt"`
	AttemptCount int       `json:"attempt_count"`
	NextRetryAt  time.Time `json:"next_retry_at"`
}

// ProviderStatus represents the availability status of a provider for health checks.
type ProviderStatus struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// AllProviderStatuses returns the status of all configured providers (ready and pending).
func (m *Manager) AllProviderStatuses() []ProviderStatus {
	statuses := make([]ProviderStatus, 0)

	// Add ready providers
	for _, inst := range m.registry.All() {
		statuses = append(statuses, ProviderStatus{
			Name:      inst.Name(),
			Type:      inst.Type(),
			Available: true,
		})
	}

	// Add pending providers
	m.mu.RLock()
	for _, p := range m.pending {
		statuses = append(statuses, ProviderStatus{
			Name:      p.Config.Name,
			Type:      p.Config.TypeName,
			Available: false,
			Error:     p.LastError.Error(),
		})
	}
	m.mu.RUnlock()

	return statuses
}
