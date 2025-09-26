// serve_http_table_test.go (replace your current table test)
package traefikdisolver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/l4rm4nd/traefikdisolver/providers"
)

type verboseNext struct{}

func (verboseNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Echo headers
	xrip := r.Header.Get("X-Real-IP")
	w.Header().Set("Got-XRIP", xrip)
	w.Header().Set("Got-XFF", r.Header.Get("X-Forwarded-For"))
	w.Header().Set("Got-XFP", r.Header.Get("X-Forwarded-Proto"))
	w.Header().Set("Got-CFTrusted", r.Header.Get("X-Is-Trusted"))

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
		name       string
		provider   providers.Provider
		trustCIDRs map[providers.Provider][]string
		remoteAddr string
		headers    map[string]string
		wantIP     string
		wantXIs    string // "" → don’t check
	}{
		// … keep your cases exactly as before …
		{
			name:       "untrusted socket only",
			provider:   providers.Cloudflare,
			remoteAddr: "203.0.113.7:54321",
			wantIP:     "203.0.113.7",
			wantXIs:    "no",
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
			wantIP:  "1.2.3.4",
			wantXIs: "yes",
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
			wantIP: "5.6.7.8",
		},
		// … include your other cases …
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
			gotXIs := rr.Header().Get("Got-CFTrusted")

			// pretty one-liner trace per case (visible with -v)
			t.Logf("provider=%-10s remote=%-22s src=%-10s -> X-Real-IP=%-15s X-Is-Trusted=%q",
				tc.provider.String(),
				tc.remoteAddr,
				gotSrc,
				gotIP,
				gotXIs,
			)

			if gotIP != tc.wantIP {
				t.Fatalf("want X-Real-IP=%q got=%q", tc.wantIP, gotIP)
			}
			if tc.wantXIs != "" && gotXIs != tc.wantXIs {
				t.Fatalf("want X-Is-Trusted=%q got=%q", tc.wantXIs, gotXIs)
			}
		})
	}
}
