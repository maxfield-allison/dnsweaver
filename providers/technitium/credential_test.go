package technitium

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

type failingTransport struct {
	err error
}

func (tr failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, tr.err
}

func TestFailedRequestLogsDoNotContainAPIToken(t *testing.T) {
	const token = "test-token-with+reserved&characters"
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, errors.New("connection refused")} {
		t.Run(cause.Error(), func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			client := NewClient("http://dns.example.com:5380", token,
				WithLogger(logger), WithHTTPClient(&http.Client{Transport: failingTransport{err: cause}}))

			err := client.Ping(context.Background())
			logger.Warn("provider request failed", "error", err)

			if !errors.Is(err, cause) {
				t.Fatalf("request error does not preserve its cause: %v", err)
			}
			for _, secret := range []string{token, url.QueryEscape(token), "token="} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("failed request log exposes the token or its query parameter")
				}
			}
		})
	}
}

func TestAPIErrorResponsesDoNotExposeAPIToken(t *testing.T) {
	const token = "test-token-with+reserved&characters"
	for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("token") != token {
					t.Error("request does not retain query authentication for older servers")
				}
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status":       "error",
					"errorMessage": "Record already exists: " + token + " " + url.QueryEscape(token),
				})
			}))
			defer server.Close()
			client := NewClient(server.URL, token)

			err := client.Ping(context.Background())

			if err == nil {
				t.Fatal("expected API error")
			}
			if status == http.StatusOK && !errors.Is(err, provider.ErrConflict) {
				t.Error("redaction discarded the API conflict classification")
			}
			for _, secret := range []string{token, url.QueryEscape(token), `test-token-with+reserved\u0026characters`} {
				if strings.Contains(fmt.Sprintf("%+v", err), secret) {
					t.Error("API error exposes the token")
				}
			}
		})
	}
}

func TestMalformedRequestURLDoesNotExposeAPIToken(t *testing.T) {
	const token = "test-token-with+reserved&characters"
	client := NewClient("http://dns.example.com:invalid-port", token)

	err := client.Ping(context.Background())

	if err == nil {
		t.Fatal("expected invalid URL error")
	}
	for _, secret := range []string{token, url.QueryEscape(token)} {
		if strings.Contains(err.Error(), secret) {
			t.Error("request construction error exposes the token")
		}
	}
}
