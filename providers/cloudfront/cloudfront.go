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

var Cache = &providers.ListCache{Fetch: fetch, TTL: providers.ListTTL}

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
	var data map[string][]string
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", URL, err)
	}

	globalIPList, globalExists := data["CLOUDFRONT_GLOBAL_IP_LIST"]
	regionalIPList, regionalExists := data["CLOUDFRONT_REGIONAL_EDGE_IP_LIST"]
	if !globalExists && !regionalExists {
		return nil, errors.New("both keys are missing in the response")
	}

	var merged []string
	merged = append(merged, globalIPList...)
	merged = append(merged, regionalIPList...)
	for _, cidr := range merged {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("not a CIDR in %s: %q", URL, cidr)
		}
	}
	return merged, nil
}

const ClientIPHeaderName = "Cloudfront-Viewer-Address"
