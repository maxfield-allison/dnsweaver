package kubernetes

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type groupDiscovery struct {
	discovery.DiscoveryInterface
	err error
}

func (d *groupDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return &metav1.APIGroupList{}, d.err
}

func TestWatcherDiscoveryFailureDoesNotBecomeAbsentCRDs(t *testing.T) {
	disc := &groupDiscovery{err: errors.New("discovery temporarily unavailable")}
	w := New(&Clients{Discovery: disc}, WithConfig(Config{WatchIngressRoute: true}),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer w.Stop()
	if err := w.Start(ctx); err == nil {
		t.Fatal("failed API discovery was treated as an authoritative absence of CRDs")
	}
	if w.IsRunning() {
		t.Fatal("failed startup left the watcher running")
	}
	disc.err = nil
	if err := w.Start(ctx); err != nil {
		t.Fatalf("startup did not recover after discovery recovered: %v", err)
	}
}

func TestWatcherFailedCacheSyncCannotPublishEmptySnapshot(t *testing.T) {
	client := kubernetesfake.NewClientset()
	client.PrependReactor("list", "ingresses", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("ingresses temporarily unavailable")
	})
	w := New(&Clients{Typed: client}, WithConfig(Config{WatchIngress: true}),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	defer w.Stop()
	if err := w.Start(ctx); err == nil {
		t.Fatal("an unsynchronized informer was reported as successfully started")
	}
	if _, err := w.ListWorkloads(context.Background()); err == nil {
		t.Fatal("an unsynchronized cache was returned as an authoritative empty snapshot")
	}
}

func TestWatcherRejectsListingBeforeStart(t *testing.T) {
	w := New(nil)
	if _, err := w.ListWorkloads(context.Background()); err == nil {
		t.Fatal("an unstarted watcher returned an authoritative empty snapshot")
	}
}
