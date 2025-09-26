package traefikwarp

import (
	"net"
	"net/http"

	"github.com/l4rm4nd/traefik-warp/providers"
)

// Disolver is a plugin that overwrites the true IP.
type Disolver struct {
	next               http.Handler
	name               string
	provider           providers.Provider
	TrustIP            map[providers.Provider][]*net.IPNet
	clientIPHeaderName string
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

// trust decides whether the REMOTE socket IP belongs to a trusted edge network.
// IMPORTANT: In Auto mode we treat trust as the UNION of Cloudflare + CloudFront.
// Headers must NOT influence this decision.
func (r *Disolver) trust(remote string, _ *http.Request) *TrustResult {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		// If it wasn’t host:port, take the raw string as a best effort
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return &TrustResult{
			isFatal:  false,
			isError:  true,
			trusted:  false,
			directIP: "",
		}
	}

	contains := func(list []*net.IPNet, ip net.IP) bool {
		for _, n := range list {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}

	switch r.provider {
	case providers.Cloudflare:
		if contains(r.TrustIP[providers.Cloudflare], ip) {
			return &TrustResult{trusted: true, directIP: ip.String()}
		}
	case providers.Cloudfront:
		if contains(r.TrustIP[providers.Cloudfront], ip) {
			return &TrustResult{trusted: true, directIP: ip.String()}
		}
	case providers.Auto:
		// UNION: trust if socket IP is in either bucket
		if contains(r.TrustIP[providers.Cloudflare], ip) || contains(r.TrustIP[providers.Cloudfront], ip) {
			return &TrustResult{trusted: true, directIP: ip.String()}
		}
	default:
		// Unknown provider → untrusted
	}

	return &TrustResult{
		trusted:  false,
		directIP: ip.String(),
	}
}
