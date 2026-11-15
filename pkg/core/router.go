package core

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"network-router/pkg/utils"

	"golang.org/x/sync/errgroup"
)

const (
	resolveConcurrency = 8
	resolveAttempts    = 2
	resolveBackoff     = 500 * time.Millisecond
)

// Router turns configuration into a set of routes over the tethered path.
//
// It holds no mutable network state of its own: the coordinator passes the
// snapshot to apply against, and installed routes live in the RouteApplier.
type Router struct {
	config  *Config
	applier *RouteApplier
}

func NewRouter(config *Config, applier *RouteApplier) *Router {
	return &Router{config: config, applier: applier}
}

// ResolveDomains resolves the configured domains that are not wildcards.
//
// Wildcard patterns are skipped on purpose. Resolving the base of
// `*.googlevideo.com` yields an address that no real subdomain uses, so the
// resulting route was never the one traffic needed. Wildcards are handled at
// runtime by the DNS proxy, which sees the actual name being queried.
func (r *Router) ResolveDomains() []string {
	targets := make([]string, 0, len(r.config.TetherDomains))
	seen := make(map[string]bool)

	for _, domain := range r.config.TetherDomains {
		name := strings.ToLower(strings.TrimSpace(domain))
		if name == "" || strings.HasPrefix(name, "*.") || seen[name] {
			continue
		}
		seen[name] = true
		targets = append(targets, name)
	}

	if len(targets) == 0 {
		return nil
	}

	log.Printf("Resolving %d domain(s)...", len(targets))

	var mu sync.Mutex
	var resolved []string
	var failed []string

	group, ctx := errgroup.WithContext(context.Background())
	group.SetLimit(resolveConcurrency)

	for _, target := range targets {
		target := target
		group.Go(func() error {
			ips, err := resolveWithRetry(ctx, target)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, target)
				return nil
			}
			resolved = append(resolved, ips...)
			return nil
		})
	}
	_ = group.Wait()

	log.Printf("📊 Resolved %d/%d domain(s); %d produced no address", len(targets)-len(failed), len(targets), len(failed))
	if len(failed) > 0 && len(failed) <= 8 {
		log.Printf("   Unresolved: %v", failed)
	}

	return dedupeStrings(resolved)
}

func resolveWithRetry(ctx context.Context, domain string) ([]string, error) {
	var lastErr error
	for attempt := 0; attempt < resolveAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(resolveBackoff):
			}
		}
		ips, err := utils.ResolveDomainToIPs(domain)
		if err == nil {
			return ips, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// ApplyRoutes installs the configured routes over the tethered path.
//
// The system default route is never touched. Only host and network routes for
// the configured destinations are installed.
func (r *Router) ApplyRoutes(snapshot *NetworkSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("%w: network has not been discovered yet", ErrPhoneNotReady)
	}
	if !snapshot.PhoneReady {
		return fmt.Errorf("%w: %s", ErrPhoneNotReady, snapshot.PhoneReason)
	}

	destinations := make([]string, 0, len(r.config.TetherCIDRs))
	destinations = append(destinations, r.config.TetherCIDRs...)

	// Pin the fallback resolvers to the tethered path. Without this the proxy
	// would reach them over the default route, which defeats the bypass.
	infraHosts := PinnedUpstreamHosts(r.config)

	resolved := r.ResolveDomains()

	if len(destinations)+len(resolved)+len(infraHosts) == 0 {
		return fmt.Errorf("no routes to apply: check tether_cidrs and tether_domains")
	}

	log.Printf("📋 Applying %d CIDR(s), %d resolved host(s), %d resolver host(s)",
		len(destinations), len(resolved), len(infraHosts))

	failures := r.applier.AddMany(infraHosts)
	failures = append(failures, r.applier.AddMany(destinations)...)
	failures = append(failures, r.applier.AddMany(resolved)...)

	// A single rejected destination is not a reason to tear everything down.
	// Rolling the whole batch back would let one problematic entry deny every
	// other route, and the five-second retry loop would never make progress.
	if len(failures) > 0 {
		log.Printf("⚠️ %d route(s) could not be installed:", len(failures))
		for _, failure := range failures {
			log.Printf("   %v", failure)
		}
	}

	if r.applier.Count() == 0 {
		return fmt.Errorf("no routes could be installed: %v", failures)
	}

	log.Printf("✓ %d route(s) active via %s", r.applier.Count(), snapshot.PhoneGateway)
	return nil
}
