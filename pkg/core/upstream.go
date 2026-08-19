package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"network-router/pkg/utils"

	"github.com/miekg/dns"
)

const (
	// upstreamBudget stays comfortably under the 5s timeout macOS applies to a
	// resolver listed in /etc/resolver, so the proxy answers rather than being
	// abandoned mid-query.
	upstreamBudget = 3500 * time.Millisecond
	upstreamPerTry = 1500 * time.Millisecond
	// upstreamStagger delays each additional candidate so the common case costs
	// exactly one upstream query.
	upstreamStagger = 250 * time.Millisecond
)

// ErrNoUpstream reports that no upstream produced a definitive answer.
var ErrNoUpstream = errors.New("no upstream produced an answer")

// exchangeFunc performs a single upstream exchange. Injectable for tests.
type exchangeFunc func(msg *dns.Msg, address string, network string, timeout time.Duration) (*dns.Msg, error)

// UpstreamResolver decides which resolver answers a query.
//
// A configured domain is routed over the tethered link because that is the
// desired path for it, and often the only one that can reach it. Name resolution
// therefore has to travel the same link. Resolving through /etc/resolv.conf would
// ask the other network for names it may not be able to answer for, and since
// /etc/resolver/<domain> makes this proxy the sole resolver for those names, the
// client has nowhere else to turn when that fails.
//
// Order of preference:
//  1. the DNS server advertised on the tethered interface
//  2. pinned public resolvers, which the applier routes over the tethered path
//  3. the system resolver, as a degraded last resort whose answers are marked
//     unroutable so no route is derived from an address another network supplied
type UpstreamResolver struct {
	config    *Config
	snapshots *SnapshotStore
	exchange  exchangeFunc
	lookupDNS func(device string) []string

	mu            sync.Mutex
	cachedDevice  string
	cachedServers []string
}

func NewUpstreamResolver(config *Config, snapshots *SnapshotStore) *UpstreamResolver {
	return &UpstreamResolver{
		config:    config,
		snapshots: snapshots,
		exchange:  exchangeWith,
		lookupDNS: utils.GetInterfaceDNSServers,
	}
}

// Resolve answers the request. The boolean reports whether the answer came from
// the tethered path and may therefore be used to install routes.
func (r *UpstreamResolver) Resolve(request *dns.Msg) (*dns.Msg, bool, error) {
	deadline := time.Now().Add(upstreamBudget)

	if override := strings.TrimSpace(r.config.DNSUpstream); override != "" {
		reply, err := r.try(request, withDefaultPort(override), deadline, 1)
		if err != nil {
			return nil, false, err
		}
		return reply, true, nil
	}

	reply, lastErr := r.race(request, r.tetheredUpstreams(), deadline)
	if lastErr == nil {
		return reply, true, nil
	}

	// Degraded path. Returning a usable answer beats SERVFAIL, because the
	// resolver file leaves the client with nowhere else to ask.
	for _, address := range systemUpstreams() {
		reply, err := r.try(request, address, deadline, 1)
		if err == nil {
			log.Printf("⚠️ Answered %s from the system resolver; routes were not installed for it", questionName(request))
			return reply, false, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = ErrNoUpstream
	}
	return nil, false, lastErr
}

// tetheredUpstreams lists resolvers reachable over the phone path.
func (r *UpstreamResolver) tetheredUpstreams() []string {
	var upstreams []string

	snapshot := r.snapshots.Load()
	if snapshot != nil && snapshot.PhoneReady {
		for _, server := range r.phoneServers(snapshot.PhoneDevice()) {
			upstreams = append(upstreams, withDefaultPort(server))
		}
		// The tethering gateway itself is normally the resolver, so it is a
		// sensible candidate when DHCP did not advertise one.
		if len(upstreams) == 0 && snapshot.PhoneGateway != "" {
			upstreams = append(upstreams, withDefaultPort(snapshot.PhoneGateway))
		}
	}

	for _, pinned := range r.config.PinnedUpstreams() {
		upstreams = append(upstreams, withDefaultPort(pinned))
	}

	return dedupeStrings(upstreams)
}

func (r *UpstreamResolver) phoneServers(device string) []string {
	if device == "" || r.lookupDNS == nil {
		return nil
	}

	r.mu.Lock()
	if r.cachedDevice == device {
		servers := r.cachedServers
		r.mu.Unlock()
		return servers
	}
	r.mu.Unlock()

	servers := r.lookupDNS(device)

	r.mu.Lock()
	r.cachedDevice = device
	r.cachedServers = servers
	r.mu.Unlock()

	return servers
}

type attemptResult struct {
	reply *dns.Msg
	err   error
}

// race queries the tethered upstreams with a staggered start and takes the first
// definitive answer.
//
// The stagger keeps the common case at a single query, because the phone resolver
// normally answers long before the next candidate is due to fire. It also bounds
// the cost of an unreachable candidate at one stagger interval rather than a full
// attempt timeout, which matters because a pinned public resolver is a black hole
// until its host route is in place.
func (r *UpstreamResolver) race(request *dns.Msg, upstreams []string, deadline time.Time) (*dns.Msg, error) {
	if len(upstreams) == 0 {
		return nil, ErrNoUpstream
	}

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	results := make(chan attemptResult, len(upstreams))
	for index, address := range upstreams {
		go func(index int, address string) {
			if index > 0 {
				select {
				case <-ctx.Done():
					results <- attemptResult{err: ctx.Err()}
					return
				case <-time.After(time.Duration(index) * upstreamStagger):
				}
			}
			reply, err := r.try(request, address, deadline, 1)
			results <- attemptResult{reply: reply, err: err}
		}(index, address)
	}

	var lastErr error
	for range upstreams {
		select {
		case result := <-results:
			if result.err == nil {
				return result.reply, nil
			}
			lastErr = result.err
		case <-ctx.Done():
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			return nil, lastErr
		}
	}

	if lastErr == nil {
		lastErr = ErrNoUpstream
	}
	return nil, lastErr
}

// try performs up to attempts exchanges against one upstream, and retries over
// TCP when the answer is truncated.
func (r *UpstreamResolver) try(request *dns.Msg, address string, deadline time.Time, attempts int) (*dns.Msg, error) {
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		timeout := timeUntil(deadline)
		if timeout <= 0 {
			break
		}
		if timeout > upstreamPerTry {
			timeout = upstreamPerTry
		}

		reply, err := r.exchange(request, address, "udp", timeout)
		if err != nil {
			lastErr = fmt.Errorf("upstream %s: %w", address, err)
			continue
		}

		// macOS retries over TCP when a reply is truncated, so do the same here
		// rather than handing back a partial answer.

		if reply.Truncated {
			if tcpReply, tcpErr := r.exchange(request, address, "tcp", timeUntil(deadline)); tcpErr == nil {
				reply = tcpReply
			}
		}

		if !isDefinitive(reply.Rcode) {
			lastErr = fmt.Errorf("upstream %s returned %s", address, dns.RcodeToString[reply.Rcode])
			continue
		}
		return reply, nil
	}

	if lastErr == nil {
		lastErr = ErrNoUpstream
	}
	return nil, lastErr
}

// isDefinitive distinguishes a real answer from an upstream failure. A
// successful exchange is not the same as an answer: SERVFAIL and friends mean the
// upstream could not answer, and the next candidate should be tried.
func isDefinitive(rcode int) bool {
	return rcode == dns.RcodeSuccess || rcode == dns.RcodeNameError
}

func exchangeWith(msg *dns.Msg, address string, network string, timeout time.Duration) (*dns.Msg, error) {
	if timeout <= 0 {
		return nil, errors.New("no time left in the upstream budget")
	}
	client := &dns.Client{Net: network, Timeout: timeout}
	reply, _, err := client.Exchange(msg, address)
	if err != nil {
		return nil, err
	}
	return reply, nil
}

// systemUpstreams reads /etc/resolv.conf for the degraded path only.
func systemUpstreams() []string {
	config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(config.Servers) == 0 {
		return nil
	}

	port := config.Port
	if port == "" {
		port = "53"
	}

	servers := make([]string, 0, len(config.Servers))
	for _, server := range config.Servers {
		// JoinHostPort brackets IPv6 addresses, which plain concatenation would
		// leave unusable.
		servers = append(servers, net.JoinHostPort(server, port))
	}
	return servers
}

func withDefaultPort(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(address, "53")
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func timeUntil(deadline time.Time) time.Duration {
	return time.Until(deadline)
}

func questionName(msg *dns.Msg) string {
	if len(msg.Question) == 0 {
		return "(no question)"
	}
	return strings.TrimSuffix(msg.Question[0].Name, ".")
}

// PinnedUpstreamHosts returns the bare IPs of pinned resolvers so they can be
// given a host route over the tethered path.
func PinnedUpstreamHosts(config *Config) []string {
	var hosts []string
	for _, upstream := range config.PinnedUpstreams() {
		host := upstream
		if parsedHost, _, err := net.SplitHostPort(upstream); err == nil {
			host = parsedHost
		}
		if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil && ip.To4() != nil {
			hosts = append(hosts, ip.String())
		}
	}
	return dedupeStrings(hosts)
}
