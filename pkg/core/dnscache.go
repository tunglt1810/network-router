package core

import (
	"container/list"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	cacheMinTTL     = 5 * time.Second
	cacheMaxTTL     = time.Hour
	cacheNegativeTTL = 30 * time.Second
	cacheDefaultMax  = 4096
)

type cacheKey struct {
	name   string
	qtype  uint16
	qclass uint16
}

type cacheEntry struct {
	key      cacheKey
	msg      *dns.Msg
	storedAt time.Time
	expires  time.Time
}

// DNSCache is a small TTL cache for upstream answers. It exists for two reasons:
// it keeps repeat queries off the upstream path, and a cache hit means no route
// work is scheduled, which is what keeps the DNS reply path cheap.
type DNSCache struct {
	mu    sync.Mutex
	items map[cacheKey]*list.Element
	order *list.List
	max   int
	now   func() time.Time
}

func NewDNSCache(max int) *DNSCache {
	if max <= 0 {
		max = cacheDefaultMax
	}
	return &DNSCache{
		items: make(map[cacheKey]*list.Element, max),
		order: list.New(),
		max:   max,
		now:   time.Now,
	}
}

func keyFor(question dns.Question) cacheKey {
	return cacheKey{
		name:   dns.CanonicalName(question.Name),
		qtype:  question.Qtype,
		qclass: question.Qclass,
	}
}

// Get returns a reply for the request, with TTLs aged by the time spent in cache.
func (c *DNSCache) Get(request *dns.Msg) (*dns.Msg, bool) {
	if len(request.Question) == 0 {
		return nil, false
	}

	key := keyFor(request.Question[0])
	now := c.now()

	c.mu.Lock()
	element, ok := c.items[key]
	if !ok {
		c.mu.Unlock()
		return nil, false
	}
	entry := element.Value.(*cacheEntry)
	if now.After(entry.expires) {
		c.order.Remove(element)
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	c.order.MoveToFront(element)
	reply := entry.msg.Copy()
	elapsed := uint32(now.Sub(entry.storedAt).Seconds())
	c.mu.Unlock()

	ageRecords(reply, elapsed)
	reply.Id = request.Id
	reply.Question = request.Question
	return reply, true
}

// Put stores a reply. Only definitive answers are cached.
func (c *DNSCache) Put(request *dns.Msg, reply *dns.Msg) {
	if len(request.Question) == 0 || reply == nil {
		return
	}
	if reply.Rcode != dns.RcodeSuccess && reply.Rcode != dns.RcodeNameError {
		return
	}
	if reply.Truncated {
		return
	}

	ttl := cacheNegativeTTL
	if reply.Rcode == dns.RcodeSuccess && len(reply.Answer) > 0 {
		ttl = clampTTL(minimumTTL(reply))
	}

	now := c.now()
	entry := &cacheEntry{
		key:      keyFor(request.Question[0]),
		msg:      reply.Copy(),
		storedAt: now,
		expires:  now.Add(ttl),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.items[entry.key]; ok {
		element.Value = entry
		c.order.MoveToFront(element)
		return
	}

	c.items[entry.key] = c.order.PushFront(entry)
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheEntry).key)
	}
}

// Purge empties the cache. It is called when the routing path changes, since
// cached answers may name addresses that are no longer routed.
func (c *DNSCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[cacheKey]*list.Element, c.max)
	c.order.Init()
}

func minimumTTL(msg *dns.Msg) time.Duration {
	smallest := uint32(0)
	first := true
	for _, section := range [][]dns.RR{msg.Answer, msg.Ns} {
		for _, rr := range section {
			if rr.Header().Rrtype == dns.TypeOPT {
				continue
			}
			ttl := rr.Header().Ttl
			if first || ttl < smallest {
				smallest = ttl
				first = false
			}
		}
	}
	if first {
		return cacheMinTTL
	}
	return time.Duration(smallest) * time.Second
}

func clampTTL(ttl time.Duration) time.Duration {
	if ttl < cacheMinTTL {
		return cacheMinTTL
	}
	if ttl > cacheMaxTTL {
		return cacheMaxTTL
	}
	return ttl
}

func ageRecords(msg *dns.Msg, elapsed uint32) {
	if elapsed == 0 {
		return
	}
	for _, section := range [][]dns.RR{msg.Answer, msg.Ns, msg.Extra} {
		for _, rr := range section {
			header := rr.Header()
			if header.Rrtype == dns.TypeOPT {
				continue
			}
			if header.Ttl > elapsed {
				header.Ttl -= elapsed
			} else {
				header.Ttl = 1
			}
		}
	}
}
