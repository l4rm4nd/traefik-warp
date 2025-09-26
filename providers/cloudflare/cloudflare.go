// Package cloudflare contains a list of current Cloudflare IP ranges
package cloudflare

import (
	"bufio"
	"fmt"
	"net/http"
)

// TrustedIPS fetches Cloudflare's current IP ranges (IPv4 + IPv6).
func TrustedIPS() []string {
	urls := []string{
		"https://www.cloudflare.com/ips-v4",
		"https://www.cloudflare.com/ips-v6",
	}

	var ipList []string
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			fmt.Println("Error fetching", url, ":", err)
			continue
		}
		defer resp.Body.Close()

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
	}

	// Fallback: if nothing was fetched, allow all
	if len(ipList) == 0 {
		return []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"172.16.0.0/12",
		}
	}

	return ipList
}

const ClientIPHeaderName = "CF-Connecting-IP"
const CfVisitor = "CF-Visitor"
const XCfTrusted = "X-Is-Trusted"
