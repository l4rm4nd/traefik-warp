package traefik_warp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/l4rm4nd/traefik-warp/providers"
	"github.com/l4rm4nd/traefik-warp/providers/cloudflare"
	"github.com/l4rm4nd/traefik-warp/providers/cloudfront"
)

// New creates a new plugin.
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.Provider == "" {
		return nil, fmt.Errorf("no provider has been defined")
	}

	provider := providers.Provider(config.Provider)
	if err := provider.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate provider %q: %w", config.Provider, err)
	}

	d := &Disolver{
		next:     next,
		name:     name,
		provider: provider,
		TrustIP:  make(map[providers.Provider][]*net.IPNet),
	}

	// Choose header for direct client IP extraction; Auto is decided in ServeHTTP.
	switch provider {
	case providers.Cloudflare:
		d.clientIPHeaderName = cloudflare.ClientIPHeaderName
	case providers.Cloudfront:
		d.clientIPHeaderName = cloudfront.ClientIPHeaderName
	}

	addCIDRs := func(p providers.Provider, cidrs []string) {
		for _, v := range cidrs {
			cidr := strings.TrimSpace(v)
			if cidr == "" {
				continue
			}
			_, n, err := net.ParseCIDR(cidr)
			if err != nil {
				// skip bad entries (log if you have a logger)
				fmt.Printf("skipping invalid CIDR %q for provider %s: %v\n", cidr, p, err)
				continue
			}
			d.TrustIP[p] = append(d.TrustIP[p], n)
		}
	}

	// 1) Load defaults
	switch provider {
	case providers.Cloudflare:
		addCIDRs(providers.Cloudflare, cloudflare.TrustedIPS())
	case providers.Cloudfront:
		addCIDRs(providers.Cloudfront, cloudfront.TrustedIPS())
	case providers.Auto:
		addCIDRs(providers.Cloudflare, cloudflare.TrustedIPS())
		addCIDRs(providers.Cloudfront, cloudfront.TrustedIPS())
		// or: addCIDRs(providers.Auto, auto.TrustedIPS()) if you prefer a single bucket
	}

	// 2) Merge user-provided CIDRs
	for prov := range providers.ListExisting {
		if list, ok := config.TrustIP[prov.String()]; ok {
			addCIDRs(prov, list)
		}
	}

	return d, nil
}
