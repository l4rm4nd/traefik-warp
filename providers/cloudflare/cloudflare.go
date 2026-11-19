// Package cloudflare contains a list of current Cloudflare IP ranges
package cloudflare

import (
	"bufio"
	"fmt"
	"net/http"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
)

var URLs = []string{
	"https://www.cloudflare.com/ips-v4",
	"https://www.cloudflare.com/ips-v6",
}

var Cache = &providers.ListCache{Fetch: fetch, TTL: time.Hour, RetryAfter: 30 * time.Second}

// TrustedIPS fetches Cloudflare's current IP ranges (IPv4 + IPv6).
func TrustedIPS() []string {
	ipList, err := Cache.Get(time.Now())

	// Fallback: if nothing was fetched, allow all
	if err != nil || len(ipList) == 0 {
		return []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"172.16.0.0/12",
		}
	}

	return ipList
}

func fetch() ([]string, error) {
	var ipList []string
	for _, url := range URLs {
		resp, err := providers.HTTPClient.Get(url)
		if err != nil {
			fmt.Println("Error fetching", url, ":", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			fmt.Println("Error fetching", url, ": status", resp.StatusCode)
			continue
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			ip := scanner.Text()
			if ip != "" {
				ipList = append(ipList, ip)
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Println("Error reading response from", url, ":", err)
		}
		resp.Body.Close()
	}

	if len(ipList) == 0 {
		return nil, providers.ErrNoList
	}
	return ipList, nil
}

const ClientIPHeaderName = "CF-Connecting-IP"
const CfVisitor = "CF-Visitor"
const XCfTrusted = "X-Is-Trusted"
