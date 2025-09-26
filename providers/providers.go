package providers

import (
	"errors"
	"fmt"
)

type Provider string

const (
	// Auto Detect - it's only for testing purposes... when you have a Router which handles incoming connections from different Providers
	Unknown    Provider = "unknown"
	Auto       Provider = "auto"
	Cloudfront Provider = "cloudfront"
	Cloudflare Provider = "cloudflare"
)

var List = map[Provider]Provider{
	Auto:       Auto,
	Cloudfront: Cloudfront,
	Cloudflare: Cloudflare,
}

var ListExisting = map[Provider]Provider{
	Cloudfront: Cloudfront,
	Cloudflare: Cloudflare,
}

func (p Provider) String() string {
	if v, ok := List[p]; ok {
		return string(v)
	}
	return ""
}

func (p *Provider) Validate() error {
	if _, ok := List[*p]; !ok {
		return errors.New(fmt.Sprint("invalid value", *p))
	}
	return nil
}
