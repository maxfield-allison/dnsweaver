package proxmox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestAgentUnavailableResponseShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		status      int
		unavailable bool
	}{
		{"disabled agent message", `{"data":null,"message":"No QEMU guest agent configured\n"}`, 500, true},
		{"stopped agent message", `{"message":"QEMU guest agent is not running"}`, 500, true},
		{"stopped agent errors", `{"errors":{"agent":"QEMU guest agent is not running"}}`, 500, true},
		{"unrelated failure", `{"message":"storage unavailable"}`, 500, false},
		{"permission denied", `{"message":"No QEMU guest agent configured"}`, 403, false},
		{"malformed failure", `not JSON`, 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})
			_, err := client.GetVMAgentNetworks(context.Background(), "node-a", 101)
			if err == nil {
				t.Fatal("expected error")
			}
			var unavailable *ErrAgentNotRunning
			if errors.As(err, &unavailable) != tc.unavailable {
				t.Fatalf("unavailable classification = %v, want %v: %v", unavailable != nil, tc.unavailable, err)
			}
			if unavailable != nil && (unavailable.Node != "node-a" || unavailable.VMID != 101) {
				t.Errorf("guest identity = %s/%d, want node-a/101", unavailable.Node, unavailable.VMID)
			}
		})
	}
}

func TestAgentErrorTextDoesNotHideInventoryFailure(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"errors":{"detail":"No QEMU guest agent configured"}}`)
	})
	_, err := client.ListClusterResources(context.Background())
	var agentErr *ErrAgentNotRunning
	if err == nil || errors.As(err, &agentErr) {
		t.Fatalf("inventory failure must remain an API error: %v", err)
	}
}

func TestAdapterKeepsOtherGuestsWhenAgentDisabled(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/cluster/resources":
			_, _ = fmt.Fprint(w, `{"data":[{"vmid":101,"name":"no-agent","node":"node","type":"qemu","status":"running"},{"vmid":102,"name":"container","node":"node","type":"lxc","status":"running"},{"vmid":103,"name":"vm","node":"node","type":"qemu","status":"running"}]}`)
		case "/api2/json/nodes/node/qemu/101/agent/network-get-interfaces":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"data":null,"message":"No QEMU guest agent configured\n"}`)
		case "/api2/json/nodes/node/lxc/102/config":
			_, _ = fmt.Fprint(w, `{"data":{"net0":"name=eth0,ip=10.0.0.5/24"}}`)
		case "/api2/json/nodes/node/qemu/103/agent/network-get-interfaces":
			_, _ = fmt.Fprint(w, `{"data":{"result":[{"name":"eth0","ip-addresses":[{"ip-address":"10.0.0.6","ip-address-type":"ipv4"}]}]}}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	a := NewWorkloadListerAdapter(client, AdapterConfig{}, testLogger())
	guests, err := a.ListWorkloads(context.Background())
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
	if len(guests) != 3 {
		t.Fatalf("guest count = %d, want 3", len(guests))
	}
	if guests[0].Metadata["ip"] != "" || guests[1].Metadata["ip"] != "10.0.0.5" || guests[2].Metadata["ip"] != "10.0.0.6" {
		t.Fatalf("unexpected guest addresses: %+v", guests)
	}
}
