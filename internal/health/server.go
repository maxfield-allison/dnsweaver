// Package health provides HTTP endpoints for health checks and Prometheus metrics.
package health

import (
	"net/http"
)

// Server provides /health, /ready, and /metrics endpoints.
type Server struct {
	port int
	mux  *http.ServeMux
}

// New creates a new health server on the specified port.
func New(port int) *Server {
	s := &Server{
		port: port,
		mux:  http.NewServeMux(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/ready", s.handleReady)
	// TODO: Add /metrics endpoint with Prometheus handler
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// TODO: Check provider connectivity in Issue #8
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}

// TODO: Implement in Issue #8 - Health & metrics endpoints
// - Start() method
// - Provider health checks for /ready
// - Prometheus metrics integration
