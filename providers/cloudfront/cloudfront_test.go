package cloudfront

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/l4rm4nd/traefik-warp/providers"
)

func serve(t *testing.T, code int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	oldURL, oldCache := URL, Cache
	t.Cleanup(func() { URL, Cache = oldURL, oldCache })
	URL = srv.URL
	Cache = &providers.ListCache{Fetch: fetch, TTL: providers.ListTTL, RetryAfter: providers.RetryInterval}
}

func TestFetch(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		body    string
		want    []string
		wantErr bool
	}{
		{"both lists", 200, `{"CLOUDFRONT_GLOBAL_IP_LIST":["120.52.22.96/27"],"CLOUDFRONT_REGIONAL_EDGE_IP_LIST":["13.113.196.64/26"]}`,
			[]string{"120.52.22.96/27", "13.113.196.64/26"}, false},
		{"global only", 200, `{"CLOUDFRONT_GLOBAL_IP_LIST":["120.52.22.96/27"]}`, []string{"120.52.22.96/27"}, false},
		{"regional only", 200, `{"CLOUDFRONT_REGIONAL_EDGE_IP_LIST":["13.113.196.64/26"]}`, []string{"13.113.196.64/26"}, false},
		{"extra key of another type", 200, `{"syncToken":1700000000,"CLOUDFRONT_GLOBAL_IP_LIST":["120.52.22.96/27"]}`, []string{"120.52.22.96/27"}, false},
		{"no known key", 200, `{"something":[]}`, nil, true},
		{"list of the wrong type", 200, `{"CLOUDFRONT_GLOBAL_IP_LIST":"120.52.22.96/27"}`, nil, true},
		{"not a CIDR", 200, `{"CLOUDFRONT_GLOBAL_IP_LIST":["120.52.22.96"]}`, nil, true},
		{"not JSON", 200, `<html>error</html>`, nil, true},
		{"status 500", 500, `{"CLOUDFRONT_GLOBAL_IP_LIST":["120.52.22.96/27"]}`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serve(t, tt.code, tt.body)
			got, err := fetch()
			if (err != nil) != tt.wantErr || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestTrustedIPSIsEmptyForEmptyLists(t *testing.T) {
	serve(t, 200, `{"CLOUDFRONT_GLOBAL_IP_LIST":[],"CLOUDFRONT_REGIONAL_EDGE_IP_LIST":[]}`)

	if got := TrustedIPS(); got != nil {
		t.Fatalf("got %v; empty lists must not count as a download", got)
	}
}
