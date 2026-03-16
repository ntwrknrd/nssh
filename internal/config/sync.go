package config

import (
	"fmt"
	"strings"
)

// Provider name constants.
const (
	ProviderContainerlab = "containerlab"
	ProviderNetBox       = "netbox"
)

// SyncConfig holds the top-level sync configuration.
type SyncConfig struct {
	Sources []SyncSourceConfig `toml:"sources"`
}

// SyncSourceConfig defines a single sync source.
type SyncSourceConfig struct {
	Name         string              `toml:"name"`
	Provider     string              `toml:"provider"`
	Containerlab *ContainerlabConfig `toml:"containerlab"`
	NetBox       *NetBoxConfig       `toml:"netbox"`
	Routes       []SyncRouteConfig   `toml:"routes"`
}

// ContainerlabConfig holds containerlab provider settings.
type ContainerlabConfig struct {
	JumpHost              string `toml:"jump_host"`
	Sudo                  bool   `toml:"sudo"`
	StrictHostKeyChecking bool   `toml:"strict_host_key_checking"`
}

// NetBoxConfig holds NetBox provider settings.
type NetBoxConfig struct {
	BaseURL  string `toml:"base_url"`
	TokenEnv string `toml:"token_env"`
}

// SyncRouteConfig defines a route that filters and places discovered objects.
type SyncRouteConfig struct {
	Name    string         `toml:"name"`
	Context string         `toml:"context"`
	Match   SyncRouteMatch `toml:"match"`
}

// SyncRouteMatch is an open map of field names to value lists.
// Different fields are AND; multiple values within one field are OR.
type SyncRouteMatch map[string][]string

// supportedProviders lists the implemented provider names.
var supportedProviders = map[string]bool{
	ProviderContainerlab: true,
}

// Validate checks that the sync config is well-formed.
func (c *SyncConfig) Validate() error {
	if len(c.Sources) == 0 {
		return nil // no sync configured
	}

	seen := make(map[string]bool)
	for i, src := range c.Sources {
		if src.Name == "" {
			return fmt.Errorf("sync.sources[%d]: name is required", i)
		}
		if strings.ContainsAny(src.Name, "/\\.") {
			return fmt.Errorf("sync.sources[%d]: name %q must not contain '/', '\\', or '.'", i, src.Name)
		}
		if seen[src.Name] {
			return fmt.Errorf("sync.sources: duplicate source name %q", src.Name)
		}
		seen[src.Name] = true

		if err := src.Validate(); err != nil {
			return fmt.Errorf("sync.sources[%d] (%s): %w", i, src.Name, err)
		}
	}
	return nil
}

// Validate checks a single source config.
func (c *SyncSourceConfig) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if !supportedProviders[c.Provider] {
		return fmt.Errorf("unsupported provider %q", c.Provider)
	}

	// Exactly one provider block must be populated and must match provider field.
	hasClab := c.Containerlab != nil
	hasNetBox := c.NetBox != nil

	switch c.Provider {
	case ProviderContainerlab:
		if !hasClab {
			return fmt.Errorf("provider is %q but containerlab config block is missing", c.Provider)
		}
		if hasNetBox {
			return fmt.Errorf("provider is %q but netbox config block is also present", c.Provider)
		}
		if c.Containerlab.JumpHost == "" {
			return fmt.Errorf("containerlab.jump_host is required")
		}
	case ProviderNetBox:
		if !hasNetBox {
			return fmt.Errorf("provider is %q but netbox config block is missing", c.Provider)
		}
		if hasClab {
			return fmt.Errorf("provider is %q but containerlab config block is also present", c.Provider)
		}
		if c.NetBox.BaseURL == "" {
			return fmt.Errorf("netbox.base_url is required")
		}
		if c.NetBox.TokenEnv == "" {
			return fmt.Errorf("netbox.token_env is required")
		}
	}

	if len(c.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
	}

	for i, r := range c.Routes {
		if r.Context == "" {
			return fmt.Errorf("routes[%d]: context is required", i)
		}
	}

	return nil
}
