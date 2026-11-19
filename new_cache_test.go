package traefik_warp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
	"github.com/l4rm4nd/traefik-warp/providers/cloudflare"
	"github.com/l4rm4nd/traefik-warp/providers/cloudfront"
)

const (
	cfEdge  = "173.245.48.5"
	cfnEdge = "120.52.22.100"
)

// cdn fakes both providers' downloads, switchable at runtime. HTTP and parsing are covered
// by the provider packages; here only the fetch outcome matters.
type cdn struct {
	cfUp, cfnUp           atomic.Bool
	cfFetches, cfnFetches atomic.Int32
}

// useCDN gives the providers fresh caches backed by the fake, with a short retry. The caches
// are swapped through their pointer vars: under Yaegi an assignment to another package's
// slice var (cloudflare.URLs) is silently dropped.
// Background goroutines started by a test must stop touching these globals before it ends:
// cancel their context, or let them load the list and go idle.
func useCDN(t *testing.T) *cdn {
	t.Helper()
	c := &cdn{}
	c.cfUp.Store(true)
	c.cfnUp.Store(true)

	oldCF, oldCFN, oldRetry := cloudflare.Cache, cloudfront.Cache, providers.RetryInterval
	t.Cleanup(func() {
		cloudflare.Cache, cloudfront.Cache = oldCF, oldCFN
		providers.RetryInterval = oldRetry
	})
	providers.RetryInterval = 20 * time.Millisecond
	cloudflare.Cache = &providers.ListCache{TTL: time.Hour, RetryAfter: providers.RetryInterval, Fetch: func() ([]string, error) {
		c.cfFetches.Add(1)
		if !c.cfUp.Load() {
			return nil, errors.New("cloudflare down")
		}
		return []string{"173.245.48.0/20", "2400:cb00::/32"}, nil
	}}
	cloudfront.Cache = &providers.ListCache{TTL: time.Hour, RetryAfter: providers.RetryInterval, Fetch: func() ([]string, error) {
		c.cfnFetches.Add(1)
		if !c.cfnUp.Load() {
			return nil, errors.New("cloudfront down")
		}
		return []string{"120.52.22.96/27"}, nil
	}}
	return c
}

func newWarp(t *testing.T, ctx context.Context, provider string, autoRefresh bool, extra map[string][]string) *Disolver {
	t.Helper()
	cfg := CreateConfig()
	cfg.Provider = provider
	cfg.AutoRefresh = autoRefresh
	cfg.RefreshInterval = "24h"
	if extra != nil {
		cfg.TrustIP = extra
	}
	h, err := New(ctx, http.HandlerFunc(echoHeaders), cfg, "real-ip")
	if err != nil {
		t.Fatal(err)
	}
	return h.(*Disolver)
}

func echoHeaders(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{"X-Real-Ip", "X-Forwarded-For", "X-Warp-Trusted", "Cf-Connecting-Ip"} {
		w.Header().Set("Seen-"+k, r.Header.Get(k))
	}
}

func send(d *Disolver, remoteIP, cfConnectingIP string) http.Header {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = net.JoinHostPort(remoteIP, "4242")
	if cfConnectingIP != "" {
		req.Header.Set("CF-Connecting-IP", cfConnectingIP)
	}
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	return rec.Header()
}

func trusts(d *Disolver, ip string) bool {
	return send(d, ip, "").Get("Seen-X-Warp-Trusted") == "yes"
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Traefik builds one instance per router and entry point on every configuration change.
func TestManyInstancesFetchTheListsOnce(t *testing.T) {
	c := useCDN(t)

	for i := 0; i < 38; i++ {
		newWarp(t, context.Background(), "auto", false, nil)
	}

	if c.cfFetches.Load() != 1 || c.cfnFetches.Load() != 1 {
		t.Fatalf("cloudflare %d fetches, cloudfront %d; want 1 each for 38 instances", c.cfFetches.Load(), c.cfnFetches.Load())
	}
}

func TestNoListTrustsNobody(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newWarp(t, ctx, "cloudflare", true, nil)

	for _, ip := range []string{cfEdge, "10.0.0.1", "172.16.0.1", "192.168.1.1"} {
		h := send(d, ip, "198.51.100.7")
		if h.Get("Seen-X-Warp-Trusted") != "no" || h.Get("Seen-X-Real-Ip") != ip || h.Get("Seen-Cf-Connecting-Ip") != "" {
			t.Fatalf("source %s: trusted=%q real-ip=%q cf-connecting-ip=%q; want untrusted, socket IP, header stripped",
				ip, h.Get("Seen-X-Warp-Trusted"), h.Get("Seen-X-Real-Ip"), h.Get("Seen-Cf-Connecting-Ip"))
		}
	}

	// Let the retry loop load the list and go idle before cleanup restores the globals.
	c.cfUp.Store(true)
	eventually(t, "list after recovery", func() bool { return trusts(d, cfEdge) })
}

func TestUserTrustIPAppliesWithoutAList(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newWarp(t, ctx, "cloudflare", true, map[string][]string{"cloudflare": {"10.0.0.0/8"}})

	if h := send(d, "10.1.2.3", "198.51.100.7"); h.Get("Seen-X-Real-Ip") != "198.51.100.7" {
		t.Fatalf("trustip source: real-ip %q, want the CF-Connecting-IP value", h.Get("Seen-X-Real-Ip"))
	}
	if trusts(d, cfEdge) {
		t.Fatal("a Cloudflare edge must not be trusted before the list is downloaded")
	}
	c.cfUp.Store(true)
	eventually(t, "list after recovery", func() bool { return trusts(d, cfEdge) })
	if !trusts(d, "10.1.2.3") {
		t.Fatal("trustip ranges must survive the refresh")
	}
}

// A pod that starts while Cloudflare is unreachable picks the list up on the next retry
// instead of waiting for refreshInterval.
func TestInstanceRetriesUntilTheListArrives(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newWarp(t, ctx, "cloudflare", true, nil)

	if trusts(d, cfEdge) {
		t.Fatal("trusted before any list was downloaded")
	}
	c.cfUp.Store(true)
	eventually(t, "list after recovery", func() bool { return trusts(d, cfEdge) })
}

func TestRetriesEvenWithAutoRefreshOff(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	d := newWarp(t, context.Background(), "cloudflare", false, nil)

	c.cfUp.Store(true)
	eventually(t, "list after recovery", func() bool { return trusts(d, cfEdge) })
}

func TestRetriesDoNotStampede(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	instances := make([]*Disolver, 40)
	for i := range instances {
		instances[i] = newWarp(t, context.Background(), "cloudflare", false, nil)
	}

	time.Sleep(10 * providers.RetryInterval)
	// One fetch per RetryInterval for the whole process, not one per instance.
	if n := c.cfFetches.Load(); n > 14 {
		t.Fatalf("%d Cloudflare fetches in 10 retry intervals from 40 instances", n)
	}

	c.cfUp.Store(true)
	for _, d := range instances {
		eventually(t, "instance list after recovery", func() bool { return trusts(d, cfEdge) })
	}
}

func TestAutoTrustsCloudflareWhileCloudFrontIsMissing(t *testing.T) {
	c := useCDN(t)
	c.cfnUp.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newWarp(t, ctx, "auto", true, nil)

	if !trusts(d, cfEdge) {
		t.Fatal("Cloudflare must be trusted even though CloudFront failed")
	}
	if trusts(d, cfnEdge) {
		t.Fatal("CloudFront trusted before its list was downloaded")
	}
	c.cfnUp.Store(true)
	eventually(t, "CloudFront list after recovery", func() bool { return trusts(d, cfnEdge) })
}

func TestCanceledInstanceStopsRetrying(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	d := newWarp(t, ctx, "cloudflare", true, nil)
	eventually(t, "a retry", func() bool { return c.cfFetches.Load() >= 2 })

	cancel()
	time.Sleep(3 * providers.RetryInterval)
	stopped := c.cfFetches.Load()
	time.Sleep(5 * providers.RetryInterval)
	if c.cfFetches.Load() != stopped {
		t.Fatalf("Cloudflare fetched %d more times after the instance was canceled", c.cfFetches.Load()-stopped)
	}
	// Taking the instance's lock orders the loop's last reads of the globals before cleanup.
	trusts(d, cfEdge)
}

// Requests keep being served while refreshes swap the allowlist (run with -race).
func TestServeWhileRefreshing(t *testing.T) {
	c := useCDN(t)
	c.cfUp.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newWarp(t, ctx, "cloudflare", true, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					send(d, cfEdge, "198.51.100.7")
				}
			}
		}()
	}
	time.Sleep(3 * providers.RetryInterval)
	c.cfUp.Store(true)
	eventually(t, "list after recovery", func() bool { return trusts(d, cfEdge) })
	close(stop)
	wg.Wait()
}
