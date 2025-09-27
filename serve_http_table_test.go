// serve_http_table_test.go
package traefik_warp

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/l4rm4nd/traefik-warp/providers"
)

type verboseNext struct{}

func (verboseNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Echo selected headers so tests can assert easily
	xrip := r.Header.Get("X-Real-IP")
	w.Header().Set("Got-XRIP", xrip)
	w.Header().Set("Got-XFF", r.Header.Get("X-Forwarded-For"))
	w.Header().Set("Got-XFP", r.Header.Get("X-Forwarded-Proto"))

	// Neutral markers
	w.Header().Set("Got-Warp-Trusted", r.Header.Get("X-Warp-Trusted"))
	w.Header().Set("Got-Warp-Provider", r.Header.Get("X-Warp-Provider"))

	// Infer where XRIP came from: cf header, cloudfront header, or socket
	src := "socket"
	if cf := extractClientIP(r.Header.Get("CF-Connecting-IP")); cf != "" && cf == xrip {
		src = "cf"
	} else if cfn := extractClientIP(r.Header.Get("Cloudfront-Viewer-Address")); cfn != "" && cfn == xrip {
		src = "cloudfront"
	} else {
		// compare against socket
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host == "" {
			host = r.RemoteAddr
		}
		if host != xrip {
			src = "unknown"
		}
	}
	w.Header().Set("Got-Source", src)

	w.WriteHeader(http.StatusOK)
}

func Test_Scenarios_RealIPExtraction(t *testing.T) {
	tests := []struct {
		name            string
		provider        providers.Provider
		trustCIDRs      map[providers.Provider][]string
		remoteAddr      string
		headers         map[string]string
		wantIP          string
		wantWarpTrusted string // "yes" | "no"
		wantWarpProv    string // "cloudflare" | "cloudfront" | "unknown"
	}{
		{
			name:            "untrusted socket only",
			provider:        providers.Cloudflare,
			remoteAddr:      "203.0.113.7:54321",
			wantIP:          "203.0.113.7",
			wantWarpTrusted: "no",
			wantWarpProv:    "unknown",
		},
		{
			name:     "trusted cloudflare header and CF-Visitor",
			provider: providers.Cloudflare,
			trustCIDRs: map[providers.Provider][]string{
				providers.Cloudflare: {"198.51.100.0/24"},
			},
			remoteAddr: "198.51.100.23:443",
			headers: map[string]string{
				"CF-Connecting-IP": "1.2.3.4",
				"CF-Visitor":       `{"scheme":"https"}`,
			},
			wantIP:          "1.2.3.4",
			wantWarpTrusted: "yes",
			wantWarpProv:    "cloudflare",
		},
		{
			name:     "trusted cloudfront viewer address",
			provider: providers.Cloudfront,
			trustCIDRs: map[providers.Provider][]string{
				providers.Cloudfront: {"203.0.113.0/24"},
			},
			remoteAddr: "203.0.113.99:1234",
			headers: map[string]string{
				"Cloudfront-Viewer-Address": "5.6.7.8:5555",
			},
			wantIP:          "5.6.7.8",
			wantWarpTrusted: "yes",
			wantWarpProv:    "cloudfront",
		},
		{
			name:     "auto picks cloudfront when socket in cloudfront and both headers present",
			provider: providers.Auto,
			trustCIDRs: map[providers.Provider][]string{
				providers.Cloudfront: {"203.0.113.0/24"},
				providers.Cloudflare: {"198.51.100.0/24"},
			},
			remoteAddr: "203.0.113.10:443",
			headers: map[string]string{
				"CF-Connecting-IP":          "9.9.9.9",        // should be ignored
				"Cloudfront-Viewer-Address": "5.6.7.8:1234",  // should win
			},
			wantIP:          "5.6.7.8",
			wantWarpTrusted: "yes",
			wantWarpProv:    "cloudfront",
		},
		{
			name:     "auto picks cloudflare when socket in cloudflare and CF header present",
			provider: providers.Auto,
			trustCIDRs: map[providers.Provider][]string{
				providers.Cloudfront: {"203.0.113.0/24"},
				providers.Cloudflare: {"198.51.100.0/24"},
			},
			remoteAddr: "198.51.100.23:443",
			headers: map[string]string{
				"CF-Connecting-IP": "7.7.7.7",
			},
			wantIP:          "7.7.7.7",
			wantWarpTrusted: "yes",
			wantWarpProv:    "cloudflare",
		},
		{
			name:     "malformed cloudfront header falls back to socket ip (still trusted)",
			provider: providers.Cloudfront,
			trustCIDRs: map[providers.Provider][]string{
				providers.Cloudfront: {"203.0.113.0/24"},
			},
			remoteAddr: "203.0.113.10:443",
			headers: map[string]string{
				"Cloudfront-Viewer-Address": "not-an-ip:abc",
			},
			wantIP:          "203.0.113.10",
			wantWarpTrusted: "yes",
			wantWarpProv:    "cloudfront",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Handler under test
			d := &Disolver{
				next:     verboseNext{},
				name:     "test",
				provider: tc.provider,
				TrustIP:  make(map[providers.Provider][]*net.IPNet),
			}
			switch tc.provider {
			case providers.Cloudflare:
				d.clientIPHeaderName = "CF-Connecting-IP"
			case providers.Cloudfront:
				d.clientIPHeaderName = "Cloudfront-Viewer-Address"
			}

			// Seed trust CIDRs
			for p, cidrs := range tc.trustCIDRs {
				for _, c := range cidrs {
					_, n, err := net.ParseCIDR(c)
					if err != nil {
						t.Fatalf("bad test CIDR %q: %v", c, err)
					}
					d.TrustIP[p] = append(d.TrustIP[p], n)
				}
			}

			// Build request
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://example.test/", nil)
			req.RemoteAddr = tc.remoteAddr
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			// Exercise
			d.ServeHTTP(rr, req)

			// Assertions
			if rr.Code != http.StatusOK {
				t.Fatalf("status=%d", rr.Code)
			}
			gotIP := rr.Header().Get("Got-XRIP")
			gotSrc := rr.Header().Get("Got-Source")
			gotWarpTrusted := rr.Header().Get("Got-Warp-Trusted")
			gotWarpProv := rr.Header().Get("Got-Warp-Provider")

			// pretty one-liner trace per case (visible with -v)
			t.Logf("provider=%-10s remote=%-22s src=%-10s -> X-Real-IP=%-15s WarpTrusted=%q WarpProvider=%q",
				tc.provider.String(),
				tc.remoteAddr,
				gotSrc,
				gotIP,
				gotWarpTrusted,
				gotWarpProv,
			)

			if gotIP != tc.wantIP {
				t.Fatalf("want X-Real-IP=%q got=%q", tc.wantIP, gotIP)
			}
			if gotWarpTrusted != tc.wantWarpTrusted {
				t.Fatalf("want X-Warp-Trusted=%q got=%q", tc.wantWarpTrusted, gotWarpTrusted)
			}
			if gotWarpProv != tc.wantWarpProv {
				t.Fatalf("want X-Warp-Provider=%q got=%q", tc.wantWarpProv, gotWarpProv)
			}
		})
	}
}
