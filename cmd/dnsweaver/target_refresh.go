package main

import (
	"context"
	"log/slog"

	"github.com/maxfield-allison/dnsweaver/internal/config"
	"github.com/maxfield-allison/dnsweaver/internal/target"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// buildTargetRefreshers creates a target.Refresher for every provider instance
// that configures DNSWEAVER_{NAME}_TARGET_MODE. Each refresher resolves the
// instance's target on an interval and applies changes via SetDynamicTarget,
// triggering a reconcile when the value changes. Instances without a target
// mode are left untouched (they use their literal TARGET).
//
// The returned refreshers are not started; the caller starts them once the
// reconcile trigger is wired, and stops them during shutdown.
func buildTargetRefreshers(
	cfg *config.Config,
	registry *provider.Registry,
	triggerReconcile func(),
	logger *slog.Logger,
) []*target.Refresher {
	var refreshers []*target.Refresher

	for _, instCfg := range cfg.ProviderInstances {
		if instCfg.TargetMode == "" {
			continue
		}

		inst, ok := registry.Get(instCfg.Name)
		if !ok {
			// Provider isn't registered (e.g. still pending). Dynamic targets
			// pair with DNS providers that register synchronously; if a provider
			// is genuinely absent there is nothing to update, so skip it.
			logger.Warn("target mode set for unknown provider instance; skipping dynamic target",
				slog.String("instance", instCfg.Name),
				slog.String("target_mode", instCfg.TargetMode),
			)
			continue
		}

		family := target.FamilyIPv4
		if inst.RecordType == provider.RecordTypeAAAA {
			family = target.FamilyIPv6
		}

		resolver, err := target.Parse(instCfg.TargetMode, family)
		if err != nil {
			// Validation already rejects bad modes; guard defensively.
			logger.Error("invalid target mode; skipping dynamic target",
				slog.String("instance", instCfg.Name),
				slog.String("target_mode", instCfg.TargetMode),
				slog.String("error", err.Error()),
			)
			continue
		}
		// Apply a custom public-endpoint list if configured (only affects the
		// public resolver).
		if len(instCfg.TargetPublicEndpoints) > 0 {
			if _, isPublic := resolver.(*target.PublicResolver); isPublic {
				resolver = target.NewPublicResolver(family, instCfg.TargetPublicEndpoints)
			}
		}

		refresher := target.NewRefresher(target.RefresherConfig{
			Resolver: resolver,
			Interval: instCfg.TargetRefreshInterval,
			OnChange: func(value string) {
				inst.SetDynamicTarget(value)
				triggerReconcile()
			},
			Logger: logger.With(slog.String("instance", instCfg.Name)),
		})
		refreshers = append(refreshers, refresher)
	}

	return refreshers
}

// startTargetRefreshers starts each refresher. The initial resolution runs
// synchronously inside Start so the target is set before the first reconcile.
func startTargetRefreshers(ctx context.Context, refreshers []*target.Refresher) {
	for _, r := range refreshers {
		r.Start(ctx)
	}
}

// stopTargetRefreshers stops each refresher.
func stopTargetRefreshers(refreshers []*target.Refresher) {
	for _, r := range refreshers {
		r.Stop()
	}
}
