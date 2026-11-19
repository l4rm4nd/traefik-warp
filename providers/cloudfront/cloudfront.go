// Package ips contains a list of current cloud flare IP ranges
package cloudfront

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
)

// Found at https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html
var URL = "https://d7uri8nf7uskq.cloudfront.net/tools/list-cloudfront-ips"

var Cache = &providers.ListCache{Fetch: fetch, TTL: providers.ListTTL, RetryAfter: providers.RetryInterval}

// TrustedIPS returns CloudFront's current IP ranges, or nil if they could not be
// downloaded yet: then no source is trusted.
func TrustedIPS() []string {
	ipList, err := Cache.Get(time.Now())
	if err != nil {
		fmt.Println("CloudFront IP list unavailable:", err)
		return nil
	}
	return ipList
}

func fetch() ([]string, error) {
	resp, err := providers.HTTPClient.Get(URL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", URL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", URL, err)
	}
	// Raw values, so a key AWS might add with another type does not break the whole response.
	var data map[string]json.RawMessage
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", URL, err)
	}

	var merged []string
	found := false
	for _, key := range []string{"CLOUDFRONT_GLOBAL_IP_LIST", "CLOUDFRONT_REGIONAL_EDGE_IP_LIST"} {
		raw, ok := data[key]
		if !ok {
			continue
		}
		found = true
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("parsing %s in %s: %w", key, URL, err)
		}
		merged = append(merged, list...)
	}
	if !found {
		return nil, errors.New("both keys are missing in the response")
	}
	for _, cidr := range merged {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("not a CIDR in %s: %q", URL, cidr)
		}
	}
	return merged, nil
}

const ClientIPHeaderName = "Cloudfront-Viewer-Address"
