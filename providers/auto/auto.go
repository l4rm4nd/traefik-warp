package auto

import (
	"github.com/l4rm4nd/traefikdisolver/providers/cloudflare"
	"github.com/l4rm4nd/traefikdisolver/providers/cloudfront"
)

// TrustedIPS returns the union of Cloudflare and CloudFront trusted ranges.
func TrustedIPS() []string {
	var merged []string
	merged = append(merged, cloudflare.TrustedIPS()...)
	merged = append(merged, cloudfront.TrustedIPS()...)
	return merged
}
