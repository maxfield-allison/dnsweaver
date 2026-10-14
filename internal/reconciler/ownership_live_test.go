//go:build integration

package reconciler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	k8sclient "github.com/maxfield-allison/dnsweaver/internal/kubernetes"
	"github.com/maxfield-allison/dnsweaver/internal/kubernetes/resources"
	"github.com/maxfield-allison/dnsweaver/internal/testutil"
	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/providers/cloudflare"
	"github.com/maxfield-allison/dnsweaver/providers/technitium"
	nativesource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
	k8ssource "github.com/maxfield-allison/dnsweaver/sources/kubernetes"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

// This test requires explicit protected configuration. It changes only records
// under its fresh prefix and its two unique owner namespaces. Production source
// workloads are read, never changed. Each actor is a separate disposable process.
type liveOwnershipConfig struct {
	Provider       string
	Zone           string
	ZoneID         string
	URL            string
	TokenFile      string
	Target         string
	RecordType     provider.RecordType
	AutoHTTPS      bool
	Prefix         string
	Owners         []string
	Kubeconfig     string
	DockerAddress  string
	SSHKey         string
	NameServers    []string
	TraceHTTP      bool
	ProbeRecords   string
	Selected       [][]string
	LegacyReader   string
	Output         string
	Actor          int
	Mode           string
	Phase          string
	Limit          int
	DryRun         bool
	SourceFailure  bool
	WithdrawShared bool
}

func liveConfig(t *testing.T) liveOwnershipConfig {
	t.Helper()
	path := os.Getenv("DNSWEAVER_LIVE_CONFIG")
	if path == "" {
		t.Skip("DNSWEAVER_LIVE_CONFIG not set; no live DNS writes authorized")
	}
	var cfg liveOwnershipConfig
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.Prefix, "qa-") || len(cfg.Owners) != 2 || cfg.Owners[0] == cfg.Owners[1] ||
		!strings.HasPrefix(cfg.Owners[0], cfg.Prefix+"-") || !strings.HasPrefix(cfg.Owners[1], cfg.Prefix+"-") {
		t.Fatal("qualification requires a fresh qa prefix and two distinct scoped owners")
	}
	return cfg
}

func liveNative(cfg liveOwnershipConfig) (provider.Provider, error) {
	token, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return nil, err
	}
	switch cfg.Provider {
	case "cloudflare":
		opts := []cloudflare.ProviderOption{cloudflare.WithProviderLogger(quietLogger())}
		if cfg.TraceHTTP {
			if err := os.MkdirAll(cfg.Output, 0o700); err != nil {
				return nil, err
			}
			client := httputil.DefaultClient()
			transport := client.Transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			client.Transport = liveHTTPTrace{transport, filepath.Join(cfg.Output, fmt.Sprintf("http-%d.jsonl", os.Getpid()))}
			opts = append(opts, cloudflare.WithProviderHTTPClient(client))
		}
		return cloudflare.New("live", &cloudflare.Config{Zone: cfg.Zone, ZoneID: cfg.ZoneID, Token: strings.TrimSpace(string(token)), TTL: 300}, opts...)
	case "technitium":
		return technitium.New("live", &technitium.Config{URL: cfg.URL, Zone: cfg.Zone, Token: strings.TrimSpace(string(token)), TTL: 300, AutoHTTPSRecords: cfg.AutoHTTPS, AutoHTTPSALPN: "h2"}, technitium.WithProviderLogger(quietLogger()))
	default:
		return nil, fmt.Errorf("unsupported live qualification provider")
	}
}

// Record only request metadata. Headers, query values, credentials and response
// bodies never enter the trace. Names and payload sizes identify a rejected page.
type liveHTTPTrace struct {
	transport http.RoundTripper
	path      string
}

func (trace liveHTTPTrace) RoundTrip(request *http.Request) (*http.Response, error) {
	entry := map[string]any{"method": request.Method, "path": request.URL.Path}
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		var record struct{ Name, Type, Content string }
		decodeErr := json.NewDecoder(body).Decode(&record)
		closeErr := body.Close()
		if decodeErr != nil || closeErr != nil {
			return nil, errors.Join(decodeErr, closeErr)
		}
		entry["name"], entry["type"], entry["content_bytes"] = record.Name, record.Type, len(record.Content)
	}
	started := time.Now()
	response, transportErr := trace.transport.RoundTrip(request)
	entry["elapsed_ms"], entry["transport_error"] = time.Since(started).Milliseconds(), transportErr != nil
	if response != nil {
		entry["status"], entry["ray"] = response.StatusCode, response.Header.Get("CF-Ray")
	}
	file, err := os.OpenFile(trace.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	writeErr := json.NewEncoder(file).Encode(entry)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, errors.Join(writeErr, closeErr)
	}
	return response, transportErr
}

func liveRegistry(cfg liveOwnershipConfig) (*provider.Registry, error) {
	registry := provider.NewRegistry(quietLogger())
	registry.SetInstanceID(cfg.Owners[cfg.Actor])
	registry.SetOwnershipStorage(cfg.Mode)
	registry.RegisterFactory(cfg.Provider, func(provider.FactoryConfig) (provider.Provider, error) { return liveNative(cfg) })
	err := registry.CreateInstance(provider.ProviderInstanceConfig{Name: "live", TypeName: cfg.Provider,
		RecordType: cfg.RecordType, Target: cfg.Target, TTL: 300, Mode: provider.ModeManaged,
		Domains: []string{"*." + cfg.Prefix + "." + cfg.Zone}, ProviderConfig: map[string]string{"ZONE": cfg.Zone}})
	return registry, err
}

func liveWorkloads(ctx context.Context, cfg liveOwnershipConfig, actor int) ([]workload.Workload, error) {
	if actor == 1 {
		clients, err := k8sclient.NewClients(cfg.Kubeconfig)
		if err != nil {
			return nil, err
		}
		list, err := clients.Dynamic.Resource(schema.GroupVersionResource{Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"}).Namespace("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		var result []workload.Workload
		for i := range list.Items {
			result = append(result, resources.ConvertIngressRoute(&list.Items[i]))
		}
		return result, nil
	}
	// Filter on the remote machine so container environment and middleware
	// credentials never leave it. Read only hostname declarations and enable flags.
	code := `import json,subprocess
ids=subprocess.check_output(["docker","ps","-q"],text=True).split()
items=json.loads(subprocess.check_output(["docker","inspect",*ids],text=True)) if ids else []
result=[]
for item in items:
 labels={k:v for k,v in (item["Config"].get("Labels") or {}).items() if (k.startswith("traefik.http.routers.") and k.endswith((".rule",".entrypoints"))) or (k.startswith("dnsweaver.") and k.endswith((".hostname",".hostnames",".enabled")))}
 result.append({"ID":item["Id"],"Name":item["Name"].lstrip("/"),"Labels":labels,"Platform":"docker","Kind":"container"})
print(json.dumps(result))`
	command := exec.CommandContext(ctx, "ssh", "-F", "/dev/null", "-i", cfg.SSHKey, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10",
		"administrator@"+cfg.DockerAddress, "python3 -c '"+strings.ReplaceAll(code, "'", "'\"'\"'")+"'")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("live Docker declaration read failed: %w", err)
	}
	var result []workload.Workload
	err = json.Unmarshal(output, &result)
	return result, err
}

func liveSources() *source.Registry {
	registry := source.NewRegistry(quietLogger())
	_ = registry.Register(nativesource.New(nativesource.WithLogger(quietLogger())))
	_ = registry.Register(traefik.New(traefik.WithLogger(quietLogger())))
	_ = registry.Register(k8ssource.New(k8ssource.WithLogger(quietLogger())))
	return registry
}

func liveAlias(cfg liveOwnershipConfig, original string) string {
	digest := sha256.Sum256([]byte(original))
	return fmt.Sprintf("h-%x.%s.%s", digest[:6], cfg.Prefix, cfg.Zone)
}

type liveLister struct{ cfg liveOwnershipConfig }

func (l liveLister) Platform() workload.Platform { return workload.PlatformStatic }
func (l liveLister) ListWorkloads(ctx context.Context) ([]workload.Workload, error) {
	if l.cfg.SourceFailure {
		return nil, fmt.Errorf("controlled discovery interruption")
	}
	items, err := liveWorkloads(ctx, l.cfg, l.cfg.Actor)
	if err != nil {
		return nil, err
	}
	allowed := l.cfg.Selected[l.cfg.Actor][:min(l.cfg.Limit, len(l.cfg.Selected[l.cfg.Actor]))]
	registry := liveSources()
	seen := map[string]bool{}
	var names []string
	for _, item := range items {
		hosts, complete := registry.ExtractAllWithStatus(ctx, item)
		if !complete {
			return nil, fmt.Errorf("live declaration extraction incomplete")
		}
		for _, host := range hosts {
			if slices.Contains(allowed, host.Name) && !seen[host.Name] {
				seen[host.Name] = true
				names = append(names, liveAlias(l.cfg, host.Name))
			}
		}
	}
	shared := liveAlias(l.cfg, l.cfg.Selected[0][0])
	if l.cfg.WithdrawShared {
		names = slices.DeleteFunc(names, func(name string) bool { return name == shared })
	} else if l.cfg.Actor == 1 && !slices.Contains(names, shared) {
		names = append(names, shared)
	}
	return []workload.Workload{{ID: "live-scope", Name: "live-scope", Platform: workload.PlatformKubernetes, Kind: workload.KindIngressRoute, Hostnames: names}}, nil
}

func TestOwnershipLiveActor(t *testing.T) {
	if os.Getenv("DNSWEAVER_LIVE_ACTOR") != "1" {
		t.Skip("internal disposable actor")
	}
	cfg := liveConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	registry, err := liveRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance, _ := registry.Get("live")
	if cfg.Phase == "migrate" {
		migrator := instance.Provider.(provider.OwnershipMigrator)
		for attempt := range 10 {
			err = migrator.MigrateOwnership(ctx)
			if err == nil {
				return
			}
			if (!errors.Is(err, provider.ErrOwnershipState) && !liveTransientRead(err.Error())) || attempt == 9 {
				t.Fatal(err)
			}
			t.Logf("migration read rejected; retrying a complete operation: %s", liveSafeError(cfg, err))
			time.Sleep(100 * time.Millisecond)
		}
		return
	}
	settings := DefaultConfig()
	settings.InstanceID, settings.DryRun = cfg.Owners[cfg.Actor], cfg.DryRun
	sources := source.NewRegistry(quietLogger())
	if err := sources.Register(k8ssource.New(k8ssource.WithLogger(quietLogger()))); err != nil {
		t.Fatal(err)
	}
	reconciler := New([]workload.Lister{liveLister{cfg}}, sources, registry, WithConfig(settings), WithLogger(quietLogger()))
	if err := reconciler.RecoverOwnership(ctx); err != nil {
		// The daemon logs recovery failure and continues to reconciliation. Keep
		// that behavior, while retaining the underlying read error for diagnosis.
		_, cause := instance.RecoverOwnedHostnames(ctx)
		t.Logf("startup recovery failed; subsequent read: %s", liveSafeError(cfg, cause))
	}
	// The daemon reconciles again after a rejected, changing snapshot. Exercise
	// that normal recovery rather than requiring every concurrent cycle to pass.
	// Final ownership assertions still reject any unclaimed created answer.
	for attempt := range 5 {
		result, err := reconciler.Reconcile(ctx)
		if cfg.SourceFailure {
			if err == nil {
				t.Fatal("interrupted discovery unexpectedly succeeded")
			}
			return
		}
		if err != nil {
			if liveTransientRead(err.Error()) && attempt < 4 {
				t.Logf("provider read rejected; retrying normal reconciliation: %s", liveSafeError(cfg, err))
				time.Sleep(250 * time.Millisecond)
				continue
			}
			t.Fatal(err)
		}
		if !result.HasErrors() {
			fmt.Printf("LIVE_RESULT actor=%d phase=%s desired=%d created=%d deleted=%d retries=%d\n", cfg.Actor, cfg.Phase, result.DesiredMembers, len(result.Created()), len(result.Deleted()), attempt)
			return
		}
		transient := true
		for _, failure := range result.Failed() {
			transient = transient && (strings.Contains(failure.Error, "ownership changed") || liveTransientRead(failure.Error))
		}
		if !transient || attempt == 4 {
			t.Fatalf("reconcile failed: %+v", result.Failed())
		}
		t.Logf("changing ownership snapshot rejected; retrying normal reconciliation (attempt %d)", attempt+1)
		time.Sleep(250 * time.Millisecond)
	}
}

func liveTransientRead(message string) bool {
	return strings.Contains(message, "listing") && strings.Contains(message, "Operation is not valid due to the current state of the object.")
}

func liveSafeError(cfg liveOwnershipConfig, err error) string {
	if err == nil {
		return "succeeded"
	}
	token, readErr := os.ReadFile(cfg.TokenFile)
	if readErr != nil || len(strings.TrimSpace(string(token))) == 0 {
		return "provider read failed (details suppressed)"
	}
	return strings.ReplaceAll(err.Error(), strings.TrimSpace(string(token)), "[redacted]")
}

func TestOwnershipLiveWorkloads(t *testing.T) {
	if os.Getenv("DNSWEAVER_LIVE_ACTOR") == "1" {
		t.Skip("coordinator runs in parent only")
	}
	cfg := liveConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	raw, err := liveNative(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := raw.List(ctx)
	if err != nil || len(initial) != 0 {
		t.Fatal("live qualification requires a zone with no provider-managed record types", err)
	}
	if len(cfg.Selected) == 0 {
		for actor := range 2 {
			items, err := liveWorkloads(ctx, cfg, actor)
			if err != nil {
				t.Fatal(err)
			}
			sources := liveSources()
			seen := map[string]bool{}
			for _, item := range items {
				hosts, complete := sources.ExtractAllWithStatus(ctx, item)
				if !complete {
					t.Fatal("live extraction incomplete")
				}
				for _, host := range hosts {
					seen[host.Name] = true
				}
			}
			var names []string
			for name := range seen {
				names = append(names, name)
			}
			slices.Sort(names)
			if len(names) < 8 {
				t.Fatalf("live actor %d exposes only %d distinct hostname declarations", actor, len(names))
			}
			cfg.Selected = append(cfg.Selected, names[:8])
			t.Logf("actor %d: read %d live workloads, selected 8 of %d declared hostnames", actor, len(items), len(names))
		}
	}
	if err := os.MkdirAll(cfg.Output, 0o700); err != nil {
		t.Fatal(err)
	}
	save := func(name string, value any) {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg.Output, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	owned := func(record provider.Record) bool {
		if strings.HasSuffix(record.Hostname, "."+cfg.Prefix+"."+cfg.Zone) {
			return true
		}
		for _, owner := range cfg.Owners {
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(owner)))[:16]
			if record.Hostname == provider.OwnershipOwnerManifestName(cfg.Zone, owner) || strings.HasPrefix(record.Hostname, "_dw-p."+hash+".") {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cleanupCancel()
		records, err := raw.List(cleanupCtx)
		if err != nil {
			t.Error("cleanup listing failed", err)
			return
		}
		save("before-cleanup.json", records)
		for _, record := range records {
			if owned(record) {
				current, listErr := raw.List(cleanupCtx)
				if listErr != nil {
					t.Error("cleanup verification failed", listErr)
					return
				}
				present := slices.ContainsFunc(current, func(member provider.Record) bool {
					return member.Hostname == record.Hostname && provider.SameRecordMember(member, record)
				})
				if !present {
					continue // A companion operation already removed this member.
				}
				if err := raw.Delete(cleanupCtx, record); err != nil {
					t.Error("cleanup deletion failed", err)
				}
			} else {
				t.Errorf("unexpected outside record %s %s; left untouched", record.Hostname, record.Type)
			}
		}
		records, err = raw.List(cleanupCtx)
		save("after-cleanup.json", records)
		if err != nil || len(records) != 0 {
			t.Error("test record inventory did not return to baseline", err)
		}
	})
	// Probe transport at the page byte bound before invoking any state decoder.
	// This unreferenced value is removed immediately; it cannot grant ownership.
	probeValue := "dnsweaver-page=v1:" + strings.Repeat("a", provider.DefaultOwnershipPageBytes-len("dnsweaver-page=v1:"))
	probeDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(probeValue)))
	probe := provider.Record{Hostname: provider.OwnershipPageName(cfg.Zone, cfg.Owners[0], probeDigest), Type: provider.RecordTypeTXT, Target: probeValue, TTL: 300}
	if err := raw.Create(ctx, probe); err != nil {
		t.Fatal("page-bound TXT create failed", err)
	}
	probeRecords, err := raw.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	probeIndex := slices.IndexFunc(probeRecords, func(record provider.Record) bool {
		return record.Hostname == probe.Hostname && record.Target == probeValue
	})
	if probeIndex < 0 {
		t.Fatal("page-bound TXT did not round-trip")
	}
	if err := raw.Delete(ctx, probeRecords[probeIndex]); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d-byte ownership TXT transport round-trip passed", len(probeValue))
	// A user TXT value at the same name as a member marker must survive every
	// conversion and ownership withdrawal. It belongs to this test's namespace.
	note := provider.Record{Hostname: provider.OwnershipRecordName(liveAlias(cfg, cfg.Selected[0][0])), Type: provider.RecordTypeTXT, Target: "operator-qualification-note", TTL: 300}
	if err := raw.Create(ctx, note); err != nil {
		t.Fatal(err)
	}
	noteRecords, err := raw.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	noteIndex := slices.IndexFunc(noteRecords, func(record provider.Record) bool {
		return record.Hostname == note.Hostname && record.Type == provider.RecordTypeTXT
	})
	if noteIndex < 0 {
		t.Fatal("user sibling was not listed")
	}
	note = noteRecords[noteIndex]
	runActor := func(actor int, mode, phase string, limit int, dry, failure, withdraw bool) error {
		actorCfg := cfg
		actorCfg.Actor, actorCfg.Mode, actorCfg.Phase, actorCfg.Limit = actor, mode, phase, limit
		actorCfg.DryRun, actorCfg.SourceFailure, actorCfg.WithdrawShared = dry, failure, withdraw
		path := filepath.Join(cfg.Output, fmt.Sprintf("actor-%d-%s.json", actor, phase))
		data, err := json.Marshal(actorCfg)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnershipLiveActor$", "-test.v")
		command.Env = append(os.Environ(), "DNSWEAVER_LIVE_ACTOR=1", "DNSWEAVER_LIVE_CONFIG="+path)
		output, err := command.CombinedOutput()
		if writeErr := os.WriteFile(filepath.Join(cfg.Output, fmt.Sprintf("actor-%d-%s.log", actor, phase)), output, 0o600); writeErr != nil {
			return fmt.Errorf("save actor evidence: %w", writeErr)
		}
		if err != nil {
			return fmt.Errorf("actor %d %s failed: %s", actor, phase, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "LIVE_RESULT") {
				t.Log(line)
			}
		}
		return nil
	}
	parallel := func(mode, phase string, limit int) {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for actor := range 2 {
			wg.Go(func() { errs <- runActor(actor, mode, phase, limit, false, false, false) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	inspect := func(label string, claims, answers int, compact bool) []provider.Record {
		t.Helper()
		physical, err := raw.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(physical, func(record provider.Record) bool {
			return record.Hostname == note.Hostname && provider.SameRecordMember(record, note)
		}) {
			t.Fatal("user TXT sibling was lost")
		}
		save(label+"-physical.json", physical)
		logical, err := provider.ExpandOwnershipState(cfg.Zone, physical)
		if err != nil {
			t.Fatal(err)
		}
		claimCount, answerCount, stateCount, legacyCount := 0, 0, 0, 0
		var claimed []provider.Record
		for _, record := range logical {
			if provider.IsOwnershipRecord(record.Hostname) && provider.IsDnsweaverOwned(record.Target) {
				claimCount++
				_, _, member, _ := provider.ParseMemberOwnershipValue(record.Target)
				if member == nil {
					t.Fatal("live snapshot contains an ambiguous member claim")
				}
				member.Hostname = provider.ExtractHostnameFromOwnership(record.Hostname)
				claimed = append(claimed, *member)
			} else if record.Type == cfg.RecordType {
				answerCount++
			}
		}
		for _, record := range logical {
			if record.Type == cfg.RecordType && !slices.ContainsFunc(claimed, func(member provider.Record) bool {
				return member.Hostname == record.Hostname && provider.SameRecordMember(member, record)
			}) {
				t.Fatalf("unclaimed live answer: %s %s", record.Hostname, record.Type)
			}
		}
		for _, record := range physical {
			if provider.IsOwnershipStateRecord(record.Hostname) {
				stateCount++
			} else if provider.IsOwnershipRecord(record.Hostname) && provider.IsDnsweaverOwned(record.Target) {
				legacyCount++
			}
		}
		if claimCount != claims || answerCount != answers || compact && legacyCount != 0 {
			t.Fatalf("%s: claims=%d answers=%d state=%d legacy=%d, expected claims=%d answers=%d", label, claimCount, answerCount, stateCount, legacyCount, claims, answers)
		}
		t.Logf("%s: answers=%d claims=%d physical ownership TXT=%d", label, answerCount, claimCount, stateCount+legacyCount)
		return physical
	}
	for actor := range 2 {
		if err := runActor(actor, "legacy", "bootstrap", 6, false, false, false); err != nil {
			t.Fatal(err)
		}
	}
	inspect("legacy", 13, 12, false)
	before, err := raw.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := runActor(0, "compact", "dry-run", 6, true, false, false); err != nil {
		t.Fatal(err)
	}
	after, err := raw.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !liveRecordsEqual(before, after) {
		t.Fatal("dry-run changed records")
	}
	parallel("compact", "migrate", 6)
	inspect("migrated", 13, 12, true)
	parallel("compact", "grow", 8)
	inspect("active-active", 17, 16, true)
	for actor := range 2 {
		if err := runActor(actor, "compact", "restart", 8, false, false, false); err != nil {
			t.Fatal(err)
		}
	}
	before = inspect("restarted", 17, 16, true)
	if err := runActor(0, "compact", "source-interruption", 8, false, true, false); err != nil {
		t.Fatal(err)
	}
	after, err = raw.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !liveRecordsEqual(before, after) {
		t.Fatal("source interruption changed records")
	}
	if err := runActor(0, "compact", "withdraw-first", 8, false, false, true); err != nil {
		t.Fatal(err)
	}
	inspect("shared-retained", 16, 16, true)
	if err := runActor(1, "compact", "withdraw-last", 8, false, false, true); err != nil {
		t.Fatal(err)
	}
	inspect("last-claim-removed", 15, 15, true)
	for actor := range 2 {
		if err := runActor(actor, "legacy", "migrate", 8, false, false, true); err != nil {
			t.Fatal(err)
		}
	}
	inspect("reversed", 15, 15, false)
	if cfg.LegacyReader != "" {
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(cfg.Output, "legacy-reader.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(ctx, cfg.LegacyReader)
		command.Env = append(os.Environ(), "DNSWEAVER_LIVE_CONFIG="+path)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatal("actual old-source reader failed", err)
		}
		var result struct{ Claims, Answers int }
		if err := json.Unmarshal(output, &result); err != nil || result.Claims != 15 || result.Answers != 15 {
			t.Fatalf("old-source reader: claims=%d answers=%d error=%v", result.Claims, result.Answers, err)
		}
		t.Log("actual old-source reader recovered all 15 remaining claims and answers")
		save("legacy-reader-result.json", result)
	}
}

// TestOwnershipLiveWirePages complements live source reconciliation with mixed
// exact members and opaque metadata large enough to require multiple pages.
// It queries the configured authoritative servers over TCP, without DNS caches.
func TestOwnershipLiveWirePages(t *testing.T) {
	cfg := liveConfig(t)
	if len(cfg.NameServers) == 0 {
		t.Fatal("wire qualification requires explicit authoritative server addresses")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	raw, err := liveNative(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := raw.List(ctx)
	if err != nil || len(initial) != 0 {
		t.Fatal("wire qualification requires an empty managed-record inventory", err)
	}
	if err := os.MkdirAll(cfg.Output, 0o700); err != nil {
		t.Fatal(err)
	}
	save := func(name string, records []provider.Record) {
		t.Helper()
		data, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg.Output, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cleanupCancel()
		records, err := raw.List(cleanupCtx)
		if err != nil {
			t.Error("wire cleanup listing failed", err)
			return
		}
		save("before-cleanup.json", records)
		for _, record := range records {
			owned := strings.HasSuffix(record.Hostname, "."+cfg.Prefix+"."+cfg.Zone)
			for _, owner := range cfg.Owners {
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(owner)))[:16]
				owned = owned || record.Hostname == provider.OwnershipOwnerManifestName(cfg.Zone, owner) || strings.HasPrefix(record.Hostname, "_dw-p."+hash+".")
			}
			if !owned {
				t.Errorf("outside record %s left untouched", record.Hostname)
				continue
			}
			current, err := raw.List(cleanupCtx)
			if err != nil {
				t.Error(err)
				return
			}
			if slices.ContainsFunc(current, func(member provider.Record) bool {
				return member.Hostname == record.Hostname && provider.SameRecordMember(member, record)
			}) {
				if err := raw.Delete(cleanupCtx, record); err != nil {
					t.Error(err)
				}
			}
		}
		records, err = raw.List(cleanupCtx)
		save("after-cleanup.json", records)
		if err != nil || len(records) != 0 {
			t.Error("wire cleanup did not restore the managed-record baseline", err)
		}
	})
	var members, markers []provider.Record
	for i := range 16 {
		members = append(members, provider.Record{Hostname: fmt.Sprintf("wire-%02d.%s.%s", i, cfg.Prefix, cfg.Zone), Type: provider.RecordTypeA, Target: fmt.Sprintf("192.0.2.%d", i+1), TTL: 300})
	}
	target := members[0].Hostname
	members = append(members,
		provider.Record{Hostname: "wire-ipv6." + cfg.Prefix + "." + cfg.Zone, Type: provider.RecordTypeAAAA, Target: "2001:db8::1", TTL: 300},
		provider.Record{Hostname: "wire-alias." + cfg.Prefix + "." + cfg.Zone, Type: provider.RecordTypeCNAME, Target: target, TTL: 300},
		provider.Record{Hostname: "_sip._tcp." + cfg.Prefix + "." + cfg.Zone, Type: provider.RecordTypeSRV, Target: target, TTL: 300, SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 5060}},
	)
	for i, member := range members {
		if err := raw.Create(ctx, member); err != nil {
			t.Fatal(err)
		}
		var opaque strings.Builder
		for part := range 8 {
			fmt.Fprintf(&opaque, "%x", sha256.Sum256(fmt.Appendf(nil, "member-%d-part-%d", i, part)))
		}
		marker := provider.MemberOwnershipRecord(member.Hostname, 300, cfg.Owners[0], member,
			map[string]string{"note": fmt.Sprintf("member %d with spaces and \"quotes\"", i), "opaque": opaque.String()})
		markers = append(markers, marker)
		if err := raw.Create(ctx, marker); err != nil {
			t.Fatal(err)
		}
	}
	foreign := provider.MemberOwnershipRecord(members[0].Hostname, 300, cfg.Owners[1], members[0], nil)
	user := provider.Record{Hostname: markers[0].Hostname, Type: provider.RecordTypeTXT, Target: "operator-note", TTL: 300}
	for _, record := range []provider.Record{foreign, user} {
		if err := raw.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Actor, cfg.Mode = 0, "compact"
	registry, err := liveRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance, _ := registry.Get("live")
	if err := instance.Provider.(provider.OwnershipMigrator).MigrateOwnership(ctx); err != nil {
		t.Fatal(err)
	}
	physical, err := raw.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	save("compact-physical.json", physical)
	stateCount, largestPage := 0, 0
	for _, record := range physical {
		if provider.IsOwnershipStateRecord(record.Hostname) {
			stateCount++
			if strings.HasPrefix(record.Hostname, "_dw-p.") {
				largestPage = max(largestPage, len(record.Target))
			}
			liveAssertWire(ctx, t, cfg.NameServers, record)
		}
	}
	valueLimit := provider.DefaultOwnershipPageBytes
	if limiter, ok := raw.(provider.OwnershipTXTValueLimiter); ok {
		valueLimit = min(valueLimit, limiter.OwnershipTXTValueBytes())
	}
	if stateCount < 3 || stateCount >= len(markers) || largestPage < min(3000, valueLimit-300) || largestPage > valueLimit {
		t.Fatalf("compact TXT=%d largest page=%d; expected multiple pages near the byte bound", stateCount, largestPage)
	}
	for _, record := range append(slices.Clone(members), foreign, user) {
		liveAssertWire(ctx, t, cfg.NameServers, record)
	}
	// Reconstruct the snapshot from wire-verified state and require
	// each exact legacy member marker, including all opaque metadata.
	expanded, err := provider.ExpandOwnershipState(cfg.Zone, physical)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range append(markers, foreign, user) {
		if !slices.ContainsFunc(expanded, func(got provider.Record) bool {
			return got.Hostname == want.Hostname && provider.SameRecordMember(got, want)
		}) {
			t.Fatalf("member or metadata lost: %s", want.Hostname)
		}
	}
	t.Logf("LIVE_WIRE claims=19 compact_TXT=%d largest_page_bytes=%d authoritative_servers=%d mixed_types=A,AAAA,CNAME,SRV metadata=512bytes", stateCount, largestPage, len(cfg.NameServers))
}

func liveAssertWire(ctx context.Context, t *testing.T, servers []string, record provider.Record) {
	t.Helper()
	queryType := dns.StringToType[string(record.Type)]
	if queryType == 0 {
		t.Fatalf("unsupported wire check type %s", record.Type)
	}
	for _, server := range servers {
		query := new(dns.Msg)
		query.SetQuestion(dns.Fqdn(record.Hostname), queryType)
		query.RecursionDesired = false
		client := &dns.Client{Net: "tcp", Timeout: 5 * time.Second}
		deadline := time.Now().Add(2 * time.Minute)
		for {
			response, _, err := client.ExchangeContext(ctx, query, server)
			matched := err == nil && response.Authoritative && response.Rcode == dns.RcodeSuccess && slices.ContainsFunc(response.Answer, func(answer dns.RR) bool {
				if !strings.EqualFold(answer.Header().Name, dns.Fqdn(record.Hostname)) {
					return false
				}
				switch value := answer.(type) {
				case *dns.TXT:
					return strings.Join(value.Txt, "") == record.Target
				case *dns.A:
					return value.A.String() == record.Target
				case *dns.AAAA:
					return value.AAAA.String() == record.Target
				case *dns.CNAME:
					return strings.EqualFold(value.Target, dns.Fqdn(record.Target))
				case *dns.SRV:
					return record.SRV != nil && value.Priority == record.SRV.Priority && value.Weight == record.SRV.Weight && value.Port == record.SRV.Port && strings.EqualFold(value.Target, dns.Fqdn(record.Target))
				}
				return false
			})
			if matched {
				break
			}
			if ctx.Err() != nil || time.Now().After(deadline) {
				t.Fatalf("authoritative wire readback failed: server=%s name=%s type=%s", server, record.Hostname, record.Type)
			}
			time.Sleep(time.Second)
		}
	}
}

func liveRecordsEqual(a, b []provider.Record) bool {
	normalize := func(records []provider.Record) []byte {
		records = slices.Clone(records)
		for i := range records {
			records[i].ProviderID = ""
		}
		slices.SortFunc(records, func(a, b provider.Record) int {
			return strings.Compare(a.Hostname+string(a.Type)+a.Target, b.Hostname+string(b.Type)+b.Target)
		})
		data, _ := json.Marshal(records)
		return data
	}
	return bytes.Equal(normalize(a), normalize(b))
}

// Prepare a rejected publication's exact pages without contacting a provider.
func TestOwnershipPrepareProbe(t *testing.T) {
	cfg := liveConfig(t)
	if cfg.ProbeRecords == "" {
		t.Skip("no rejected snapshot configured")
	}
	data, err := os.ReadFile(cfg.ProbeRecords)
	if err != nil {
		t.Fatal(err)
	}
	var records []provider.Record
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	backend := testutil.NewMockProvider("offline-probe")
	backend.SetRecords(records...)
	compact, err := provider.NewCompactOwnershipProvider(backend, cfg.Owners[0], cfg.Zone, quietLogger(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := compact.MigrateOwnership(context.Background()); err != nil {
		t.Fatal(err)
	}
	physical, err := backend.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var pages []provider.Record
	for _, record := range physical {
		if strings.HasPrefix(record.Hostname, "_dw-p.") {
			pages = append(pages, record)
		}
	}
	data, err = json.MarshalIndent(pages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Output, "prepared-pages.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("prepared %d exact pages locally", len(pages))
}
