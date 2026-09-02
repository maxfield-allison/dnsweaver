package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestProbe(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "healthy", statusCode: http.StatusOK},
		{name: "unhealthy status", statusCode: http.StatusServiceUnavailable, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			port := serverPort(t, server.Listener.Addr())
			err := Probe(context.Background(), port)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Probe() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestProbeConnectionFailure(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := serverPort(t, listener.Addr())
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err = Probe(context.Background(), port)
	if err == nil {
		t.Fatal("Probe() error = nil, want a connection error")
	}
}

func serverPort(t *testing.T, addr net.Addr) int {
	t.Helper()

	_, portString, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portString, err)
	}
	return port
}

func TestProbeErrorIncludesEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Probe(ctx, 18080)
	if err == nil {
		t.Fatal("Probe() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:18080/health") {
		t.Errorf("Probe() error = %q, want endpoint", err)
	}
}
