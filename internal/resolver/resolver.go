// Package resolver provides DNS lookups against explicit nameservers,
// plus TTL caches for MX records and catch-all detection results.
package resolver

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var DefaultNameservers = []string{"1.1.1.1", "8.8.8.8"}

const queryTimeout = 10 * time.Second

// New returns a pure-Go resolver that queries the given nameservers in
// round-robin order, ignoring /etc/resolv.conf. Empty means DefaultNameservers.
func New(nameservers []string) *net.Resolver {
	servers := Nameservers(nameservers)
	var next atomic.Uint32
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			server := servers[int(next.Add(1)-1)%len(servers)]
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, server)
		},
	}
}

// Nameservers normalises a configured list into host:port form
func Nameservers(configured []string) []string {
	if len(configured) == 0 {
		configured = DefaultNameservers
	}
	out := make([]string, 0, len(configured))
	for _, s := range configured {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			s = net.JoinHostPort(s, "53")
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return Nameservers(nil)
	}
	return out
}

// LookupMX returns MX hostnames sorted by preference, without trailing dots.
// A null MX (".") is dropped.
func LookupMX(ctx context.Context, r *net.Resolver, domain string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	records, err := r.LookupMX(ctx, domain)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Pref < records[j].Pref })
	hosts := make([]string, 0, len(records))
	for _, mx := range records {
		if h := strings.TrimSuffix(mx.Host, "."); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts, nil
}

// IsNotFound reports a permanent "no such domain / no records" answer
func IsNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// IsTimeout reports a DNS query timeout
func IsTimeout(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsTimeout {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// MXFunc resolves the MX hosts of a domain
type MXFunc func(ctx context.Context, domain string) ([]string, error)

// Cache wraps an MXFunc with a TTL cache and also stores catch-all results.
// Concurrent lookups for the same domain share a single query.
type Cache struct {
	lookup MXFunc
	mxTTL  time.Duration
	caTTL  time.Duration
	now    func() time.Time

	mu       sync.Mutex
	mx       map[string]entry[[]string]
	catchAll map[string]entry[bool]
	inflight map[string]*call
}

type entry[T any] struct {
	val T
	at  time.Time
}

type call struct {
	done  chan struct{}
	hosts []string
	err   error
}

func NewCache(lookup MXFunc, mxTTL, catchAllTTL time.Duration) *Cache {
	return &Cache{
		lookup:   lookup,
		mxTTL:    mxTTL,
		caTTL:    catchAllTTL,
		now:      time.Now,
		mx:       make(map[string]entry[[]string]),
		catchAll: make(map[string]entry[bool]),
		inflight: make(map[string]*call),
	}
}

// MX returns cached hosts or queries them. NXDOMAIN / no-answer is cached as
// an empty list; transient errors (timeouts, SERVFAIL) are returned uncached.
func (c *Cache) MX(ctx context.Context, domain string) ([]string, error) {
	c.mu.Lock()
	if e, ok := c.mx[domain]; ok && c.now().Sub(e.at) <= c.mxTTL {
		c.mu.Unlock()
		return e.val, nil
	}
	if cl, ok := c.inflight[domain]; ok {
		c.mu.Unlock()
		select {
		case <-cl.done:
			return cl.hosts, cl.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[domain] = cl
	c.mu.Unlock()

	hosts, err := c.lookup(ctx, domain)
	if IsNotFound(err) {
		hosts, err = []string{}, nil
	}

	c.mu.Lock()
	if err == nil {
		c.mx[domain] = entry[[]string]{hosts, c.now()}
	}
	delete(c.inflight, domain)
	c.mu.Unlock()

	cl.hosts, cl.err = hosts, err
	close(cl.done)
	return hosts, err
}

// CatchAll returns the cached catch-all flag for domain, if fresh
func (c *Cache) CatchAll(domain string) (isCatchAll, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.catchAll[domain]
	if !found || c.now().Sub(e.at) > c.caTTL {
		return false, false
	}
	return e.val, true
}

func (c *Cache) SetCatchAll(domain string, isCatchAll bool) {
	c.mu.Lock()
	c.catchAll[domain] = entry[bool]{isCatchAll, c.now()}
	c.mu.Unlock()
}
