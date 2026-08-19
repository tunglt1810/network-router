package core

import (
	"errors"
	"sync"
	"testing"
	"time"

	"network-router/pkg/utils"

	"github.com/miekg/dns"
)

type recordedExchange struct {
	address string
	network string
}

// upstreamHarness records which upstreams were tried and in what order.
type upstreamHarness struct {
	mu        sync.Mutex
	attempts  []recordedExchange
	responses map[string]*dns.Msg
	failures  map[string]error
	truncated map[string]bool
}

func newHarness() *upstreamHarness {
	return &upstreamHarness{
		responses: make(map[string]*dns.Msg),
		failures:  make(map[string]error),
		truncated: make(map[string]bool),
	}
}

func (h *upstreamHarness) exchange(msg *dns.Msg, address string, network string, timeout time.Duration) (*dns.Msg, error) {
	h.mu.Lock()
	h.attempts = append(h.attempts, recordedExchange{address: address, network: network})
	failure := h.failures[address]
	reply, ok := h.responses[address]
	truncate := h.truncated[address] && network == "udp"
	h.mu.Unlock()

	if failure != nil {
		return nil, failure
	}
	if !ok {
		return nil, errors.New("no configured response")
	}

	result := reply.Copy()
	result.SetReply(msg)
	result.Rcode = reply.Rcode
	result.Answer = reply.Answer
	result.Truncated = truncate
	return result, nil
}

func (h *upstreamHarness) tried() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	addresses := make([]string, 0, len(h.attempts))
	for _, attempt := range h.attempts {
		addresses = append(addresses, attempt.address)
	}
	return addresses
}

func answerMsg(rcode int, ip string) *dns.Msg {
	msg := new(dns.Msg)
	msg.Rcode = rcode
	if ip != "" {
		rr, err := dns.NewRR("example.com. 300 IN A " + ip)
		if err != nil {
			panic(err)
		}
		msg.Answer = []dns.RR{rr}
	}
	return msg
}

func queryFor(name string, qtype uint16) *dns.Msg {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), qtype)
	return msg
}

func newTestUpstream(t *testing.T, config *Config, snapshot *NetworkSnapshot, harness *upstreamHarness, phoneDNS []string) *UpstreamResolver {
	t.Helper()

	snapshots := NewSnapshotStore()
	if snapshot != nil {
		snapshots.Store(snapshot)
	}
	resolver := NewUpstreamResolver(config, snapshots)
	resolver.exchange = harness.exchange
	resolver.lookupDNS = func(device string) []string { return phoneDNS }
	return resolver
}

// Name resolution must travel the same link the traffic will.
func TestResolvePrefersTheTetheredResolver(t *testing.T) {
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.5")

	resolver := newTestUpstream(t,
		&Config{DNSUpstreamPinned: []string{"1.1.1.1:53"}},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	reply, routable, err := resolver.Resolve(queryFor("example.com", dns.TypeA))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !routable {
		t.Fatal("an answer from the tethered path must be routable")
	}
	if len(reply.Answer) != 1 {
		t.Fatalf("expected one answer, got %#v", reply.Answer)
	}
	if tried := harness.tried(); len(tried) != 1 || tried[0] != "172.20.10.1:53" {
		t.Fatalf("expected only the tethered resolver to be tried, got %v", tried)
	}
}

func TestResolveFallsBackToPinnedResolversOverTheTetheredPath(t *testing.T) {
	harness := newHarness()
	harness.failures["172.20.10.1:53"] = errors.New("i/o timeout")
	harness.responses["1.1.1.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.6")

	resolver := newTestUpstream(t,
		&Config{DNSUpstreamPinned: []string{"1.1.1.1:53", "8.8.8.8:53"}},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	_, routable, err := resolver.Resolve(queryFor("example.com", dns.TypeA))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !routable {
		t.Fatal("a pinned resolver is on the tethered path, so its answer is routable")
	}
	if !containsString(harness.tried(), "1.1.1.1:53") {
		t.Fatalf("expected the pinned resolver to be tried, got %v", harness.tried())
	}
}

// A successful exchange is not an answer: SERVFAIL must advance to the next
// candidate rather than reach the client.
func TestResolveTreatsServfailAsUpstreamFailure(t *testing.T) {
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeServerFailure, "")
	harness.responses["1.1.1.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.7")

	resolver := newTestUpstream(t,
		&Config{DNSUpstreamPinned: []string{"1.1.1.1:53"}},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	reply, _, err := resolver.Resolve(queryFor("example.com", dns.TypeA))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if reply.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected the working upstream's answer, got %s", dns.RcodeToString[reply.Rcode])
	}
}

func TestResolveAcceptsNXDomainAsDefinitive(t *testing.T) {
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeNameError, "")
	harness.responses["1.1.1.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.8")

	resolver := newTestUpstream(t,
		&Config{DNSUpstreamPinned: []string{"1.1.1.1:53"}},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	reply, _, err := resolver.Resolve(queryFor("example.com", dns.TypeA))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if reply.Rcode != dns.RcodeNameError {
		t.Fatalf("NXDOMAIN is an answer, not a failure; got %s", dns.RcodeToString[reply.Rcode])
	}
	if containsString(harness.tried(), "1.1.1.1:53") {
		t.Fatal("a definitive NXDOMAIN must not trigger another upstream")
	}
}

// When nothing on the tethered path answers, the caller must never be told the
// result is routable, otherwise routes would be derived from the bypassed network.
func TestResolveNeverReportsRoutableWhenTheTetheredPathFails(t *testing.T) {
	harness := newHarness()
	harness.failures["172.20.10.1:53"] = errors.New("i/o timeout")
	harness.failures["1.1.1.1:53"] = errors.New("i/o timeout")

	resolver := newTestUpstream(t,
		&Config{DNSUpstreamPinned: []string{"1.1.1.1:53"}},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	_, routable, err := resolver.Resolve(queryFor("example.com", dns.TypeA))
	if routable {
		t.Fatal("an answer that did not come from the tethered path must not be routable")
	}
	// Either a degraded answer from the host resolver or an error is acceptable;
	// what matters is that routable is false.
	_ = err
}

// A degraded answer beats SERVFAIL, because /etc/resolver leaves the client with
// nowhere else to ask.
func TestTryReturnsAnswerFromDegradedUpstream(t *testing.T) {
	harness := newHarness()
	harness.responses["10.0.12.2:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.9")

	resolver := newTestUpstream(t, &Config{}, nil, harness, nil)

	reply, err := resolver.try(queryFor("example.com", dns.TypeA), "10.0.12.2:53", time.Now().Add(time.Second), 1)
	if err != nil {
		t.Fatalf("degraded exchange: %v", err)
	}
	if reply.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected an answer, got %s", dns.RcodeToString[reply.Rcode])
	}
}

func TestResolveRetriesTruncatedAnswersOverTCP(t *testing.T) {
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.10")
	harness.truncated["172.20.10.1:53"] = true

	resolver := newTestUpstream(t,
		&Config{},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	if _, _, err := resolver.Resolve(queryFor("example.com", dns.TypeA)); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	var sawTCP bool
	for _, attempt := range harness.attempts {
		if attempt.network == "tcp" {
			sawTCP = true
		}
	}
	if !sawTCP {
		t.Fatal("expected a TCP retry after a truncated reply")
	}
}

func TestResolveHonoursExplicitUpstreamOverride(t *testing.T) {
	harness := newHarness()
	harness.responses["9.9.9.9:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.11")

	resolver := newTestUpstream(t,
		&Config{DNSUpstream: "9.9.9.9"},
		&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")},
		harness,
		[]string{"172.20.10.1"},
	)

	if _, _, err := resolver.Resolve(queryFor("example.com", dns.TypeA)); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tried := harness.tried(); len(tried) != 1 || tried[0] != "9.9.9.9:53" {
		t.Fatalf("expected only the override to be used, got %v", tried)
	}
}

func TestPinnedUpstreamHostsExtractsBareIPs(t *testing.T) {
	hosts := PinnedUpstreamHosts(&Config{DNSUpstreamPinned: []string{"1.1.1.1:53", "8.8.8.8", "not-an-ip:53"}})
	if len(hosts) != 2 || hosts[0] != "1.1.1.1" || hosts[1] != "8.8.8.8" {
		t.Fatalf("unexpected pinned hosts: %#v", hosts)
	}
}

func TestWithDefaultPort(t *testing.T) {
	cases := map[string]string{
		"1.1.1.1":            "1.1.1.1:53",
		"1.1.1.1:5353":       "1.1.1.1:5353",
		"2606:4700::1111":    "[2606:4700::1111]:53",
		"[2606:4700::1]:853": "[2606:4700::1]:853",
	}
	for input, want := range cases {
		if got := withDefaultPort(input); got != want {
			t.Errorf("withDefaultPort(%q) = %q, want %q", input, got, want)
		}
	}
}

func deviceInfo(device string) *utils.InterfaceInfo {
	return &utils.InterfaceInfo{Name: "iPhone USB", DeviceName: device}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
