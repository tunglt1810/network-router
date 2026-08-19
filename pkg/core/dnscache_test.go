package core

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func cachedReply(t *testing.T, name string, ttl uint32, rcode int) (*dns.Msg, *dns.Msg) {
	t.Helper()

	request := queryFor(name, dns.TypeA)
	reply := new(dns.Msg)
	reply.SetReply(request)
	reply.Rcode = rcode
	if rcode == dns.RcodeSuccess {
		rr, err := dns.NewRR(dns.Fqdn(name) + "\t" + itoa(ttl) + "\tIN\tA\t203.0.113.1")
		if err != nil {
			t.Fatalf("build rr: %v", err)
		}
		reply.Answer = []dns.RR{rr}
	}
	return request, reply
}

func itoa(value uint32) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func TestCacheReturnsStoredAnswer(t *testing.T) {
	cache := NewDNSCache(16)
	request, reply := cachedReply(t, "example.com", 300, dns.RcodeSuccess)

	cache.Put(request, reply)

	got, ok := cache.Get(request)
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if len(got.Answer) != 1 {
		t.Fatalf("expected the answer to be preserved, got %#v", got.Answer)
	}
}

func TestCacheReplyCarriesTheRequestID(t *testing.T) {
	cache := NewDNSCache(16)
	request, reply := cachedReply(t, "example.com", 300, dns.RcodeSuccess)
	cache.Put(request, reply)

	second := queryFor("example.com", dns.TypeA)
	second.Id = 4242

	got, ok := cache.Get(second)
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if got.Id != 4242 {
		t.Fatalf("cached reply must match the new request ID, got %d", got.Id)
	}
}

func TestCacheAgesTTLs(t *testing.T) {
	cache := NewDNSCache(16)
	now := time.Now()
	cache.now = func() time.Time { return now }

	request, reply := cachedReply(t, "example.com", 300, dns.RcodeSuccess)
	cache.Put(request, reply)

	cache.now = func() time.Time { return now.Add(60 * time.Second) }
	got, ok := cache.Get(request)
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if ttl := got.Answer[0].Header().Ttl; ttl != 240 {
		t.Fatalf("expected the TTL to be aged to 240, got %d", ttl)
	}
}

func TestCacheExpiresEntries(t *testing.T) {
	cache := NewDNSCache(16)
	now := time.Now()
	cache.now = func() time.Time { return now }

	request, reply := cachedReply(t, "example.com", 10, dns.RcodeSuccess)
	cache.Put(request, reply)

	cache.now = func() time.Time { return now.Add(11 * time.Second) }
	if _, ok := cache.Get(request); ok {
		t.Fatal("expected the entry to have expired")
	}
}

func TestCacheClampsShortTTLs(t *testing.T) {
	cache := NewDNSCache(16)
	now := time.Now()
	cache.now = func() time.Time { return now }

	request, reply := cachedReply(t, "example.com", 1, dns.RcodeSuccess)
	cache.Put(request, reply)

	// A one-second TTL is clamped up to the floor, so the entry survives.
	cache.now = func() time.Time { return now.Add(3 * time.Second) }
	if _, ok := cache.Get(request); !ok {
		t.Fatal("expected a short TTL to be clamped to the minimum")
	}
}

func TestCacheStoresNegativeAnswers(t *testing.T) {
	cache := NewDNSCache(16)
	request, reply := cachedReply(t, "nope.example.com", 0, dns.RcodeNameError)

	cache.Put(request, reply)

	got, ok := cache.Get(request)
	if !ok {
		t.Fatal("expected NXDOMAIN to be cached")
	}
	if got.Rcode != dns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got %s", dns.RcodeToString[got.Rcode])
	}
}

func TestCacheSkipsFailuresAndTruncatedReplies(t *testing.T) {
	cache := NewDNSCache(16)

	request, reply := cachedReply(t, "example.com", 300, dns.RcodeServerFailure)
	cache.Put(request, reply)
	if _, ok := cache.Get(request); ok {
		t.Fatal("SERVFAIL must not be cached")
	}

	request, reply = cachedReply(t, "trunc.example.com", 300, dns.RcodeSuccess)
	reply.Truncated = true
	cache.Put(request, reply)
	if _, ok := cache.Get(request); ok {
		t.Fatal("a truncated reply must not be cached")
	}
}

func TestCacheSeparatesQueryTypes(t *testing.T) {
	cache := NewDNSCache(16)
	request, reply := cachedReply(t, "example.com", 300, dns.RcodeSuccess)
	cache.Put(request, reply)

	if _, ok := cache.Get(queryFor("example.com", dns.TypeAAAA)); ok {
		t.Fatal("an A entry must not answer an AAAA query")
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := NewDNSCache(2)

	for _, name := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		request, reply := cachedReply(t, name, 300, dns.RcodeSuccess)
		cache.Put(request, reply)
	}

	if _, ok := cache.Get(queryFor("a.example.com", dns.TypeA)); ok {
		t.Fatal("expected the oldest entry to be evicted")
	}
	if _, ok := cache.Get(queryFor("c.example.com", dns.TypeA)); !ok {
		t.Fatal("expected the newest entry to be retained")
	}
}

func TestCachePurge(t *testing.T) {
	cache := NewDNSCache(16)
	request, reply := cachedReply(t, "example.com", 300, dns.RcodeSuccess)
	cache.Put(request, reply)

	cache.Purge()

	if _, ok := cache.Get(request); ok {
		t.Fatal("expected the cache to be empty after purge")
	}
}
