// Package ips contains a list of current cloud flare IP ranges
package cloudfront

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/l4rm4nd/traefik-warp/providers"
)

// Found at https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html
var URL = "https://d7uri8nf7uskq.cloudfront.net/tools/list-cloudfront-ips"

var Cache = &providers.ListCache{Fetch: fetch, TTL: time.Hour, RetryAfter: 30 * time.Second}

// CFIPs is the CloudFlare Server IP list (this is checked on build).
func TrustedIPS() []string {
	ipList, err := Cache.Get(time.Now())
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
	resp, err := providers.HTTPClient.Get(URL)
	if err != nil {
		fmt.Println("Error making the request:", err)
		return nil, err
	}
	defer resp.Body.Close() // Ensure the response body is closed
	if resp.StatusCode != http.StatusOK {
		fmt.Println("Error making the request: status", resp.StatusCode)
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading the response body:", err)
		return nil, err
	}
	// Define a map to hold the JSON data
	var data map[string][]string

	// Parse the JSON response
	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println("Error parsing the JSON:", err)
		return nil, err
	}

	// Extract the arrays
	globalIPList, globalExists := data["CLOUDFRONT_GLOBAL_IP_LIST"]
	regionalIPList, regionalExists := data["CLOUDFRONT_REGIONAL_EDGE_IP_LIST"]

	if !globalExists && !regionalExists {
		fmt.Println("Both keys are missing in the response")
		return nil, errors.New("both keys are missing in the response")
	}

	// Merge the arrays
	mergedIPList := append(globalIPList, regionalIPList...)

	return mergedIPList, nil
}

const ClientIPHeaderName = "Cloudfront-Viewer-Address"
