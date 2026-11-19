package cloudflare

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
)

func serve(t *testing.T, v4, v6 func(w http.ResponseWriter)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "v4") {
			v4(w)
		} else {
			v6(w)
		}
	}))
	t.Cleanup(srv.Close)

	oldURLs, oldCache := URLs, Cache
	t.Cleanup(func() { URLs, Cache = oldURLs, oldCache })
	URLs = []string{srv.URL + "/ips-v4", srv.URL + "/ips-v6"}
	Cache = &providers.ListCache{Fetch: fetch, TTL: providers.ListTTL, RetryAfter: providers.RetryInterval}
}

func body(s string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.Write([]byte(s)) }
}

func status(code int) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func TestFetchMergesBothLists(t *testing.T) {
	serve(t, body("173.245.48.0/20\n103.21.244.0/22\n"), body("2400:cb00::/32\n"))

	got, err := fetch()
	want := []string{"173.245.48.0/20", "103.21.244.0/22", "2400:cb00::/32"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestFetchFailsWhenOneListFails(t *testing.T) {
	serve(t, body("173.245.48.0/20\n"), status(http.StatusInternalServerError))

	if got, err := fetch(); err == nil {
		t.Fatalf("got %v; a missing IPv6 list must fail the whole fetch", got)
	}
}

func TestFetchRejectsAPageThatIsNotAList(t *testing.T) {
	serve(t, body("<html>Just a moment...</html>\n"), body("2400:cb00::/32\n"))

	if got, err := fetch(); err == nil {
		t.Fatalf("got %v; want an error for a non-CIDR line", got)
	}
}

func TestTrustedIPSIsEmptyWithoutAList(t *testing.T) {
	serve(t, status(http.StatusServiceUnavailable), status(http.StatusServiceUnavailable))

	if got := TrustedIPS(); got != nil {
		t.Fatalf("got %v; with no list nothing may be trusted", got)
	}
}

func TestFetchToleratesCRLFBlankLinesAndNoTrailingNewline(t *testing.T) {
	serve(t, body("173.245.48.0/20\r\n\r\n  103.21.244.0/22  \r\n"), body("2400:cb00::/32"))

	got, err := fetch()
	want := []string{"173.245.48.0/20", "103.21.244.0/22", "2400:cb00::/32"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestFetchRejectsABareIP(t *testing.T) {
	serve(t, body("173.245.48.1\n"), body("2400:cb00::/32\n"))

	if got, err := fetch(); err == nil {
		t.Fatalf("got %v; a line without a prefix length is not a range", got)
	}
}

func TestFetchRejectsAnEmptyList(t *testing.T) {
	serve(t, body("\n\n"), body("2400:cb00::/32\n"))

	if got, err := fetch(); err == nil {
		t.Fatalf("got %v; an empty IPv4 list must fail", got)
	}
}

func TestFetchRejectsAnOverlongLine(t *testing.T) {
	serve(t, body(strings.Repeat("1", 70*1024)+"\n"), body("2400:cb00::/32\n"))

	if got, err := fetch(); err == nil {
		t.Fatalf("got %d entries; a line over the scanner limit must fail", len(got))
	}
}

func TestFetchRejectsACutOffBody(t *testing.T) {
	cut := func(w http.ResponseWriter) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("173.245.48.0/20\n"))
		// The handler returns early: the client sees a body shorter than Content-Length.
	}
	serve(t, cut, body("2400:cb00::/32\n"))

	if got, err := fetch(); err == nil {
		t.Fatalf("got %v; a truncated body must fail", got)
	}
}

func TestFetchFollowsARedirect(t *testing.T) {
	var srvURL string
	serve(t, func(w http.ResponseWriter) {
		w.Header().Set("Location", srvURL+"/moved-v6")
		w.WriteHeader(http.StatusMovedPermanently)
	}, body("2400:cb00::/32\n"))
	srvURL = strings.TrimSuffix(URLs[0], "/ips-v4")

	got, err := fetch()
	want := []string{"2400:cb00::/32", "2400:cb00::/32"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestFetchTimesOut(t *testing.T) {
	release := make(chan struct{})
	serve(t, func(w http.ResponseWriter) { <-release }, body("2400:cb00::/32\n"))
	t.Cleanup(func() { close(release) }) // after serve: cleanups run in reverse, srv.Close waits for the handler
	oldClient := providers.HTTPClient
	t.Cleanup(func() { providers.HTTPClient = oldClient })
	providers.HTTPClient = &http.Client{Timeout: 50 * time.Millisecond}

	start := time.Now()
	_, err := fetch()
	if err == nil || !strings.Contains(err.Error(), "/ips-v4") {
		t.Fatalf("err %v; want a timeout naming the URL", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("fetch took %v despite the client timeout", time.Since(start))
	}
}

// Once a list is cached, a hanging Cloudflare must not delay the caller.
func TestStaleListIsReturnedWhileCloudflareHangs(t *testing.T) {
	hang := make(chan struct{})
	var hanging atomic.Bool
	serve(t, func(w http.ResponseWriter) {
		if hanging.Load() {
			<-hang
			return
		}
		w.Write([]byte("173.245.48.0/20\n"))
	}, body("2400:cb00::/32\n"))
	t.Cleanup(func() { close(hang) })
	// The refresh outlives the test, so it must not read URLs, which cleanup restores.
	v4 := URLs[0]
	Cache = &providers.ListCache{Fetch: func() ([]string, error) { return fetchURL(v4) }, TTL: providers.ListTTL, RetryAfter: providers.RetryInterval}
	if got := TrustedIPS(); len(got) != 1 {
		t.Fatalf("initial load: got %v", got)
	}

	hanging.Store(true)
	Cache.TTL = 0
	start := time.Now()
	got := TrustedIPS()
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("TrustedIPS waited %v on a hanging refresh", took)
	}
	if len(got) != 1 {
		t.Fatalf("got %v, want the cached list", got)
	}
}
