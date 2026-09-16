// Package health provides HTTP endpoints for health checks and Prometheus metrics.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Health status values.
const (
	StatusReady    = "ready"
	StatusDegraded = "degraded"
	StatusNotReady = "not_ready"
)

// HealthChecker is a function that checks the health of a component.
// Returns an error if the component is unhealthy.
type HealthChecker func(ctx context.Context) error

// DegradedChecker is a function that checks if a component is in a degraded state.
// Returns (true, message) if degraded, (false, "") if not degraded.
// Degraded means the system is functional but not fully healthy (e.g., some providers unavailable).
type DegradedChecker func(ctx context.Context) (degraded bool, message string)

// HealthStatus represents the health status of a component.
type HealthStatus struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// DegradedStatus represents a degraded component.
type DegradedStatus struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Response represents a health check response.
type Response struct {
	Status     string           `json:"status"`
	Components []HealthStatus   `json:"components,omitempty"`
	Degraded   []DegradedStatus `json:"degraded,omitempty"`
}

// Server provides /health, /ready, and /metrics endpoints.
type Server struct {
	address  string
	port     int
	mux      *http.ServeMux
	server   *http.Server
	listener net.Listener
	logger   *slog.Logger
	timeout  time.Duration
	interval time.Duration

	shuttingDown      atomic.Bool
	readiness         atomic.Int32
	checkerGeneration atomic.Uint64
	mu                sync.RWMutex
	checkers          map[string]HealthChecker
	degradedCheckers  map[string]DegradedChecker
}

const (
	readinessNotReady int32 = iota
	readinessReady
	readinessDegraded
)

// Option is a functional option for configuring the Server.
type Option func(*Server)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithTimeout sets the timeout for health checks.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		s.timeout = timeout
	}
}

// WithCheckInterval sets how often active readiness checks run in the
// background. HTTP requests only read the cached result.
func WithCheckInterval(interval time.Duration) Option {
	return func(s *Server) {
		s.interval = interval
	}
}

// WithAddress sets the IP address used by the management listener. The
// application configuration layer validates that non-loopback addresses have
// an explicit network-listener opt-in.
func WithAddress(address string) Option {
	return func(s *Server) {
		s.address = address
	}
}

// New creates a new health server on the specified port.
func New(port int, opts ...Option) *Server {
	s := &Server{
		address:          "127.0.0.1",
		port:             port,
		mux:              http.NewServeMux(),
		logger:           slog.Default(),
		timeout:          5 * time.Second,
		interval:         30 * time.Second,
		checkers:         make(map[string]HealthChecker),
		degradedCheckers: make(map[string]DegradedChecker),
	}
	s.readiness.Store(readinessReady)

	for _, opt := range opts {
		opt(s)
	}

	s.setupRoutes()
	return s
}

// RegisterChecker adds a health checker for the /ready endpoint.
func (s *Server) RegisterChecker(name string, checker HealthChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkers[name] = checker
	s.checkerGeneration.Add(1)
	s.readiness.Store(readinessNotReady)
	s.logger.Debug("registered health checker", slog.String("name", name))
}

// RegisterDegradedChecker adds a degraded state checker for the /ready endpoint.
// Degraded checkers report when the system is functional but not fully healthy.
func (s *Server) RegisterDegradedChecker(name string, checker DegradedChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.degradedCheckers[name] = checker
	s.checkerGeneration.Add(1)
	s.readiness.Store(readinessNotReady)
	s.logger.Debug("registered degraded checker", slog.String("name", name))
}

// SetShuttingDown marks the server as shutting down.
// The /ready endpoint will return 503 Service Unavailable to signal
// load balancers and orchestrators to stop sending traffic.
func (s *Server) SetShuttingDown() {
	s.shuttingDown.Store(true)
	s.logger.Info("health server marked as shutting down, /ready will return 503")
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/ready", s.handleReady)
	s.mux.Handle("/metrics", promhttp.Handler())
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := Response{Status: "healthy"}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Return 503 immediately during shutdown to signal orchestrators
	if s.shuttingDown.Load() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		resp := Response{Status: "shutting_down"}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	status := s.readiness.Load()
	w.Header().Set("Content-Type", "application/json")
	resp := Response{}
	switch status {
	case readinessReady:
		resp.Status = StatusReady
		w.WriteHeader(http.StatusOK)
	case readinessDegraded:
		resp.Status = StatusDegraded
		w.WriteHeader(http.StatusOK)
	default:
		resp.Status = StatusNotReady
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// refreshReadiness performs active checks outside the request path and stores
// only the aggregate result. Detailed component names and upstream errors stay
// in local logs rather than being returned over the management endpoint.
func (s *Server) refreshReadiness(parent context.Context) {
	generation := s.checkerGeneration.Load()
	s.mu.RLock()
	checkers := make(map[string]HealthChecker, len(s.checkers))
	for name, checker := range s.checkers {
		checkers[name] = checker
	}
	degradedCheckers := make(map[string]DegradedChecker, len(s.degradedCheckers))
	for name, checker := range s.degradedCheckers {
		degradedCheckers[name] = checker
	}
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	allHealthy := true
	hasDegraded := false

	// Run health checkers
	for name, checker := range checkers {
		if err := checker(ctx); err != nil {
			allHealthy = false
			s.logger.Warn("health check failed",
				slog.String("component", name),
				slog.String("error", err.Error()),
			)
		}
	}

	// Run degraded checkers
	for name, checker := range degradedCheckers {
		if degraded, message := checker(ctx); degraded {
			hasDegraded = true
			s.logger.Debug("degraded state detected",
				slog.String("component", name),
				slog.String("message", message),
			)
		}
	}
	// A checker registered while this snapshot was running has not been
	// evaluated. RegisterChecker already marked readiness not-ready; do not
	// overwrite that state with a stale result.
	if s.checkerGeneration.Load() != generation {
		return
	}

	if !allHealthy {
		s.readiness.Store(readinessNotReady)
	} else if hasDegraded {
		s.readiness.Store(readinessDegraded)
	} else {
		s.readiness.Store(readinessReady)
	}
}

func (s *Server) runChecks(ctx context.Context) {
	s.refreshReadiness(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshReadiness(ctx)
		}
	}
}

// Start starts the health server in a goroutine.
func (s *Server) Start() error {
	listenAddress := net.JoinHostPort(s.address, strconv.Itoa(s.port))
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listening on management address %s: %w", listenAddress, err)
	}
	s.listener = listener
	s.server = &http.Server{
		Addr:              listenAddress,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	checkCtx, cancelChecks := context.WithCancel(context.Background())
	s.server.RegisterOnShutdown(cancelChecks)
	go s.runChecks(checkCtx)

	go func() {
		s.logger.Info("health server starting",
			slog.String("address", s.address),
			slog.Int("port", s.port),
		)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("health server error", slog.String("error", err.Error()))
		}
	}()

	return nil
}

// Shutdown gracefully shuts down the health server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}
