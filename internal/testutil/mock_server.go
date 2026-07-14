package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// RecordedRequest captures an HTTP request made to a MockServer.
type RecordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

// MockServer wraps httptest.Server with path-based routing and request recording.
// It allows tests to configure per-path responses and verify which requests were made.
type MockServer struct {
	*httptest.Server

	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	fallback http.HandlerFunc
	requests []RecordedRequest
}

// NewMockServer creates a mock HTTP server with request recording.
// The server is automatically cleaned up when the test ends.
func NewMockServer(t *testing.T) *MockServer {
	t.Helper()

	m := &MockServer{
		handlers: make(map[string]http.HandlerFunc),
		requests: make([]RecordedRequest, 0),
	}

	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read body for recording
		var body []byte
		if r.Body != nil {
			body = make([]byte, 0, 1024)
			buf := make([]byte, 1024)
			for {
				n, err := r.Body.Read(buf)
				if n > 0 {
					body = append(body, buf[:n]...)
				}
				if err != nil {
					break
				}
			}
		}

		// Record the request
		m.mu.Lock()
		m.requests = append(m.requests, RecordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Header: r.Header.Clone(),
			Body:   body,
		})

		// Find handler for this path
		handler, ok := m.handlers[r.URL.Path]
		m.mu.Unlock()

		if ok {
			handler(w, r)
			return
		}

		// Try fallback
		if m.fallback != nil {
			m.fallback(w, r)
			return
		}

		// Default: 404
		http.NotFound(w, r)
	}))

	t.Cleanup(m.Close)
	return m
}

// Handle registers a handler for the given path.
func (m *MockServer) Handle(path string, handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[path] = handler
}

// HandleDefault sets a fallback handler for unmatched paths.
func (m *MockServer) HandleDefault(handler http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallback = handler
}

// Requests returns all recorded requests.
func (m *MockServer) Requests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]RecordedRequest, len(m.requests))
	copy(result, m.requests)
	return result
}

// RequestsForPath returns all recorded requests for the given path.
func (m *MockServer) RequestsForPath(path string) []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []RecordedRequest
	for _, r := range m.requests {
		if r.Path == path {
			result = append(result, r)
		}
	}
	return result
}

// RequestCount returns total number of recorded requests.
func (m *MockServer) RequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// ResetRequests clears all recorded requests.
func (m *MockServer) ResetRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = make([]RecordedRequest, 0)
}

// --- Common handler factories ---

// JSONResponse returns a handler that responds with the given status and JSON body.
func JSONResponse(status int, body any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}
}

// JSONBytes returns a handler that responds with raw JSON bytes.
func JSONBytes(status int, data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(data)
	}
}

// ErrorResponse returns a handler that responds with the given status and error message.
func ErrorResponse(status int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{keyError: message})
	}
}

// EmptyResponse returns a handler that responds with the given status and no body.
func EmptyResponse(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}
}
