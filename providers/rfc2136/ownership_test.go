package rfc2136

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/maxfield-allison/dnsweaver/pkg/dnsupdate"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// The fixture handles real DNS wire messages and exact-value UPDATEs over TCP.
func compactDNSServer(t *testing.T) string {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var records []dns.RR
	ready := make(chan struct{})
	server := &dns.Server{Listener: listener, Net: "tcp", MsgAcceptFunc: func(dns.Header) dns.MsgAcceptAction { return dns.MsgAccept }, NotifyStartedFunc: func() { close(ready) }, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		mu.Lock()
		defer mu.Unlock()
		reply := new(dns.Msg)
		reply.SetReply(request)
		reply.Authoritative = true
		if request.Opcode == dns.OpcodeUpdate {
			for _, rr := range request.Ns {
				incoming, _ := dnsupdate.RecordFromRR(rr)
				found := -1
				for i, existing := range records {
					current, _ := dnsupdate.RecordFromRR(existing)
					if current.Name == incoming.Name && current.Type == incoming.Type && current.RData == incoming.RData {
						found = i
						break
					}
				}
				if rr.Header().Class == dns.ClassNONE {
					if found >= 0 {
						records = append(records[:found], records[found+1:]...)
					}
				} else if rr.Header().Class == dns.ClassINET && found < 0 {
					records = append(records, dns.Copy(rr))
				}
			}
		} else {
			for _, question := range request.Question {
				for _, rr := range records {
					if rr.Header().Name == question.Name && rr.Header().Rrtype == question.Qtype {
						reply.Answer = append(reply.Answer, dns.Copy(rr))
					}
				}
			}
		}
		_ = w.WriteMsg(reply)
	})}
	done := make(chan error, 1)
	go func() { done <- server.ActivateAndServe() }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS fixture did not start")
	}
	t.Cleanup(func() {
		_ = server.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return listener.Addr().String()
}

func TestRFC2136CompactOwnershipDiscoversAfterRestartWithoutCatalog(t *testing.T) {
	ctx := context.Background()
	address := compactDNSServer(t)
	newProcess := func(owner string) (*Provider, *provider.CompactOwnershipProvider) {
		raw, err := New(owner, &Config{Server: address, Zone: "example.com.", TTL: 300, Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		raw.EnableCompactOwnership()
		decorated, err := provider.NewCompactOwnershipProvider(raw, owner, "example.com", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return raw, decorated
	}
	raw, a := newProcess("docker")
	for i := range 12 {
		member := provider.Record{Hostname: fmt.Sprintf("app-%d.example.com", i), Type: provider.RecordTypeA, Target: fmt.Sprintf("192.0.2.%d", i+1), TTL: 300}
		if err := a.Create(ctx, member); err != nil {
			t.Fatal(err)
		}
		if err := a.Create(ctx, provider.MemberOwnershipRecord(member.Hostname, 300, "docker", member, nil)); err != nil {
			t.Fatal(err)
		}
	}
	_, b := newProcess("cluster")
	member := provider.Record{Hostname: "cluster.example.com", Type: provider.RecordTypeCNAME, Target: "backend.example.com", TTL: 300}
	if err := b.Create(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := b.Create(ctx, provider.MemberOwnershipRecord(member.Hostname, 300, "cluster", member, nil)); err != nil {
		t.Fatal(err)
	}
	_, restarted := newProcess("docker")
	listed, err := restarted.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, claims := 0, 0
	for _, r := range listed {
		if provider.IsOwnershipRecord(r.Hostname) {
			claims++
		} else if r.Type != provider.RecordTypeTXT {
			data++
		}
		if provider.IsOwnershipStateRecord(r.Hostname) {
			t.Fatal("physical state leaked through logical listing")
		}
	}
	if data != 13 || claims != 13 {
		t.Fatalf("restart recovered data=%d claims=%d, want 13 each", data, claims)
	}
	catalog, err := raw.client.Query(ctx, "_dnsweaver-catalog-0.example.com.", dns.TypeTXT)
	if err != nil || len(catalog) != 0 {
		t.Fatal("compact writers maintained shared legacy catalog", err)
	}
	state, err := raw.ListOwnershipState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range state {
		if strings.HasPrefix(r.Hostname, "_dw-p.") && len(r.Target) > 255 {
			return
		}
	}
	t.Fatal("test did not exercise TXT pages exceeding one wire string")
}
