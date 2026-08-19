package core

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func redirectResolverDir(t *testing.T) string {
	t.Helper()
	original := resolverDir
	directory := t.TempDir()
	resolverDir = directory
	t.Cleanup(func() { resolverDir = original })
	return directory
}

// Wildcard and bare forms of the same domain name a single resolver file, and the
// created-file bookkeeping must record it once.
func TestResolverFileNamesAreDeduplicated(t *testing.T) {
	proxy := NewDNSProxy(&Config{
		TetherDomains: []string{
			"googleapis.com",
			"*.googleapis.com",
			"line.me",
			"*.line.me",
			"  APPLE.COM  ",
		},
	}, nil, nil)

	names := proxy.resolverFileNames()
	if len(names) != 3 {
		t.Fatalf("expected three resolver files, got %v", names)
	}
	for _, expected := range []string{"apple.com", "googleapis.com", "line.me"} {
		if !contains(names, expected) {
			t.Fatalf("expected %s in %v", expected, names)
		}
	}
}

// A resolver file named after an IP literal has no effect; such entries belong
// in tether_cidrs.
func TestResolverFileNamesSkipIPLiterals(t *testing.T) {
	proxy := NewDNSProxy(&Config{
		TetherDomains: []string{"34.2.135.176", "94.136.186.73", "example.com"},
	}, nil, nil)

	names := proxy.resolverFileNames()
	if len(names) != 1 || names[0] != "example.com" {
		t.Fatalf("expected only the real domain, got %v", names)
	}
}

func TestStartIsIdempotentAndStopIsToo(t *testing.T) {
	redirectResolverDir(t)
	proxy := newLoopbackProxy(t)

	if err := proxy.Start(); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := proxy.Start(); err != nil {
		t.Fatalf("second start must be a no-op: %v", err)
	}
	if err := proxy.Stop(); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := proxy.Stop(); err != nil {
		t.Fatalf("second stop must be a no-op: %v", err)
	}
}

// Resolver files must never advertise a port this process is not serving.
func TestStartFailsWithoutWritingResolverFilesWhenThePortIsTaken(t *testing.T) {
	directory := redirectResolverDir(t)

	occupied, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port

	proxy := NewDNSProxy(&Config{
		DNSProxyEnabled: true,
		DNSProxyPort:    port,
		TetherDomains:   []string{"example.com"},
	}, nil, nil)

	if err := proxy.Start(); err == nil {
		_ = proxy.Stop()
		t.Fatal("expected the bind conflict to be reported")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read resolver dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("no resolver file may be written when the listener failed, found %d", len(entries))
	}
}

func TestStartDoesNothingWhenDisabled(t *testing.T) {
	redirectResolverDir(t)
	proxy := NewDNSProxy(&Config{DNSProxyEnabled: false}, nil, nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("a disabled proxy must start cleanly: %v", err)
	}
	if proxy.running {
		t.Fatal("a disabled proxy must not be running")
	}
}

func TestReconcileResolversRemovesOnlyOurFiles(t *testing.T) {
	directory := redirectResolverDir(t)

	ours := filepath.Join(directory, "ours.example.com")
	theirs := filepath.Join(directory, "theirs.example.com")
	if err := os.WriteFile(ours, []byte(resolverMarker+"\nnameserver 127.0.0.1\nport 5454\n"), 0o644); err != nil {
		t.Fatalf("write ours: %v", err)
	}
	if err := os.WriteFile(theirs, []byte("nameserver 10.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write theirs: %v", err)
	}

	if err := ReconcileResolvers(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, err := os.Stat(ours); !os.IsNotExist(err) {
		t.Fatalf("expected our orphaned file to be removed, stat error: %v", err)
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Fatalf("a file we did not generate must be left alone: %v", err)
	}
}

func TestIsIPv6Preferring(t *testing.T) {
	for _, qtype := range []uint16{dns.TypeAAAA, dns.TypeHTTPS, dns.TypeSVCB} {
		if !isIPv6Preferring(qtype) {
			t.Errorf("%s should be treated as IPv6-preferring", dns.TypeToString[qtype])
		}
	}
	for _, qtype := range []uint16{dns.TypeA, dns.TypeCNAME, dns.TypeMX, dns.TypeTXT} {
		if isIPv6Preferring(qtype) {
			t.Errorf("%s should not be treated as IPv6-preferring", dns.TypeToString[qtype])
		}
	}
}

// AAAA, HTTPS and SVCB let a client reach a host without using the A records the
// applier installs routes for, so they are answered empty.
func TestForceIPv4AnswersIPv6PreferringTypesEmpty(t *testing.T) {
	proxy := NewDNSProxy(&Config{DNSProxyEnabled: true}, nil, nil)

	for _, qtype := range []uint16{dns.TypeAAAA, dns.TypeHTTPS, dns.TypeSVCB} {
		writer := &captureWriter{}
		proxy.handleDNSRequest(writer, queryFor("example.com", qtype))

		if writer.msg == nil {
			t.Fatalf("%s: expected a reply", dns.TypeToString[qtype])
		}
		if writer.msg.Rcode != dns.RcodeSuccess {
			t.Fatalf("%s: expected NOERROR, got %s", dns.TypeToString[qtype], dns.RcodeToString[writer.msg.Rcode])
		}
		if len(writer.msg.Answer) != 0 {
			t.Fatalf("%s: expected an empty answer, got %#v", dns.TypeToString[qtype], writer.msg.Answer)
		}
	}
}

func TestForceIPv4CanBeDisabled(t *testing.T) {
	disabled := false
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.1")

	config := &Config{DNSProxyEnabled: true, ForceIPv4: &disabled}
	snapshots := NewSnapshotStore()
	snapshots.Store(&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")})

	upstream := NewUpstreamResolver(config, snapshots)
	upstream.exchange = harness.exchange
	upstream.lookupDNS = func(string) []string { return []string{"172.20.10.1"} }

	proxy := NewDNSProxy(config, upstream, nil)
	writer := &captureWriter{}
	proxy.handleDNSRequest(writer, queryFor("example.com", dns.TypeAAAA))

	if len(harness.tried()) == 0 {
		t.Fatal("with force_ipv4 disabled the query must reach an upstream")
	}
}

func TestHandleRequestSubmitsResolvedHostsForRouting(t *testing.T) {
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.77")

	config := &Config{DNSProxyEnabled: true}
	snapshots := NewSnapshotStore()
	snapshots.Store(&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")})

	manager := NewMockRouteManager()
	applier := NewRouteApplier(manager, snapshots, filepath.Join(t.TempDir(), "state.yaml"))
	applier.Start()
	defer applier.Stop()

	upstream := NewUpstreamResolver(config, snapshots)
	upstream.exchange = harness.exchange
	upstream.lookupDNS = func(string) []string { return []string{"172.20.10.1"} }

	proxy := NewDNSProxy(config, upstream, applier)
	writer := &captureWriter{}
	proxy.handleDNSRequest(writer, queryFor("example.com", dns.TypeA))

	if writer.msg == nil || len(writer.msg.Answer) != 1 {
		t.Fatalf("expected the answer to be returned, got %#v", writer.msg)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && applier.Count() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if !contains(manager.routes(), "203.0.113.77/32") {
		t.Fatalf("expected a host route for the answer, got %v", manager.routes())
	}
}

// A second identical query must be served from cache without touching upstream,
// which is also what keeps route work off the repeat path.
func TestHandleRequestServesRepeatQueriesFromCache(t *testing.T) {
	harness := newHarness()
	harness.responses["172.20.10.1:53"] = answerMsg(dns.RcodeSuccess, "203.0.113.78")

	config := &Config{DNSProxyEnabled: true}
	snapshots := NewSnapshotStore()
	snapshots.Store(&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")})

	upstream := NewUpstreamResolver(config, snapshots)
	upstream.exchange = harness.exchange
	upstream.lookupDNS = func(string) []string { return []string{"172.20.10.1"} }

	proxy := NewDNSProxy(config, upstream, nil)

	proxy.handleDNSRequest(&captureWriter{}, queryFor("example.com", dns.TypeA))
	first := len(harness.tried())
	proxy.handleDNSRequest(&captureWriter{}, queryFor("example.com", dns.TypeA))

	if len(harness.tried()) != first {
		t.Fatalf("expected the second query to be served from cache, attempts went from %d to %d", first, len(harness.tried()))
	}
}

func TestHandleRequestReportsFailureWhenNoUpstreamAnswers(t *testing.T) {
	harness := newHarness()
	harness.failures["172.20.10.1:53"] = errors.New("i/o timeout")

	config := &Config{DNSProxyEnabled: true, DNSUpstream: "172.20.10.1:53"}
	snapshots := NewSnapshotStore()
	snapshots.Store(&NetworkSnapshot{PhoneReady: true, PhoneGateway: "172.20.10.1", PhoneInterface: deviceInfo("en7")})

	upstream := NewUpstreamResolver(config, snapshots)
	upstream.exchange = harness.exchange
	upstream.lookupDNS = func(string) []string { return nil }

	proxy := NewDNSProxy(config, upstream, nil)
	writer := &captureWriter{}
	proxy.handleDNSRequest(writer, queryFor("example.com", dns.TypeA))

	if writer.msg == nil || writer.msg.Rcode != dns.RcodeServerFailure {
		t.Fatalf("expected SERVFAIL when nothing answers, got %#v", writer.msg)
	}
}

func TestHandleRequestRejectsEmptyQuestion(t *testing.T) {
	proxy := NewDNSProxy(&Config{DNSProxyEnabled: true}, nil, nil)
	writer := &captureWriter{}

	proxy.handleDNSRequest(writer, new(dns.Msg))

	if writer.msg == nil || writer.msg.Rcode != dns.RcodeFormatError {
		t.Fatalf("expected FORMERR, got %#v", writer.msg)
	}
}

func newLoopbackProxy(t *testing.T) *DNSProxy {
	t.Helper()

	probe, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	return NewDNSProxy(&Config{
		DNSProxyEnabled: true,
		DNSProxyPort:    port,
		TetherDomains:   []string{"example.com"},
	}, nil, nil)
}

// captureWriter records the reply instead of sending it.
type captureWriter struct {
	msg *dns.Msg
}

func (w *captureWriter) LocalAddr() net.Addr  { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5454} }
func (w *captureWriter) RemoteAddr() net.Addr { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000} }
func (w *captureWriter) WriteMsg(msg *dns.Msg) error {
	w.msg = msg
	return nil
}
func (w *captureWriter) Write(payload []byte) (int, error) { return len(payload), nil }
func (w *captureWriter) Close() error                      { return nil }
func (w *captureWriter) TsigStatus() error                 { return nil }
func (w *captureWriter) TsigTimersOnly(bool)               {}
func (w *captureWriter) Hijack()                           {}
