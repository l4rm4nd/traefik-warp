package traefik_warp

import "github.com/l4rm4nd/traefik-warp/providers"

// Config the plugin configuration.
type Config struct {
	Provider            string              `json:"provider,omitempty"`
	TrustIP             map[string][]string `json:"trustip"`
        AutoRefresh         bool                `json:"autoRefresh,omitempty"`     // enable periodic refresh
	RefreshInterval     string              `json:"refreshInterval,omitempty"` // e.g. "12h", "1h"
        Debug               bool                `json:"debug,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		Provider:            providers.Auto.String(), // TODO: if no provider has been set...
		TrustIP:             make(map[string][]string),
                AutoRefresh:         true,
		RefreshInterval:     "12h",
                Debug:               false,
	}
}
