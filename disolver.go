package traefik_warp

import (
	"net"
	"net/http"
        "sync"
	"github.com/l4rm4nd/traefik-warp/providers"
)

func (r *Disolver) counts() (cf, cfn int) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    cf = len(r.TrustIP[providers.Cloudflare])
    cfn = len(r.TrustIP[providers.Cloudfront])
    return
}

// Disolver is a plugin that overwrites the true IP.
type Disolver struct {
	next               http.Handler
	name               string
	provider           providers.Provider
	TrustIP            map[providers.Provider][]*net.IPNet
	clientIPHeaderName string

        mu                 sync.RWMutex               // guards TrustIP
	userTrust          map[string][]string        // keep user-supplied CIDRs for merges on refresh
}

// CFVisitorHeader definition for the header value.
type CFVisitorHeader struct {
	Scheme string `json:"scheme"`
}

// TrustResult for Trust IP test result.
type TrustResult struct {
	isFatal  bool
	isError  bool
	trusted  bool
	directIP string
}

// helper: membership check with lock
func (r *Disolver) contains(prov providers.Provider, ip net.IP) bool {
	r.mu.RLock()
	nets := r.TrustIP[prov]
	r.mu.RUnlock()
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// trust decides whether the REMOTE socket IP belongs to a trusted edge network.
// In Auto mode we treat trust as the UNION of Cloudflare + CloudFront.
func (r *Disolver) trust(remote string, _ *http.Request) *TrustResult {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return &TrustResult{isError: true}
	}

	switch r.provider {
	case providers.Cloudflare:
		if r.contains(providers.Cloudflare, ip) {
			return &TrustResult{trusted: true, directIP: ip.String()}
		}
	case providers.Cloudfront:
		if r.contains(providers.Cloudfront, ip) {
			return &TrustResult{trusted: true, directIP: ip.String()}
		}
	case providers.Auto:
		if r.contains(providers.Cloudflare, ip) || r.contains(providers.Cloudfront, ip) {
			return &TrustResult{trusted: true, directIP: ip.String()}
		}
	}
	return &TrustResult{trusted: false, directIP: ip.String()}
}
