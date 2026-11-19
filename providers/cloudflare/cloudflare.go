// Package cloudflare contains a list of current Cloudflare IP ranges
package cloudflare

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
)

var URLs = []string{
	"https://www.cloudflare.com/ips-v4",
	"https://www.cloudflare.com/ips-v6",
}

var Cache = &providers.ListCache{Fetch: fetch, TTL: providers.ListTTL, RetryAfter: providers.RetryInterval}

// TrustedIPS returns Cloudflare's current IP ranges (IPv4 + IPv6), or nil if they
// could not be downloaded yet: then no source is trusted.
func TrustedIPS() []string {
	ipList, err := Cache.Get(time.Now())
	if err != nil {
		fmt.Println("Cloudflare IP list unavailable:", err)
		return nil
	}
	return ipList
}

// fetch fails as a whole if any URL fails, so a partial list never replaces a complete one.
func fetch() ([]string, error) {
	var ipList []string
	for _, url := range URLs {
		list, err := fetchURL(url)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", url, err)
		}
		ipList = append(ipList, list...)
	}
	return ipList, nil
}

func fetchURL(url string) ([]string, error) {
	resp, err := providers.HTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var list []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(line); err != nil {
			return nil, fmt.Errorf("not a CIDR: %q", line)
		}
		list = append(list, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no ranges in the response")
	}
	return list, nil
}

const ClientIPHeaderName = "CF-Connecting-IP"
const CfVisitor = "CF-Visitor"
const XCfTrusted = "X-Is-Trusted"
