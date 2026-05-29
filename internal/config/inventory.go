package config

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	CredentialProviderAge       = "age"
	CredentialProvider1Password = "1password"
)

// CredentialConfig selects the single active credential provider backend.
type CredentialConfig struct {
	Type   string                         `toml:"type"`
	Config CredentialProviderDetailConfig `toml:"config"`
	Host   map[string]CredentialRefConfig `toml:"host"`
	Group  map[string]CredentialRefConfig `toml:"group"`
}

// CredentialProviderDetailConfig holds backend-specific credential provider
// settings. Only the fields for the selected provider are used.
type CredentialProviderDetailConfig struct {
	Account string `toml:"account"`
	Vault   string `toml:"vault"`
}

// CredentialRefConfig maps a host or group credential scope to an existing
// external secret object. Ref is provider-specific but should be a stable item
// ID/name or a provider URI. Username can be literal or resolved from
// UsernameRef.
type CredentialRefConfig struct {
	Ref         string `toml:"ref"`
	Username    string `toml:"username"`
	UsernameRef string `toml:"username_ref"`
}

// InventoryConfig holds group metadata and external inventory provider config.
type InventoryConfig struct {
	DefaultGroup string                             `toml:"default_group"`
	Group        map[string]GroupConfig             `toml:"group"`
	Provider     map[string]InventoryProviderConfig `toml:"provider"`
}

// GroupConfig describes a logical inventory group.
type GroupConfig struct {
	DomainSuffix []string `toml:"domain_suffix"`
}

// InventoryProviderConfig configures one named external inventory provider.
type InventoryProviderConfig struct {
	Type   string                        `toml:"type"`
	Config InventoryProviderDetailConfig `toml:"config"`
	Route  []InventoryRouteConfig        `toml:"route"`
}

// InventoryProviderDetailConfig holds implementation-specific provider config.
type InventoryProviderDetailConfig struct {
	BaseURL               string `toml:"base_url"`
	URLEnv                string `toml:"url_env"`
	TokenEnv              string `toml:"token_env"`
	EnvFile               string `toml:"env_file"`
	JumpHost              string `toml:"jump_host"`
	Sudo                  bool   `toml:"sudo"`
	StrictHostKeyChecking bool   `toml:"strict_host_key_checking"`
}

// InventoryRouteConfig defines a provider route into a group.
type InventoryRouteConfig struct {
	Name  string              `toml:"name"`
	Group string              `toml:"group"`
	Match InventoryRouteMatch `toml:"match"`
}

// InventoryRouteMatch is an open map of field names to allowed values.
type InventoryRouteMatch map[string][]string

var bareKeySafeName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Validate checks credential provider configuration.
func (c *CredentialConfig) Validate() error {
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	if c.Type == "" {
		c.Type = CredentialProviderAge
	}

	switch c.Type {
	case CredentialProviderAge:
		return nil
	case CredentialProvider1Password:
		if strings.TrimSpace(c.Config.Vault) == "" {
			return fmt.Errorf("credential.config.vault is required for %q", CredentialProvider1Password)
		}
	default:
		return fmt.Errorf("unsupported credential provider %q", c.Type)
	}
	for host, ref := range c.Host {
		if strings.TrimSpace(host) == "" {
			return fmt.Errorf("credential.host has empty host")
		}
		if err := ref.Validate("credential.host." + host); err != nil {
			return err
		}
	}
	for group, ref := range c.Group {
		if strings.TrimSpace(group) == "" {
			return fmt.Errorf("credential.group has empty group")
		}
		if err := validateBareKeySafe("credential.group", group); err != nil {
			return err
		}
		if err := ref.Validate("credential.group." + group); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks a credential reference mapping.
func (c CredentialRefConfig) Validate(scope string) error {
	if strings.TrimSpace(c.Ref) == "" {
		return fmt.Errorf("%s.ref is required", scope)
	}
	if strings.TrimSpace(c.Username) != "" && strings.TrimSpace(c.UsernameRef) != "" {
		return fmt.Errorf("%s.username and username_ref are mutually exclusive", scope)
	}
	return nil
}

// Validate checks inventory group and provider configuration.
func (c *InventoryConfig) Validate() error {
	if strings.TrimSpace(c.DefaultGroup) == "" {
		c.DefaultGroup = "default"
	}
	if err := validateBareKeySafe("inventory.default_group", c.DefaultGroup); err != nil {
		return err
	}

	if c.Group == nil {
		c.Group = make(map[string]GroupConfig)
	}
	if _, ok := c.Group[c.DefaultGroup]; !ok {
		c.Group[c.DefaultGroup] = GroupConfig{}
	}
	for name := range c.Group {
		if err := validateBareKeySafe("inventory.group", name); err != nil {
			return err
		}
	}

	for name, provider := range c.Provider {
		if err := validateBareKeySafe("inventory.provider", name); err != nil {
			return err
		}
		if err := provider.Validate(c.Group); err != nil {
			return fmt.Errorf("inventory.provider.%s: %w", name, err)
		}
	}

	return nil
}

// Validate checks a single inventory provider.
func (c *InventoryProviderConfig) Validate(groups map[string]GroupConfig) error {
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	if c.Type == "" {
		return fmt.Errorf("type is required")
	}
	if !supportedProviders[c.Type] {
		return fmt.Errorf("unsupported provider %q", c.Type)
	}

	switch c.Type {
	case ProviderContainerlab:
		if strings.TrimSpace(c.Config.JumpHost) == "" {
			return fmt.Errorf("containerlab.config.jump_host is required")
		}
	case ProviderNetBox:
		// NetBox supports base_url directly or environment-backed URL config.
	}

	if len(c.Route) == 0 {
		return fmt.Errorf("at least one route is required")
	}
	for i, route := range c.Route {
		if strings.TrimSpace(route.Group) == "" {
			return fmt.Errorf("route[%d]: group is required", i)
		}
		if _, ok := groups[route.Group]; !ok {
			return fmt.Errorf("route[%d]: unknown group %q", i, route.Group)
		}
	}
	return nil
}

func validateBareKeySafe(scope, name string) error {
	if !bareKeySafeName.MatchString(name) {
		return fmt.Errorf("%s name %q must be TOML bare-key safe: letters, numbers, underscores, and dashes", scope, name)
	}
	return nil
}
