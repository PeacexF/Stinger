package resolver

import (
	"context"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNameservers(t *testing.T) {
	if got := Nameservers(nil); !slices.Equal(got, []string{"1.1.1.1:53", "8.8.8.8:53"}) {
		t.Errorf("default = %v", got)
	}
	if got := Nameservers([]string{"9.9.9.9", "10.0.0.1:5353", "2001:db8::1"}); !slices.Equal(got,
		[]string{"9.9.9.9:53", "10.0.0.1:5353", "[2001:db8::1]:53"}) {
		t.Errorf("configured = %v", got)
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestCache(lookup MXFunc) (*Cache, *clock) {
	c := NewCache(lookup, time.Hour, time.Hour)
	clk := &clock{t: time.Unix(1_000_000, 0)}
	c.now = clk.now
	return c, clk
}

func TestCache_MXCachesWithinTTL(t *testing.T) {
	var calls atomic.Int32
	c, clk := newTestCache(func(ctx context.Context, d string) ([]string, error) {
		calls.Add(1)
		return []string{"mx1.example.com"}, nil
	})
	ctx := context.Background()
	c.MX(ctx, "example.com")
	c.MX(ctx, "example.com")
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	clk.t = clk.t.Add(2 * time.Hour)
	c.MX(ctx, "example.com")
	if calls.Load() != 2 {
		t.Errorf("calls after TTL = %d, want 2", calls.Load())
	}
}

func TestCache_NotFoundCachedAsEmpty(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestCache(func(ctx context.Context, d string) ([]string, error) {
		calls.Add(1)
		return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
	})
	for range 2 {
		hosts, err := c.MX(context.Background(), "nope.invalid")
		if err != nil || len(hosts) != 0 {
			t.Fatalf("got %v, %v", hosts, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestCache_TransientErrorNotCached(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestCache(func(ctx context.Context, d string) ([]string, error) {
		calls.Add(1)
		return nil, &net.DNSError{Err: "i/o timeout", IsTimeout: true}
	})
	for range 2 {
		if _, err := c.MX(context.Background(), "slow.example"); !IsTimeout(err) {
			t.Fatalf("expected timeout, got %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestCache_ConcurrentLookupsShareOneQuery(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c, _ := newTestCache(func(ctx context.Context, d string) ([]string, error) {
		calls.Add(1)
		<-release
		return []string{"mx"}, nil
	})
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { c.MX(context.Background(), "example.com") })
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestCache_CatchAll(t *testing.T) {
	c, clk := newTestCache(nil)
	if _, ok := c.CatchAll("a.com"); ok {
		t.Fatal("expected miss")
	}
	c.SetCatchAll("a.com", true)
	c.SetCatchAll("b.com", false)
	if v, ok := c.CatchAll("a.com"); !ok || !v {
		t.Errorf("a.com = %v, %v", v, ok)
	}
	if v, ok := c.CatchAll("b.com"); !ok || v {
		t.Errorf("b.com = %v, %v", v, ok)
	}
	clk.t = clk.t.Add(2 * time.Hour)
	if _, ok := c.CatchAll("a.com"); ok {
		t.Error("expected expiry")
	}
}
