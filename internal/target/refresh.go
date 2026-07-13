package target

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultRefreshInterval is the default cadence for re-resolving a dynamic
// target when the provider instance does not set an explicit interval.
const DefaultRefreshInterval = 5 * time.Minute

// Refresher periodically resolves a dynamic target and applies changes via a
// callback. It keeps the last known-good value: if a resolution fails, the
// previous value is retained and a warning is logged, so a transient outage of
// an echo endpoint does not churn DNS.
type Refresher struct {
	resolver Resolver
	interval time.Duration
	// onChange is called with a newly resolved value whenever it differs from
	// the last applied value. It is also called once at Start with the initial
	// resolution (if successful).
	onChange func(string)
	logger   *slog.Logger

	mu       sync.Mutex
	lastGood string
	cancel   context.CancelFunc
	running  bool
}

// RefresherConfig configures a Refresher.
type RefresherConfig struct {
	// Resolver determines the target value. Required.
	Resolver Resolver
	// Interval is the refresh cadence. Defaults to DefaultRefreshInterval if
	// zero or negative.
	Interval time.Duration
	// OnChange is invoked with each newly resolved value that differs from the
	// previous one (and once with the initial value). Required.
	OnChange func(string)
	// Logger defaults to slog.Default() if nil.
	Logger *slog.Logger
}

// NewRefresher creates a Refresher from cfg.
func NewRefresher(cfg RefresherConfig) *Refresher {
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Refresher{
		resolver: cfg.Resolver,
		interval: interval,
		onChange: cfg.OnChange,
		logger:   logger,
	}
}

// Start performs an initial resolution and then refreshes on the configured
// interval until Stop is called or ctx is canceled. It is non-blocking. The
// initial resolution is synchronous so the caller can rely on the target being
// set (or the fallback logged) before the first reconcile. Returns the initial
// resolved value, or "" if the initial resolution failed (the caller's existing
// fallback remains in effect).
func (r *Refresher) Start(ctx context.Context) string {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return r.lastGood
	}
	ctx, r.cancel = context.WithCancel(ctx)
	r.running = true
	r.mu.Unlock()

	initial := r.resolveOnce(ctx, "initial")

	go r.loop(ctx)

	r.logger.Info("target refresher started",
		slog.String("resolver", r.resolver.Describe()),
		slog.Duration("interval", r.interval),
		slog.String("initial_target", initial),
	)
	return initial
}

// Stop halts the refresher.
func (r *Refresher) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.running = false
}

func (r *Refresher) loop(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.resolveOnce(ctx, "refresh")
		}
	}
}

// resolveOnce resolves the target once. On success, if the value changed it
// invokes onChange and records the new last known-good value. On failure it
// keeps the previous value and logs a warning. Returns the current last
// known-good value.
func (r *Refresher) resolveOnce(ctx context.Context, reason string) string {
	value, err := r.resolver.Resolve(ctx)
	if err != nil {
		r.mu.Lock()
		last := r.lastGood
		r.mu.Unlock()
		r.logger.Warn("target resolution failed; keeping last known-good value",
			slog.String("resolver", r.resolver.Describe()),
			slog.String("reason", reason),
			slog.String("last_good", last),
			slog.String("error", err.Error()),
		)
		return last
	}

	r.mu.Lock()
	changed := value != r.lastGood
	r.lastGood = value
	r.mu.Unlock()

	if changed {
		r.logger.Info("target resolved",
			slog.String("resolver", r.resolver.Describe()),
			slog.String("reason", reason),
			slog.String("target", value),
		)
		if r.onChange != nil {
			r.onChange(value)
		}
	}
	return value
}
