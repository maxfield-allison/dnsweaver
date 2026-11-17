package pfsense

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestListRejectsErrorEnvelopeWithSuccessfulHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"unauthorized envelope", `{"code":401,"status":"unauthorized","message":"denied","data":null}`, provider.ErrUnauthorized},
		{"failed envelope", `{"code":503,"status":"service unavailable","data":null}`, provider.ErrProviderUnavailable},
		{"missing envelope", `{}`, nil},
		{"null data", `{"code":200,"status":"ok","data":null}`, nil},
		{"missing data", `{"code":200,"status":"ok"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			c := newClient(srv.URL, "dummy", resolverFor(EngineUnbound))
			_, err := c.list(context.Background())
			if err == nil {
				t.Fatal("failure envelope became a successful empty list")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("error=%v, want %v", err, tc.want)
			}
		})
	}
}
