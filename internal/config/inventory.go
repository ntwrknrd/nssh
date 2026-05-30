package config

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	CredentialProviderPass      = "pass"
	CredentialProvider1Password = "1password"
	CredentialProviderBitwarden = "bitwarden"
)

const (
	ProviderSessionExternal   = "external"
	ProviderSessionAgentOwned = "agent"
	ProviderSessionNone       = "none"
)

const (
	AuthModePassword = "password"
	AuthModeKey      = "key"
)

// Provider name constants.
const (
	ProviderContainerlab = "containerlab"
	ProviderNetBox       = "netbox"
)

var supportedProviders = map[string]bool{
	ProviderContainerlab: true,
	ProviderNetBox:       true,
}

// CredentialConfig defines named credential provider instances.
type CredentialConfig struct {
	Provider map[string]CredentialProviderConfig `toml:"provider"`

	// Host and Group are legacy credential binding tables. Auth mappings now
	// live under inventory.host and inventory.group.
	Host  map[string]CredentialRefConfig `toml:"host"`
	Group map[string]CredentialRefConfig `toml:"group"`

	Type   string                         `toml:"type"`
	Config CredentialProviderDetailConfig `toml:"config"`
}

// CredentialProviderConfig configures one named credential provider instance.
type CredentialProviderConfig struct {
	Type   string                         `toml:"type"`
	Config CredentialProviderDetailConfig `toml:"config"`
}

// CredentialProviderDetailConfig holds backend-specific credential provider
// settings. Only the fields for the selected provider are used.
type CredentialProviderDetailConfig struct {
	Account string `toml:"account"`
	Vault   string `toml:"vault"`
	Command string `toml:"command"`
	Prefix  string `toml:"prefix"`
	Session string `toml:"session"`
}

// CredentialRefConfig maps a host or group credential scope to an existing
// external secret object. Ref is provider-specific but should be a stable item
// ID/name or a provider URI. Username can be literal or resolved from
// UsernameRef.
type CredentialRefConfig struct {
	Provider    string `toml:"provider"`
	Ref         string `toml:"ref"`
	Username    string `toml:"username"`
	UsernameRef string `toml:"username_ref"`
}

// InventoryConfig holds group metadata and external inventory provider config.
type InventoryConfig struct {
	Group    map[string]GroupConfig             `toml:"group"`
	Host     map[string]InventoryHostConfig     `toml:"host"`
	Provider map[string]InventoryProviderConfig `toml:"provider"`
}

// GroupConfig describes a logical inventory group.
type GroupConfig struct {
	DomainSuffix []string            `toml:"domain_suffix"`
	DefaultUser  string              `toml:"default_user"`
	Auth         InventoryAuthConfig `toml:"auth"`
}

// InventoryHostConfig stores host-level inventory metadata outside provider
// generated SSH config.
type InventoryHostConfig struct {
	Auth InventoryAuthConfig `toml:"auth"`
}

// InventoryAuthConfig maps an inventory host or group to a credential item.
type InventoryAuthConfig struct {
	Provider    string `toml:"provider"`
	Ref         string `toml:"ref"`
	Username    string `toml:"username"`
	UsernameRef string `toml:"username_ref"`
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
	Name     string              `toml:"name"`
	Group    string              `toml:"group"`
	AuthMode string              `toml:"auth_mode"`
	Match    InventoryRouteMatch `toml:"match"`
}

// InventoryRouteMatch is an open map of field names to allowed values.
type InventoryRouteMatch map[string][]string

var bareKeySafeName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Validate checks credential provider configuration.
func (c *CredentialConfig) Validate() error {
	if strings.TrimSpace(c.Type) != "" {
		return fmt.Errorf("credential.type is no longer supported; configure credential.provider instead")
	}
	if len(c.Host) > 0 {
		return fmt.Errorf("credential.host is no longer supported; configure inventory.host.<host>.auth instead")
	}
	if len(c.Group) > 0 {
		return fmt.Errorf("credential.group is no longer supported; configure inventory.group.<group>.auth instead")
	}

	zeroConfig := len(c.Provider) == 0
	if c.Provider == nil {
		c.Provider = make(map[string]CredentialProviderConfig)
	}
	if zeroConfig {
		c.Provider["pass-local"] = CredentialProviderConfig{
			Type: CredentialProviderPass,
			Config: CredentialProviderDetailConfig{
				Command: "pass",
				Prefix:  "nssh",
				Session: ProviderSessionExternal,
			},
		}
	}
	for name, provider := range c.Provider {
		if err := validateBareKeySafe("credential.provider", name); err != nil {
			return err
		}
		if err := provider.Validate(name); err != nil {
			return err
		}
		c.Provider[name] = provider
	}
	return nil
}

// Validate checks one credential provider instance.
func (c *CredentialProviderConfig) Validate(name string) error {
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	switch c.Type {
	case CredentialProviderPass:
		if strings.TrimSpace(c.Config.Command) == "" {
			c.Config.Command = "pass"
		}
		if strings.TrimSpace(c.Config.Prefix) == "" {
			c.Config.Prefix = "nssh"
		}
		if strings.TrimSpace(c.Config.Session) == "" {
			c.Config.Session = ProviderSessionExternal
		}
	case CredentialProvider1Password:
		if strings.TrimSpace(c.Config.Vault) == "" {
			return fmt.Errorf("credential.provider.%s.config.vault is required for %q", name, CredentialProvider1Password)
		}
		if strings.TrimSpace(c.Config.Session) == "" {
			c.Config.Session = ProviderSessionAgentOwned
		}
	case CredentialProviderBitwarden:
		if strings.TrimSpace(c.Config.Session) == "" {
			c.Config.Session = ProviderSessionExternal
		}
	default:
		return fmt.Errorf("unsupported credential provider %q", c.Type)
	}
	if err := validateProviderSessionPolicy("credential.provider."+name+".config.session", c.Config.Session); err != nil {
		return err
	}
	return nil
}

// Validate checks a credential reference mapping.
func (c *CredentialRefConfig) Validate(scope string, providers map[string]CredentialProviderConfig) error {
	c.Provider = strings.TrimSpace(c.Provider)
	if strings.TrimSpace(c.Ref) == "" {
		return fmt.Errorf("%s.ref is required", scope)
	}
	if strings.TrimSpace(c.Username) != "" && strings.TrimSpace(c.UsernameRef) != "" {
		return fmt.Errorf("%s.username and username_ref are mutually exclusive", scope)
	}
	if c.Provider == "" {
		return fmt.Errorf("%s.provider is required", scope)
	}
	if _, ok := providers[c.Provider]; !ok {
		return fmt.Errorf("%s.provider references unknown provider %q", scope, c.Provider)
	}
	return nil
}

func validateProviderSessionPolicy(scope, policy string) error {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case ProviderSessionExternal, ProviderSessionAgentOwned, ProviderSessionNone:
		return nil
	default:
		return fmt.Errorf("%s has invalid provider session policy %q", scope, policy)
	}
}

// Validate checks inventory group and provider configuration.
func (c *InventoryConfig) Validate() error {
	if c.Group == nil {
		c.Group = make(map[string]GroupConfig)
	}
	for name := range c.Group {
		if err := validateBareKeySafe("inventory.group", name); err != nil {
			return err
		}
		group := c.Group[name]
		group.DefaultUser = strings.TrimSpace(group.DefaultUser)
		if err := group.Auth.Validate("inventory.group." + name + ".auth"); err != nil {
			return err
		}
		c.Group[name] = group
	}

	for name, host := range c.Host {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("inventory.host has empty host")
		}
		if err := host.Auth.Validate("inventory.host." + name + ".auth"); err != nil {
			return err
		}
		c.Host[name] = host
	}

	for name := range c.Provider {
		provider := c.Provider[name]
		if err := validateBareKeySafe("inventory.provider", name); err != nil {
			return err
		}
		if err := provider.Validate(c.Group); err != nil {
			return fmt.Errorf("inventory.provider.%s: %w", name, err)
		}
	}

	return nil
}

// Validate checks one inventory auth mapping.
func (c *InventoryAuthConfig) Validate(scope string) error {
	c.Provider = strings.TrimSpace(c.Provider)
	c.Ref = strings.TrimSpace(c.Ref)
	c.Username = strings.TrimSpace(c.Username)
	c.UsernameRef = strings.TrimSpace(c.UsernameRef)
	if !c.IsSet() {
		return nil
	}
	if c.Ref == "" {
		return fmt.Errorf("%s.ref is required", scope)
	}
	if c.Provider == "" {
		return fmt.Errorf("%s.provider is required", scope)
	}
	if c.Username != "" && c.UsernameRef != "" {
		return fmt.Errorf("%s.username and username_ref are mutually exclusive", scope)
	}
	return nil
}

// IsSet reports whether the auth mapping contains any configured value.
func (c InventoryAuthConfig) IsSet() bool {
	return strings.TrimSpace(c.Provider) != "" ||
		strings.TrimSpace(c.Ref) != "" ||
		strings.TrimSpace(c.Username) != "" ||
		strings.TrimSpace(c.UsernameRef) != ""
}

// CredentialRef converts inventory auth metadata into provider ref metadata.
func (c InventoryAuthConfig) CredentialRef() CredentialRefConfig {
	return CredentialRefConfig(c)
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
		route.AuthMode = strings.ToLower(strings.TrimSpace(route.AuthMode))
		if route.AuthMode != "" && route.AuthMode != AuthModePassword && route.AuthMode != AuthModeKey {
			return fmt.Errorf("route[%d]: invalid auth_mode %q", i, route.AuthMode)
		}
		c.Route[i] = route
	}
	return nil
}

func validateBareKeySafe(scope, name string) error {
	if !bareKeySafeName.MatchString(name) {
		return fmt.Errorf("%s name %q must be TOML bare-key safe: letters, numbers, underscores, and dashes", scope, name)
	}
	return nil
}
