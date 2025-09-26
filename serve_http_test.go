package traefikdisolver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/l4rm4nd/traefikdisolver/providers"
)

type captureNext struct{}

func (captureNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Echo headers so tests can assert easily
	w.Header().Set("Got-XFF", r.Header.Get("X-Forwarded-For"))
	w.Header().Set("Got-XRIP", r.Header.Get("X-Real-IP"))
	w.Header().Set("Got-XFP", r.Header.Get("X-Forwarded-Proto"))
	w.Header().Set("Got-CFTrusted", r.Header.Get("X-Is-Trusted"))
	w.WriteHeader(http.StatusOK)
}

func mustCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil || n == nil {
		t.Fatalf("bad CIDR %q: %v", cidr, err)
	}
	return n
}

func newTestDisolver(provider providers.Provider) *Disolver {
	d := &Disolver{
		next:     captureNext{},
		name:     "test",
		provider: provider,
		TrustIP:  make(map[providers.Provider][]*net.IPNet),
	}
	switch provider {
	case providers.Cloudflare:
		d.clientIPHeaderName = "CF-Connecting-IP"
	case providers.Cloudfront:
		d.clientIPHeaderName = "Cloudfront-Viewer-Address"
	}
	return d
}

func Test_Untrusted_UsesSocketIP_AndSetsHeaders(t *testing.T) {
	d := newTestDisolver(providers.Cloudflare) // provider choice doesn’t matter when untrusted

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "203.0.113.7:54321" // not in any trust range

	d.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if got := rr.Header().Get("Got-XRIP"); got != "203.0.113.7" {
		t.Fatalf("X-Real-IP=%q", got)
	}
	if got := rr.Header().Get("Got-XFF"); got != "203.0.113.7" {
		t.Fatalf("X-Forwarded-For=%q", got)
	}
	// Proto default (no TLS, no CF-Visitor)
	if got := rr.Header().Get("Got-XFP"); got != "http" {
		t.Fatalf("X-Forwarded-Proto=%q", got)
	}
	// Marked untrusted for CF provider
	if got := rr.Header().Get("Got-CFTrusted"); got != "no" {
		t.Fatalf("X-Is-Trusted=%q", got)
	}
}

func Test_Trusted_Cloudflare_HeaderPreferred(t *testing.T) {
	d := newTestDisolver(providers.Cloudflare)
	// Seed trust with a fake CF edge range and put socket IP inside it
	d.TrustIP[providers.Cloudflare] = append(d.TrustIP[providers.Cloudflare], mustCIDR(t, "198.51.100.0/24"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "198.51.100.23:443" // trusted edge
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	req.Header.Set("CF-Visitor", `{"scheme":"https"}`)

	d.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if got := rr.Header().Get("Got-XRIP"); got != "1.2.3.4" {
		t.Fatalf("X-Real-IP=%q", got)
	}
	if got := rr.Header().Get("Got-XFF"); got == "" || got[len(got)-7:] != "1.2.3.4" {
		t.Fatalf("X-Forwarded-For=%q", got)
	}
	if got := rr.Header().Get("Got-XFP"); got != "https" {
		t.Fatalf("X-Forwarded-Proto=%q", got)
	}
	if got := rr.Header().Get("Got-CFTrusted"); got != "yes" {
		t.Fatalf("X-Is-Trusted=%q", got)
	}
}

func Test_Trusted_Cloudfront_HeaderPreferred(t *testing.T) {
	d := newTestDisolver(providers.Cloudfront)
	d.TrustIP[providers.Cloudfront] = append(d.TrustIP[providers.Cloudfront], mustCIDR(t, "203.0.113.0/24"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "203.0.113.99:1234" // trusted edge
	req.Header.Set("Cloudfront-Viewer-Address", "5.6.7.8:5555")

	d.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if got := rr.Header().Get("Got-XRIP"); got != "5.6.7.8" {
		t.Fatalf("X-Real-IP=%q", got)
	}
	// No CF-Visitor; proto falls back to http (no TLS in test)
	if got := rr.Header().Get("Got-XFP"); got != "http" {
		t.Fatalf("X-Forwarded-Proto=%q", got)
	}
}

func Test_Auto_BindsHeaderToMatchedProvider(t *testing.T) {
	d := newTestDisolver(providers.Auto)
	// Seed both buckets
	d.TrustIP[providers.Cloudflare] = append(d.TrustIP[providers.Cloudflare], mustCIDR(t, "198.51.100.0/24"))
	d.TrustIP[providers.Cloudfront] = append(d.TrustIP[providers.Cloudfront], mustCIDR(t, "203.0.113.0/24"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	// Socket IP is in CloudFront range
	req.RemoteAddr = "203.0.113.10:443"
	// Both headers present; the code should choose CloudFront’s header because the edge matched CloudFront
	req.Header.Set("CF-Connecting-IP", "9.9.9.9")               // should be ignored
	req.Header.Set("Cloudfront-Viewer-Address", "5.6.7.8:1234") // should win

	d.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if got := rr.Header().Get("Got-XRIP"); got != "5.6.7.8" {
		t.Fatalf("X-Real-IP (expected CloudFront)=%q", got)
	}
}

func Test_CFVisitor_BadJSON_IsIgnored_NotFatal(t *testing.T) {
	d := newTestDisolver(providers.Cloudflare)
	d.TrustIP[providers.Cloudflare] = append(d.TrustIP[providers.Cloudflare], mustCIDR(t, "198.51.100.0/24"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.RemoteAddr = "198.51.100.5:443"
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	req.Header.Set("CF-Visitor", `{not-json}`) // bad JSON

	d.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	// Still sets client IP despite bad CF-Visitor
	if got := rr.Header().Get("Got-XRIP"); got != "1.2.3.4" {
		t.Fatalf("X-Real-IP=%q", got)
	}
	// Proto fallback (no TLS), since CF-Visitor parsing failed
	if got := rr.Header().Get("Got-XFP"); got != "http" {
		t.Fatalf("X-Forwarded-Proto=%q", got)
	}
}
