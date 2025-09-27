package traefik_warp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
	"github.com/l4rm4nd/traefik-warp/providers/cloudflare"
	"github.com/l4rm4nd/traefik-warp/providers/cloudfront"
)

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.Provider == "" {
		return nil, fmt.Errorf("no provider has been defined")
	}

	// enable/disable debug logging for this process
	enableDebug(config.Debug)

	provider := providers.Provider(config.Provider)
	if err := provider.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate provider %q: %w", config.Provider, err)
	}

	d := &Disolver{
		next:      next,
		name:      name,
		provider:  provider,
		TrustIP:   make(map[providers.Provider][]*net.IPNet),
		userTrust: config.TrustIP, // keep user additions for merges on refresh
	}

	switch provider {
	case providers.Cloudflare:
		d.clientIPHeaderName = cloudflare.ClientIPHeaderName
	case providers.Cloudfront:
		d.clientIPHeaderName = cloudfront.ClientIPHeaderName
	}

	// Initial allowlist build
	if err := d.refreshOnce(); err != nil {
		logWarn("warp: initial CIDR load had issues", "error", err.Error(), "middleware", name)
	} else {
		cf, cfn := d.counts()
		logInfo("warp: CIDRs loaded", "cf", fmt.Sprintf("%d", cf), "cfn", fmt.Sprintf("%d", cfn), "middleware", name)
	}

	// Periodic refresh
	if config.AutoRefresh {
		ival, err := time.ParseDuration(config.RefreshInterval)
		if err != nil || ival <= 0 {
			ival = 12 * time.Hour
		}
		go d.refreshLoop(ctx, ival)
	}

	return d, nil
}

// refreshLoop periodically refreshes the allowlists until ctx is done.
func (d *Disolver) refreshLoop(ctx context.Context, interval time.Duration) {
	jitter := time.Duration(int64(time.Second) * (int64(time.Now().UnixNano())%7))
	t := time.NewTimer(interval + jitter)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.refreshOnce(); err != nil {
				logWarn("warp: periodic CIDR refresh failed", "error", err.Error())
			} else {
				cf, cfn := d.counts()
				logInfo("warp: refreshed CIDRs", "cf", fmt.Sprintf("%d", cf), "cfn", fmt.Sprintf("%d", cfn))
			}
			t.Reset(interval)
		}
	}
}

// refreshOnce fetches defaults + merges user-supplied CIDRs, then swaps atomically.
func (d *Disolver) refreshOnce() error {
	// Fetch defaults depending on configured provider
	var cfCIDRs, cfnCIDRs []string
	switch d.provider {
	case providers.Cloudflare:
		cfCIDRs = cloudflare.TrustedIPS()
	case providers.Cloudfront:
		cfnCIDRs = cloudfront.TrustedIPS()
	case providers.Auto:
		cfCIDRs = cloudflare.TrustedIPS()
		cfnCIDRs = cloudfront.TrustedIPS()
	}

	// Merge user-provided extras
	if list, ok := d.userTrust[providers.Cloudflare.String()]; ok {
		cfCIDRs = append(cfCIDRs, list...)
	}
	if list, ok := d.userTrust[providers.Cloudfront.String()]; ok {
		cfnCIDRs = append(cfnCIDRs, list...)
	}

	// Build a fresh map
	newMap := make(map[providers.Provider][]*net.IPNet)
	add := func(p providers.Provider, cidrs []string) {
		for _, v := range cidrs {
			c := strings.TrimSpace(v)
			if c == "" {
				continue
			}
			_, n, err := net.ParseCIDR(c)
			if err != nil {
				continue
			}
			newMap[p] = append(newMap[p], n)
		}
	}

	if len(cfCIDRs) > 0 {
		add(providers.Cloudflare, cfCIDRs)
	}
	if len(cfnCIDRs) > 0 {
		add(providers.Cloudfront, cfnCIDRs)
	}

	// Swap atomically
	d.mu.Lock()
	d.TrustIP = newMap
	d.mu.Unlock()

	return nil
}
