// Package health provides HTTP endpoints for health checks and Prometheus metrics.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
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
	port    int
	mux     *http.ServeMux
	server  *http.Server
	logger  *slog.Logger
	timeout time.Duration

	mu               sync.RWMutex
	checkers         map[string]HealthChecker
	degradedCheckers map[string]DegradedChecker
}

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

// New creates a new health server on the specified port.
func New(port int, opts ...Option) *Server {
	s := &Server{
		port:             port,
		mux:              http.NewServeMux(),
		logger:           slog.Default(),
		timeout:          5 * time.Second,
		checkers:         make(map[string]HealthChecker),
		degradedCheckers: make(map[string]DegradedChecker),
	}

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
	s.logger.Debug("registered health checker", slog.String("name", name))
}

// RegisterDegradedChecker adds a degraded state checker for the /ready endpoint.
// Degraded checkers report when the system is functional but not fully healthy.
func (s *Server) RegisterDegradedChecker(name string, checker DegradedChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.degradedCheckers[name] = checker
	s.logger.Debug("registered degraded checker", slog.String("name", name))
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

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	var components []HealthStatus
	var degradedList []DegradedStatus
	allHealthy := true
	hasDegraded := false

	// Run health checkers
	for name, checker := range checkers {
		status := HealthStatus{Name: name, Healthy: true}
		if err := checker(ctx); err != nil {
			status.Healthy = false
			status.Error = err.Error()
			allHealthy = false
			s.logger.Warn("health check failed",
				slog.String("component", name),
				slog.String("error", err.Error()),
			)
		}
		components = append(components, status)
	}

	// Run degraded checkers
	for name, checker := range degradedCheckers {
		if degraded, message := checker(ctx); degraded {
			hasDegraded = true
			degradedList = append(degradedList, DegradedStatus{
				Name:    name,
				Message: message,
			})
			s.logger.Debug("degraded state detected",
				slog.String("component", name),
				slog.String("message", message),
			)
		}
	}

	w.Header().Set("Content-Type", "application/json")

	resp := Response{Components: components, Degraded: degradedList}
	if !allHealthy {
		// Unhealthy - at least one health checker failed
		resp.Status = StatusNotReady
		w.WriteHeader(http.StatusServiceUnavailable)
	} else if hasDegraded {
		// Healthy but degraded - all checkers passed but some degradation
		resp.Status = StatusDegraded
		w.WriteHeader(http.StatusOK) // 200 OK for degraded (still functional)
	} else {
		// Fully healthy
		resp.Status = StatusReady
		w.WriteHeader(http.StatusOK)
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// Start starts the health server in a goroutine.
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		s.logger.Info("health server starting", slog.Int("port", s.port))
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
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
