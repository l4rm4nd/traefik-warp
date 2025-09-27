package traefik_warp

import "github.com/l4rm4nd/traefik-warp/providers"

// Config the plugin configuration.
type Config struct {
	Provider            string              `json:"provider,omitempty"`
	TrustIP             map[string][]string `json:"trustip"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		Provider:            providers.Auto.String(), // TODO: if no provider has been set...
		TrustIP:             make(map[string][]string),
	}
}
