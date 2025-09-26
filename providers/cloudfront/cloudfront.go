// Package ips contains a list of current cloud flare IP ranges
package cloudfront

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

// CFIPs is the CloudFlare Server IP list (this is checked on build).
func TrustedIPS() []string {

	// Found at https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html
	url := "https://d7uri8nf7uskq.cloudfront.net/tools/list-cloudfront-ips"

	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("Error making the request:", err)
		return []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"172.16.0.0/12",
		}
	}
	defer resp.Body.Close() // Ensure the response body is closed

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading the response body:", err)
		return []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"172.16.0.0/12",
		}
	}
	// Define a map to hold the JSON data
	var data map[string][]string

	// Parse the JSON response
	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println("Error parsing the JSON:", err)
		return []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"172.16.0.0/12",
		}
	}

	// Extract the arrays
	globalIPList, globalExists := data["CLOUDFRONT_GLOBAL_IP_LIST"]
	regionalIPList, regionalExists := data["CLOUDFRONT_REGIONAL_EDGE_IP_LIST"]

	if !globalExists && !regionalExists {
		fmt.Println("Both keys are missing in the response")
		return []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"172.16.0.0/12",
		}
	}

	// Merge the arrays
	mergedIPList := append(globalIPList, regionalIPList...)

	return mergedIPList
}

const ClientIPHeaderName = "Cloudfront-Viewer-Address"
