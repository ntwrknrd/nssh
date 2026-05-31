package config

import (
	"fmt"
	"regexp"
	"sort"
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
	ProviderLocal        = "local"
	ProviderContainerlab = "containerlab"
	ProviderNetBox       = "netbox"
)

var supportedProviders = map[string]bool{
	ProviderLocal:        true,
	ProviderContainerlab: true,
	ProviderNetBox:       true,
}

// CredentialConfig defines named credential provider instances.
type CredentialConfig struct {
	Provider map[string]CredentialProviderConfig `toml:"provider"`

	// Host and Group are legacy credential binding tables. Auth mappings now
	// live under inventory.host and inventory.provider.<provider>.group.
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

// InventoryConfig holds inventory defaults, host overrides, and provider config.
type InventoryConfig struct {
	Auth     InventoryAuthConfig                `toml:"auth"`
	Group    map[string]GroupConfig             `toml:"-"`
	Host     map[string]InventoryHostConfig     `toml:"host"`
	Provider map[string]InventoryProviderConfig `toml:"provider"`
}

// InventoryHostConfig stores host-level inventory metadata outside provider
// generated SSH config.
type InventoryHostConfig struct {
	AuthDisabled bool                `toml:"auth_disabled"`
	Auth         InventoryAuthConfig `toml:"auth"`
}

// InventoryAuthConfig maps an inventory host or group to a credential item.
type InventoryAuthConfig struct {
	CredentialProvider string `toml:"credential_provider"`
	PasswordRef        string `toml:"password_ref"`
	Username           string `toml:"username"`
	UsernameRef        string `toml:"username_ref"`
	AuthMode           string `toml:"auth_mode"`
}

// InventoryProviderConfig configures one named external inventory provider.
type InventoryProviderConfig struct {
	Type      string                        `toml:"type"`
	Auth      InventoryAuthConfig           `toml:"auth"`
	Config    InventoryProviderDetailConfig `toml:"config"`
	Group     map[string]GroupConfig        `toml:"group"`
	Selectors []InventoryGroupSelector      `toml:"-"`
}

// GroupConfig describes one provider-owned inventory group.
type GroupConfig struct {
	DomainSuffix []string            `toml:"domain_suffix"`
	Auth         InventoryAuthConfig `toml:"auth"`
	Match        InventoryMatch      `toml:"match"`
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

type InventoryAuthContext struct {
	Host     string
	Group    string
	Provider string
}

type InventoryAuthResolution struct {
	CredentialProvider string
	PasswordRef        string
	Username           string
	UsernameRef        string
	AuthMode           string
	Disabled           bool
	Source             string
	UsernameSource     string
	PasswordSource     string
	AuthModeSource     string
}

// InventoryGroupSelector is a compiled selector for one provider-backed group.
type InventoryGroupSelector struct {
	Group    string
	Provider string
	Match    InventoryMatch
}

// InventoryMatch is an open map of field names to allowed values.
type InventoryMatch map[string][]string

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
		return fmt.Errorf("credential.group is no longer supported; configure inventory.provider.<provider>.group.<group>.auth instead")
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

// FormatInventoryGroupID returns the canonical public group identifier.
func FormatInventoryGroupID(provider, group string) string {
	provider = strings.TrimSpace(provider)
	group = strings.TrimSpace(group)
	if provider == "" || group == "" {
		return strings.Trim(provider+"/"+group, "/")
	}
	return provider + "/" + group
}

// ParseInventoryGroupID splits a canonical public group identifier.
func ParseInventoryGroupID(id string) (string, string, error) {
	raw := id
	id = strings.TrimSpace(id)
	if raw != id {
		return "", "", fmt.Errorf("group must be provider-qualified as <provider>/<group>")
	}
	parts := strings.Split(id, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("group must be provider-qualified as <provider>/<group>")
	}
	provider := parts[0]
	group := parts[1]
	if provider != strings.TrimSpace(provider) || group != strings.TrimSpace(group) {
		return "", "", fmt.Errorf("group must be provider-qualified as <provider>/<group>")
	}
	if err := validateBareKeySafe("inventory provider", provider); err != nil {
		return "", "", err
	}
	if err := validateBareKeySafe("inventory group", group); err != nil {
		return "", "", err
	}
	return provider, group, nil
}

// Validate checks inventory group and provider configuration.
func (c *InventoryConfig) Validate() error {
	c.Auth.Normalize()
	if err := c.Auth.Validate("inventory.auth"); err != nil {
		return err
	}
	if len(c.Group) > 0 {
		if c.Provider == nil {
			c.Provider = make(map[string]InventoryProviderConfig)
		}
		localProvider := c.Provider[ProviderLocal]
		localProvider.Type = ProviderLocal
		if localProvider.Group == nil {
			localProvider.Group = make(map[string]GroupConfig)
		}
		for groupName, group := range c.Group {
			if _, exists := localProvider.Group[groupName]; !exists {
				localProvider.Group[groupName] = group
			}
		}
		c.Provider[ProviderLocal] = localProvider
	}

	for name, host := range c.Host {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("inventory.host has empty host")
		}
		if host.AuthDisabled && host.Auth.IsSet() {
			return fmt.Errorf("inventory.host.%s cannot set auth and auth_disabled", name)
		}
		if err := host.Auth.Validate("inventory.host." + name + ".auth"); err != nil {
			return err
		}
		c.Host[name] = host
	}

	for name := range c.Provider {
		provider := c.Provider[name]
		provider.Auth.Normalize()
		if err := validateBareKeySafe("inventory.provider", name); err != nil {
			return err
		}
		if err := provider.Auth.Validate("inventory.provider." + name + ".auth"); err != nil {
			return err
		}
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("inventory.provider.%s: %w", name, err)
		}
		if provider.Group == nil {
			provider.Group = make(map[string]GroupConfig)
		}
		for groupName, group := range provider.Group {
			if err := validateBareKeySafe("inventory.provider."+name+".group", groupName); err != nil {
				return err
			}
			group.Auth.Normalize()
			if err := group.Auth.Validate("inventory.provider." + name + ".group." + groupName + ".auth"); err != nil {
				return err
			}
			provider.Group[groupName] = group
		}
		c.Provider[name] = provider
	}

	return nil
}

// Validate checks one inventory auth mapping.
func (c *InventoryAuthConfig) Validate(scope string) error {
	c.Normalize()
	if c.AuthMode != "" && c.AuthMode != AuthModePassword && c.AuthMode != AuthModeKey {
		return fmt.Errorf("%s.auth_mode has invalid value %q", scope, c.AuthMode)
	}
	c.Username = strings.TrimSpace(c.Username)
	c.UsernameRef = strings.TrimSpace(c.UsernameRef)
	if !c.IsSet() {
		return nil
	}
	if c.PasswordRef != "" && c.CredentialProvider == "" {
		return fmt.Errorf("%s.credential_provider is required", scope)
	}
	if c.CredentialProvider != "" && c.PasswordRef == "" && c.UsernameRef == "" {
		return fmt.Errorf("%s.password_ref or username_ref is required", scope)
	}
	if c.Username != "" && c.UsernameRef != "" {
		return fmt.Errorf("%s.username and username_ref are mutually exclusive", scope)
	}
	return nil
}

func (c *InventoryAuthConfig) Normalize() {
	c.CredentialProvider = strings.TrimSpace(c.CredentialProvider)
	c.PasswordRef = strings.TrimSpace(c.PasswordRef)
	c.Username = strings.TrimSpace(c.Username)
	c.UsernameRef = strings.TrimSpace(c.UsernameRef)
	c.AuthMode = strings.ToLower(strings.TrimSpace(c.AuthMode))
}

// IsSet reports whether the auth mapping contains any configured value.
func (c InventoryAuthConfig) IsSet() bool {
	return strings.TrimSpace(c.CredentialProvider) != "" ||
		strings.TrimSpace(c.PasswordRef) != "" ||
		strings.TrimSpace(c.Username) != "" ||
		strings.TrimSpace(c.UsernameRef) != "" ||
		strings.TrimSpace(c.AuthMode) != ""
}

// CredentialRef converts inventory auth metadata into provider ref metadata.
func (c InventoryAuthConfig) CredentialRef() CredentialRefConfig {
	c.Normalize()
	return CredentialRefConfig{
		Provider:    c.CredentialProvider,
		Ref:         c.PasswordRef,
		Username:    c.Username,
		UsernameRef: c.UsernameRef,
	}
}

// Validate checks a single inventory provider.
func (c *InventoryProviderConfig) Validate() error {
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	if c.Type == "" {
		return fmt.Errorf("type is required")
	}
	if !supportedProviders[c.Type] {
		return fmt.Errorf("unsupported provider %q", c.Type)
	}

	switch c.Type {
	case ProviderLocal:
		// Local inventory is backed by the human-owned SSH include file.
	case ProviderContainerlab:
		if strings.TrimSpace(c.Config.JumpHost) == "" {
			return fmt.Errorf("containerlab.config.jump_host is required")
		}
	case ProviderNetBox:
		// NetBox supports base_url directly or environment-backed URL config.
	}

	return nil
}

// ProviderSelectors returns group selectors that target provider.
func (c InventoryConfig) ProviderSelectors(provider string) []InventoryGroupSelector {
	providerCfg, ok := c.Provider[provider]
	if !ok {
		return nil
	}
	groupNames := make([]string, 0, len(providerCfg.Group))
	for group := range providerCfg.Group {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	out := make([]InventoryGroupSelector, 0, len(groupNames))
	for _, groupName := range groupNames {
		group := providerCfg.Group[groupName]
		out = append(out, InventoryGroupSelector{
			Group:    FormatInventoryGroupID(provider, groupName),
			Provider: provider,
			Match:    group.Match,
		})
	}
	return out
}

func (c InventoryConfig) ProviderGroup(provider, group string) (GroupConfig, bool) {
	providerCfg, ok := c.Provider[provider]
	if !ok {
		if provider == ProviderLocal {
			groupCfg, ok := c.Group[group]
			return groupCfg, ok
		}
		return GroupConfig{}, false
	}
	groupCfg, ok := providerCfg.Group[group]
	if !ok && provider == ProviderLocal {
		groupCfg, ok = c.Group[group]
	}
	return groupCfg, ok
}

func (c *Config) ResolveInventoryAuth(ctx InventoryAuthContext) InventoryAuthResolution {
	if c == nil {
		c = DefaultConfig()
	}
	var res InventoryAuthResolution
	applyAuth(&res, c.Inventory.Auth, "inventory default")
	providerName := ctx.Provider
	groupName := ctx.Group
	if parsedProvider, parsedGroup, err := ParseInventoryGroupID(ctx.Group); err == nil {
		providerName = parsedProvider
		groupName = parsedGroup
	}
	if provider, ok := c.Inventory.Provider[providerName]; ok {
		applyAuth(&res, provider.Auth, "provider "+providerName)
	}
	if group, ok := c.Inventory.ProviderGroup(providerName, groupName); ok {
		applyAuth(&res, group.Auth, "group "+FormatInventoryGroupID(providerName, groupName))
	}
	if host, ok := c.Inventory.Host[ctx.Host]; ok {
		if host.AuthDisabled {
			res.Disabled = true
			res.Source = "disabled"
			res.CredentialProvider = ""
			res.PasswordRef = ""
			res.PasswordSource = "disabled"
		} else {
			applyAuth(&res, host.Auth, "host "+ctx.Host)
		}
	}
	return res
}

func applyAuth(res *InventoryAuthResolution, auth InventoryAuthConfig, source string) {
	auth.Normalize()
	if auth.CredentialProvider != "" {
		res.CredentialProvider = auth.CredentialProvider
		res.PasswordSource = source
		res.Source = source
	}
	if auth.PasswordRef != "" {
		res.PasswordRef = auth.PasswordRef
		res.PasswordSource = source
		res.Source = source
	}
	if auth.Username != "" {
		res.Username = auth.Username
		res.UsernameRef = ""
		res.UsernameSource = source
		res.Source = source
	}
	if auth.UsernameRef != "" {
		res.UsernameRef = auth.UsernameRef
		res.Username = ""
		res.UsernameSource = source
		res.Source = source
	}
	if auth.AuthMode != "" {
		res.AuthMode = auth.AuthMode
		res.AuthModeSource = source
		res.Source = source
		if auth.PasswordRef == "" {
			res.CredentialProvider = ""
			res.PasswordRef = ""
			res.PasswordSource = source
		}
	}
}

func validateBareKeySafe(scope, name string) error {
	if !bareKeySafeName.MatchString(name) {
		return fmt.Errorf("%s name %q must be TOML bare-key safe: letters, numbers, underscores, and dashes", scope, name)
	}
	return nil
}
