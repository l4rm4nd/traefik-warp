package cloudflare

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

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
	Cache = &providers.ListCache{Fetch: fetch, TTL: providers.ListTTL}
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
