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

		retryInterval: providers.RetryInterval,
	}

	switch provider {
	case providers.Cloudflare:
		d.clientIPHeaderName = cloudflare.ClientIPHeaderName
	case providers.Cloudfront:
		d.clientIPHeaderName = cloudfront.ClientIPHeaderName
	}

	// Initial allowlist build
	loadErr := d.refreshOnce()
	if loadErr != nil {
		logWarn("warp: initial CIDR load had issues", "error", loadErr.Error(), "middleware", name)
	} else {
		cf, cfn := d.counts()
		logInfo("warp: CIDRs loaded", "cf", fmt.Sprintf("%d", cf), "cfn", fmt.Sprintf("%d", cfn), "middleware", name)
	}

	// Periodic refresh. Without it a failed initial load is still retried until it succeeds.
	switch {
	case config.AutoRefresh:
		ival, err := time.ParseDuration(config.RefreshInterval)
		if err != nil || ival <= 0 {
			ival = 12 * time.Hour
		}
		jitter := time.Duration(int64(time.Second) * (int64(time.Now().UnixNano()) % 7))
		first := ival + jitter
		if loadErr != nil {
			first = d.retryInterval
		}
		go d.refreshLoop(ctx, ival, first)
	case loadErr != nil:
		go d.refreshLoop(ctx, 0, d.retryInterval)
	}

	return d, nil
}

// refreshLoop periodically refreshes the allowlists until ctx is done.
// While a provider list is missing it retries every d.retryInterval.
// With interval 0 it stops after the first successful refresh.
// It sleeps instead of selecting on a timer: under Yaegi, timers in many concurrent selects
// sometimes stop firing, which left instances without a list after an outage.
func (d *Disolver) refreshLoop(ctx context.Context, interval, first time.Duration) {
	wait := first
	for sleepCtx(ctx, wait, d.retryInterval) {
		if err := d.refreshOnce(); err != nil {
			logWarn("warp: periodic CIDR refresh failed", "error", err.Error())
			wait = d.retryInterval
			continue
		}
		cf, cfn := d.counts()
		logInfo("warp: refreshed CIDRs", "cf", fmt.Sprintf("%d", cf), "cfn", fmt.Sprintf("%d", cfn))
		if interval <= 0 {
			return
		}
		wait = interval
	}
}

// sleepCtx sleeps for total in steps of at most step and reports whether ctx is still alive,
// so a canceled instance's goroutine ends within one step.
func sleepCtx(ctx context.Context, total, step time.Duration) bool {
	for total > 0 && ctx.Err() == nil {
		s := step
		if s <= 0 || total < s {
			s = total
		}
		time.Sleep(s)
		total -= s
	}
	return ctx.Err() == nil
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

	// Checked before the user extras are merged: they must not hide a missing download.
	var missing []string
	if (d.provider == providers.Cloudflare || d.provider == providers.Auto) && len(cfCIDRs) == 0 {
		missing = append(missing, providers.Cloudflare.String())
	}
	if (d.provider == providers.Cloudfront || d.provider == providers.Auto) && len(cfnCIDRs) == 0 {
		missing = append(missing, providers.Cloudfront.String())
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

	if len(missing) > 0 {
		return fmt.Errorf("IP list not available yet: %s", strings.Join(missing, ", "))
	}
	return nil
}
