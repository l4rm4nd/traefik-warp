// serve_http.go
package traefik_warp

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/l4rm4nd/traefik-warp/providers"
	"github.com/l4rm4nd/traefik-warp/providers/cloudflare"
	"github.com/l4rm4nd/traefik-warp/providers/cloudfront"
)

// cleanInboundForwardingHeaders removes spoofable forwarding headers.
// We always set trusted values ourselves after validation.
func cleanInboundForwardingHeaders(h http.Header) {
	h.Del(xForwardFor)
	h.Del(xRealIP)
	h.Del(xForwardProto)
	h.Del("Forwarded")
}

// appendXFF appends client to X-Forwarded-For per common proxy behavior.
func appendXFF(h http.Header, client string) {
	if client == "" {
		return
	}
	if prior := h.Get(xForwardFor); prior != "" {
		h.Set(xForwardFor, prior+", "+client)
	} else {
		h.Set(xForwardFor, client)
	}
}

// parseSocketIP extracts the remote IP from a net/http RemoteAddr string (ip:port or [ip]:port).
func parseSocketIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	// Fallback: if SplitHostPort failed (rare), try as raw IP.
	return remoteAddr
}

// extractClientIP tries to normalize a header value that might be "ip:port" or just "ip".
func extractClientIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// First try standard host:port parsing (works for [v6]:port and v4:port).
	if host, _, err := net.SplitHostPort(raw); err == nil && host != "" {
		return host
	}

	// If it looks like a bracketed IPv6 without port.
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
	}

	// If it's a bare IPv6 or IPv4, keep it if valid.
	if ip := net.ParseIP(raw); ip != nil {
		return raw
	}

	// Defensive: if there's a trailing :NNN and removing it yields a valid IP, strip it.
	if i := strings.LastIndexByte(raw, ':'); i > 0 {
		hostPart := raw[:i]
		portPart := raw[i+1:]
		if _, err := strconv.Atoi(portPart); err == nil {
			if ip := net.ParseIP(hostPart); ip != nil {
				return hostPart
			}
		}
	}

	// Give up — caller should fall back to socket IP.
	return ""
}

// ipInProvider checks if ipStr is contained in a provider bucket.
func (r *Disolver) ipInProvider(prov providers.Provider, ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range r.TrustIP[prov] {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (r *Disolver) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	trustResult := r.trust(req.RemoteAddr, req)

	if trustResult.isFatal {
		http.Error(rw, "Unknown source", http.StatusInternalServerError)
		return
	}
	if trustResult.isError {
		http.Error(rw, "Unknown source", http.StatusBadRequest)
		return
	}
	if trustResult.directIP == "" {
		http.Error(rw, "Unknown source", http.StatusUnprocessableEntity)
		return
	}

	// Always clear spoofable headers first.
	cleanInboundForwardingHeaders(req.Header)

	// Figure out which provider the *socket IP* matches, if any.
	socketIP := parseSocketIP(req.RemoteAddr)
	matched := providers.Unknown
	if r.ipInProvider(providers.Cloudflare, socketIP) {
		matched = providers.Cloudflare
	} else if r.ipInProvider(providers.Cloudfront, socketIP) {
		matched = providers.Cloudfront
	}

	if trustResult.trusted {
		// Provider-agnostic trust markers
		req.Header.Set(xWarpTrusted, "yes")
		switch matched {
		case providers.Cloudflare:
			req.Header.Set(xWarpProvider, "cloudflare")
		case providers.Cloudfront:
			req.Header.Set(xWarpProvider, "cloudfront")
		default:
			req.Header.Set(xWarpProvider, "unknown")
		}

		// Provider-specific handling
		switch r.provider {
		case providers.Cloudflare, providers.Auto:
			// Only consider CF-Visitor if the socket edge matched Cloudflare.
			if matched == providers.Cloudflare {
				if v := req.Header.Get(cloudflare.CfVisitor); v != "" {
					var cfv CFVisitorHeader
					if json.Unmarshal([]byte(v), &cfv) == nil {
						s := strings.ToLower(strings.TrimSpace(cfv.Scheme))
						if s == "http" || s == "https" {
							req.Header.Set(xForwardProto, s)
						}
					}
					// Drop raw CF-Visitor header to avoid leaking upstream.
					req.Header.Del(cloudflare.CfVisitor)
				}
			}
		case providers.Cloudfront:
			// No special headers beyond client IP extraction.
		}

		// Decide which header contains the client IP, binding to the matched provider in Auto.
		var clientIPHeaderName string
		switch r.provider {
		case providers.Auto:
			// Use the socket IP match to decide which header to trust
			if matched == providers.Cloudflare && req.Header.Get(cloudflare.ClientIPHeaderName) != "" {
				clientIPHeaderName = cloudflare.ClientIPHeaderName
			} else if matched == providers.Cloudfront && req.Header.Get(cloudfront.ClientIPHeaderName) != "" {
				clientIPHeaderName = cloudfront.ClientIPHeaderName
			} else {
				// no matching provider/header combo → will fall back to socket IP
			}
		default:
			clientIPHeaderName = r.clientIPHeaderName
		}

		// Extract and validate client IP
		var clientIP string
		if clientIPHeaderName != "" {
			clientIP = extractClientIP(req.Header.Get(clientIPHeaderName))
			if net.ParseIP(clientIP) == nil {
				clientIP = ""
			}
		}
		if clientIP == "" {
			// Fallback to the direct socket IP (already parsed by r.trust()).
			clientIP = trustResult.directIP
		}

		// Ensure X-Forwarded-Proto is set if not already (e.g., CF-Visitor absent).
		if req.Header.Get(xForwardProto) == "" {
			if req.TLS != nil {
				req.Header.Set(xForwardProto, "https")
			} else {
				req.Header.Set(xForwardProto, "http")
			}
		}

		// Set forwarding headers
		appendXFF(req.Header, clientIP)
		req.Header.Set(xRealIP, clientIP)

	} else {
		// Provider-agnostic trust markers (untrusted)
		req.Header.Set(xWarpTrusted, "no")
		req.Header.Set(xWarpProvider, "unknown")

		// Untrusted: strip provider-specific headers and mark untrusted where applicable.
		switch r.provider {
		case providers.Cloudflare, providers.Auto:
			req.Header.Del(cloudflare.CfVisitor)
			req.Header.Del(cloudflare.ClientIPHeaderName)
		case providers.Cloudfront:
			req.Header.Del(cloudfront.ClientIPHeaderName)
		}

		// Use the direct socket IP.
		useIP := trustResult.directIP
		if useIP == "" {
			useIP = socketIP
		}
		appendXFF(req.Header, useIP)
		req.Header.Set(xRealIP, useIP)

		// Proto fallback
		if req.Header.Get(xForwardProto) == "" {
			if req.TLS != nil {
				req.Header.Set(xForwardProto, "https")
			} else {
				req.Header.Set(xForwardProto, "http")
			}
		}
	}

	// Hand off to the next handler.
	r.next.ServeHTTP(rw, req)
}
