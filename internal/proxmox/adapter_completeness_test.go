package proxmox

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestAdapterReportsIncompleteIPDiscovery(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/cluster/resources":
			_, _ = fmt.Fprint(w, `{"data":[{"vmid":100,"name":"web","node":"node","type":"lxc","status":"running"},{"vmid":101,"name":"db","node":"node","type":"lxc","status":"running"}]}`)
		case "/api2/json/nodes/node/lxc/100/config":
			_, _ = fmt.Fprint(w, `{"data":{"net0":"name=eth0,ip=10.0.0.5/24"}}`)
		case "/api2/json/nodes/node/lxc/101/config":
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	a := NewWorkloadListerAdapter(client, AdapterConfig{}, testLogger())
	if _, err := a.ListWorkloads(context.Background()); err == nil {
		t.Fatal("a failed guest lookup was reported as a successful complete snapshot")
	}
}

func TestAdapterReportsFailedLiveAddressDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name, net string
		version   IPVersion
	}{
		{name: "DHCP", net: "name=eth0,ip=dhcp", version: IPVersionIPv4},
		{name: "missing IPv6 alongside known IPv4", net: "name=eth0,ip=10.0.0.5/24", version: IPVersionDual},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api2/json/cluster/resources":
					_, _ = fmt.Fprint(w, `{"data":[{"vmid":100,"name":"web","node":"node","type":"lxc","status":"running"}]}`)
				case "/api2/json/nodes/node/lxc/100/config":
					_, _ = fmt.Fprintf(w, `{"data":{"net0":%q}}`, tc.net)
				case "/api2/json/nodes/node/lxc/100/interfaces":
					http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			a := NewWorkloadListerAdapter(client, AdapterConfig{IPVersion: tc.version}, testLogger())
			if _, err := a.ListWorkloads(context.Background()); err == nil {
				t.Fatal("a failed live address read was reported as successful absence")
			}
		})
	}
}
