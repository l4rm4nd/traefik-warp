package traefik_warp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
	"github.com/l4rm4nd/traefik-warp/providers/cloudflare"
	"github.com/l4rm4nd/traefik-warp/providers/cloudfront"
)

// Traefik builds one instance per router and entry point on every configuration change.
func TestManyInstancesFetchTheListsOnce(t *testing.T) {
	var cfCalls, cfnCalls atomic.Int32
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfCalls.Add(1)
		w.Write([]byte("173.245.48.0/20\n"))
	}))
	defer cf.Close()
	cfn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfnCalls.Add(1)
		w.Write([]byte(`{"CLOUDFRONT_GLOBAL_IP_LIST":["120.52.22.96/27"],"CLOUDFRONT_REGIONAL_EDGE_IP_LIST":[]}`))
	}))
	defer cfn.Close()

	useServers(t, cf.URL, cfn.URL)

	cfg := CreateConfig()
	cfg.Provider = "auto"
	cfg.AutoRefresh = false
	for i := 0; i < 38; i++ {
		if _, err := New(context.Background(), http.NotFoundHandler(), cfg, "real-ip"); err != nil {
			t.Fatalf("instance %d: %v", i, err)
		}
	}

	if cfCalls.Load() != 2 || cfnCalls.Load() != 1 {
		t.Fatalf("cloudflare %d requests, cloudfront %d; want 2 and 1 for all 38 instances", cfCalls.Load(), cfnCalls.Load())
	}
}

func useServers(t *testing.T, cfURL, cfnURL string) {
	t.Helper()
	oldURLs, oldURL, oldCF, oldCFN := cloudflare.URLs, cloudfront.URL, cloudflare.Cache, cloudfront.Cache
	oldRetry := providers.RetryInterval
	t.Cleanup(func() {
		cloudflare.URLs, cloudfront.URL, cloudflare.Cache, cloudfront.Cache = oldURLs, oldURL, oldCF, oldCFN
		providers.RetryInterval = oldRetry
	})
	cloudflare.URLs = []string{cfURL + "/ips-v4", cfURL + "/ips-v6"}
	cloudfront.URL = cfnURL
	cloudflare.Cache = &providers.ListCache{Fetch: oldCF.Fetch, TTL: time.Hour}
	cloudfront.Cache = &providers.ListCache{Fetch: oldCFN.Fetch, TTL: time.Hour}
}

// A pod that starts while Cloudflare is unreachable trusts nobody, and picks the list up
// on the next retry instead of waiting for refreshInterval.
func TestInstanceRetriesUntilTheListArrives(t *testing.T) {
	var up atomic.Bool
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if strings.HasSuffix(r.URL.Path, "v4") {
			w.Write([]byte("173.245.48.0/20\n"))
		} else {
			w.Write([]byte("2400:cb00::/32\n"))
		}
	}))
	defer cf.Close()
	useServers(t, cf.URL, cf.URL)
	providers.RetryInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := CreateConfig()
	cfg.Provider = "cloudflare"
	cfg.AutoRefresh = true
	cfg.RefreshInterval = "24h"
	h, err := New(ctx, http.NotFoundHandler(), cfg, "real-ip")
	if err != nil {
		t.Fatal(err)
	}
	d := h.(*Disolver)

	edge, private := net.ParseIP("173.245.48.5"), net.ParseIP("10.0.0.1")
	if d.contains(providers.Cloudflare, edge) || d.contains(providers.Cloudflare, private) {
		t.Fatal("without a list no source may be trusted")
	}

	up.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for !d.contains(providers.Cloudflare, edge) {
		if time.Now().After(deadline) {
			t.Fatal("the instance did not pick up the list after Cloudflare came back")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if d.contains(providers.Cloudflare, private) {
		t.Fatal("private ranges must not be trusted once the real list is loaded")
	}
}
