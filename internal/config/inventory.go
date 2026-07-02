package config

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	CredentialProviderSOPSAge   = "sops-age"
	CredentialProvider1Password = "1password"
	CredentialProviderBitwarden = "bitwarden"
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
	Provider map[string]CredentialProviderConfig `yaml:"provider,omitempty"`

	// Host and Group are legacy credential binding tables. Auth mappings now
	// live under inventory.host and inventory.provider.<provider>.group.
	Host  map[string]CredentialRefConfig `yaml:"host,omitempty"`
	Group map[string]CredentialRefConfig `yaml:"group,omitempty"`

	Type   string                         `yaml:"type,omitempty"`
	Config CredentialProviderDetailConfig `yaml:"config,omitempty"`
}

// CredentialProviderConfig configures one named credential provider instance.
type CredentialProviderConfig struct {
	Type    string `yaml:"type"`
	Account string `yaml:"account,omitempty"`
	Vault   string `yaml:"vault,omitempty"`
	Command string `yaml:"command,omitempty"`
	Prefix  string `yaml:"prefix,omitempty"`
	File    string `yaml:"file,omitempty"`

	AgeKeyFile  string `yaml:"age_key_file,omitempty"`
	WarmSession bool   `yaml:"warm_session,omitempty"`

	Keepalive         bool     `yaml:"keepalive,omitempty"`
	KeepaliveInterval Duration `yaml:"keepalive_interval,omitempty"`
	KeepaliveTimeout  Duration `yaml:"keepalive_timeout,omitempty"`

	Config CredentialProviderDetailConfig `yaml:"config,omitempty"`
}

// CredentialProviderDetailConfig holds backend-specific credential provider
// settings. Only the fields for the selected provider are used.
type CredentialProviderDetailConfig struct {
	Account string `yaml:"account,omitempty"`
	Vault   string `yaml:"vault,omitempty"`
	Command string `yaml:"command,omitempty"`
	Prefix  string `yaml:"prefix,omitempty"`
	File    string `yaml:"file,omitempty"`

	AgeKeyFile  string `yaml:"age_key_file,omitempty"`
	WarmSession bool   `yaml:"warm_session,omitempty"`

	Keepalive         bool     `yaml:"keepalive,omitempty"`
	KeepaliveInterval Duration `yaml:"keepalive_interval,omitempty"`
	KeepaliveTimeout  Duration `yaml:"keepalive_timeout,omitempty"`
}

// CredentialRefConfig maps a host or group credential scope to an existing
// external secret object. Ref is provider-specific but should be a stable item
// ID/name or a provider URI. Username can be literal or resolved from
// UsernameRef.
type CredentialRefConfig struct {
	Provider    string `yaml:"provider,omitempty"`
	Ref         string `yaml:"ref,omitempty"`
	Username    string `yaml:"username,omitempty"`
	UsernameRef string `yaml:"username_ref,omitempty"`
}

// InventoryConfig holds inventory defaults, host overrides, and provider config.
type InventoryConfig struct {
	Auth      InventoryAuthConfig                `yaml:"auth,omitempty"`
	Groups    map[string]GroupConfig             `yaml:"groups,omitempty"`
	Hosts     map[string]InventoryHostConfig     `yaml:"hosts,omitempty"`
	Providers map[string]InventoryProviderConfig `yaml:"providers,omitempty"`

	Group    map[string]GroupConfig             `yaml:"group,omitempty"`
	Host     map[string]InventoryHostConfig     `yaml:"host,omitempty"`
	Provider map[string]InventoryProviderConfig `yaml:"provider,omitempty"`
}

// InventoryHostConfig stores host-level inventory metadata and overrides.
type InventoryHostConfig struct {
	Group        string              `yaml:"group,omitempty"`
	Aliases      []string            `yaml:"aliases,omitempty"`
	Port         int                 `yaml:"port,omitempty"`
	Auth         InventoryAuthConfig `yaml:"auth,omitempty"`
	SSH          SSHHostConfig       `yaml:"ssh,omitempty"`
	Highlight    HighlightConfig     `yaml:"highlight,omitempty"`
	AuthDisabled bool                `yaml:"auth_disabled,omitempty"`
}

// InventoryAuthConfig maps an inventory host or group to a credential item.
type InventoryAuthConfig struct {
	Mode               string `yaml:"mode,omitempty"`
	CredentialProvider string `yaml:"credential_provider,omitempty"`
	PasswordRef        string `yaml:"password_ref,omitempty"`
	Username           string `yaml:"username,omitempty"`
	UsernameRef        string `yaml:"username_ref,omitempty"`
}

// InventoryProviderConfig configures one named external inventory provider.
type InventoryProviderConfig struct {
	Type      string                         `yaml:"type"`
	Auth      InventoryAuthConfig            `yaml:"auth,omitempty"`
	Config    InventoryProviderDetailConfig  `yaml:"config,omitempty"`
	Groups    map[string]GroupConfig         `yaml:"groups,omitempty"`
	Hosts     map[string]InventoryHostConfig `yaml:"hosts,omitempty"`
	Group     map[string]GroupConfig         `yaml:"group,omitempty"`
	Selectors []InventoryGroupSelector       `yaml:"-"`
}

// GroupConfig describes one provider-owned inventory group.
type GroupConfig struct {
	DomainSuffix []string            `yaml:"domain_suffix,omitempty"`
	Auth         InventoryAuthConfig `yaml:"auth,omitempty"`
	Match        InventoryMatch      `yaml:"match,omitempty"`
	SSH          SSHHostConfig       `yaml:"ssh,omitempty"`
	Highlight    HighlightConfig     `yaml:"highlight,omitempty"`
}

// InventoryProviderDetailConfig holds implementation-specific provider config.
type InventoryProviderDetailConfig struct {
	BaseURL               string                 `yaml:"base_url,omitempty"`
	URLEnv                string                 `yaml:"url_env,omitempty"`
	TokenEnv              string                 `yaml:"token_env,omitempty"`
	EnvFile               string                 `yaml:"env_file,omitempty"`
	JumpHost              string                 `yaml:"jump_host,omitempty"`
	Sudo                  bool                   `yaml:"sudo,omitempty"`
	StrictHostKeyChecking bool                   `yaml:"strict_host_key_checking,omitempty"`
	SSHDefaults           SSHDefaultsInheritance `yaml:"ssh_defaults,omitempty"`
}

type SSHDefaultsInheritance struct {
	Mode    string
	Options []string
}

func NewSSHDefaultsInheritanceMode(mode string) SSHDefaultsInheritance {
	return SSHDefaultsInheritance{Mode: mode}
}

func NewSSHDefaultsInheritanceOptions(options ...string) SSHDefaultsInheritance {
	return SSHDefaultsInheritance{Options: slices.Clone(options)}
}

func (v *SSHDefaultsInheritance) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		v.Mode = strings.TrimSpace(node.Value)
		v.Options = nil
		return nil
	case yaml.SequenceNode:
		options := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("ssh_defaults list entries must be option names")
			}
			options = append(options, item.Value)
		}
		v.Mode = ""
		v.Options = options
		return nil
	default:
		return fmt.Errorf("ssh_defaults must be a string or list")
	}
}

func (v SSHDefaultsInheritance) MarshalYAML() (any, error) {
	if len(v.Options) > 0 {
		return v.Options, nil
	}
	return v.Mode, nil
}

func (v SSHDefaultsInheritance) IsZero() bool {
	return strings.TrimSpace(v.Mode) == "" && len(v.Options) == 0
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
		c.Provider["sops"] = CredentialProviderConfig{
			Type: CredentialProviderSOPSAge,
			File: "~/.local/share/nssh/credentials.sops.yaml",
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
	c.syncDetailFields()
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	switch c.Type {
	case CredentialProviderSOPSAge:
		if c.WarmSession {
			return fmt.Errorf("credential.provider.%s.warm_session is only supported for %q", name, CredentialProviderBitwarden)
		}
		if c.Keepalive || c.KeepaliveInterval.Duration() != 0 || c.KeepaliveTimeout.Duration() != 0 {
			return fmt.Errorf("credential.provider.%s.keepalive is only supported for %q", name, CredentialProvider1Password)
		}
		if strings.TrimSpace(c.File) == "" {
			return fmt.Errorf("credential.provider.%s.config.file is required for %q", name, CredentialProviderSOPSAge)
		}
	case CredentialProvider1Password:
		if c.WarmSession {
			return fmt.Errorf("credential.provider.%s.warm_session is only supported for %q", name, CredentialProviderBitwarden)
		}
		if strings.TrimSpace(c.Vault) == "" {
			return fmt.Errorf("credential.provider.%s.config.vault is required for %q", name, CredentialProvider1Password)
		}
		if c.KeepaliveInterval.Duration() == 0 {
			c.KeepaliveInterval = Duration(5 * time.Minute)
		}
		if c.KeepaliveTimeout.Duration() == 0 {
			c.KeepaliveTimeout = Duration(10 * time.Second)
		}
		if c.KeepaliveInterval.Duration() < time.Minute {
			return fmt.Errorf("credential.provider.%s.keepalive_interval must be >= 1m (got %v)", name, c.KeepaliveInterval.Duration())
		}
		if c.KeepaliveInterval.Duration() > 9*time.Minute {
			return fmt.Errorf("credential.provider.%s.keepalive_interval must be <= 9m (got %v)", name, c.KeepaliveInterval.Duration())
		}
		if c.KeepaliveTimeout.Duration() < time.Second {
			return fmt.Errorf("credential.provider.%s.keepalive_timeout must be >= 1s (got %v)", name, c.KeepaliveTimeout.Duration())
		}
	case CredentialProviderBitwarden:
		if c.Keepalive || c.KeepaliveInterval.Duration() != 0 || c.KeepaliveTimeout.Duration() != 0 {
			return fmt.Errorf("credential.provider.%s.keepalive is only supported for %q", name, CredentialProvider1Password)
		}
	default:
		return fmt.Errorf("unsupported credential provider %q", c.Type)
	}
	c.syncConfigFields()
	return nil
}

func (c *CredentialProviderConfig) syncDetailFields() {
	if c.Account == "" {
		c.Account = c.Config.Account
	}
	if c.Vault == "" {
		c.Vault = c.Config.Vault
	}
	if c.Command == "" {
		c.Command = c.Config.Command
	}
	if c.Prefix == "" {
		c.Prefix = c.Config.Prefix
	}
	if c.File == "" {
		c.File = c.Config.File
	}
	if c.AgeKeyFile == "" {
		c.AgeKeyFile = c.Config.AgeKeyFile
	}
	if !c.WarmSession {
		c.WarmSession = c.Config.WarmSession
	}
	if !c.Keepalive {
		c.Keepalive = c.Config.Keepalive
	}
	if c.KeepaliveInterval.Duration() == 0 {
		c.KeepaliveInterval = c.Config.KeepaliveInterval
	}
	if c.KeepaliveTimeout.Duration() == 0 {
		c.KeepaliveTimeout = c.Config.KeepaliveTimeout
	}
}

func (c *CredentialProviderConfig) syncConfigFields() {
	c.Config.Account = c.Account
	c.Config.Vault = c.Vault
	c.Config.Command = c.Command
	c.Config.Prefix = c.Prefix
	c.Config.File = c.File
	c.Config.AgeKeyFile = c.AgeKeyFile
	c.Config.WarmSession = c.WarmSession
	c.Config.Keepalive = c.Keepalive
	c.Config.KeepaliveInterval = c.KeepaliveInterval
	c.Config.KeepaliveTimeout = c.KeepaliveTimeout
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
	c.syncAliasFields()
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
		provider.syncAliasFields()
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
		for hostName, host := range provider.Hosts {
			if strings.TrimSpace(hostName) == "" {
				return fmt.Errorf("inventory.provider.%s.hosts has empty host", name)
			}
			if host.AuthDisabled && host.Auth.IsSet() {
				return fmt.Errorf("inventory.provider.%s.hosts.%s cannot set auth and auth_disabled", name, hostName)
			}
			if err := host.Auth.Validate("inventory.provider." + name + ".hosts." + hostName + ".auth"); err != nil {
				return err
			}
			provider.Hosts[hostName] = host
		}
		provider.Groups = provider.Group
		c.Provider[name] = provider
		c.Providers[name] = provider
	}

	return nil
}

// Validate checks one inventory auth mapping.
func (c *InventoryAuthConfig) Validate(scope string) error {
	c.Normalize()
	if c.Mode != "" && c.Mode != AuthModePassword && c.Mode != AuthModeKey {
		return fmt.Errorf("%s.mode has invalid value %q", scope, c.Mode)
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
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	c.CredentialProvider = strings.TrimSpace(c.CredentialProvider)
	c.PasswordRef = strings.TrimSpace(c.PasswordRef)
	c.Username = strings.TrimSpace(c.Username)
	c.UsernameRef = strings.TrimSpace(c.UsernameRef)
}

// IsSet reports whether the auth mapping contains any configured value.
func (c InventoryAuthConfig) IsSet() bool {
	return strings.TrimSpace(c.CredentialProvider) != "" ||
		strings.TrimSpace(c.PasswordRef) != "" ||
		strings.TrimSpace(c.Username) != "" ||
		strings.TrimSpace(c.UsernameRef) != "" ||
		strings.TrimSpace(c.Mode) != ""
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
	c.syncAliasFields()
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	if c.Type == "" {
		return fmt.Errorf("type is required")
	}
	if !supportedProviders[c.Type] {
		return fmt.Errorf("unsupported provider %q", c.Type)
	}
	if err := c.Config.validate(c.Type); err != nil {
		return err
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

func (c *InventoryProviderDetailConfig) validate(providerType string) error {
	sshDefaults := strings.ToLower(strings.TrimSpace(c.SSHDefaults.Mode))
	if !c.SSHDefaults.IsZero() {
		if providerType != ProviderContainerlab {
			return fmt.Errorf("%s.config.ssh_defaults is only supported for containerlab providers", providerType)
		}
	}
	switch sshDefaults {
	case "", "none", "all":
	default:
		return fmt.Errorf("containerlab.config.ssh_defaults must be all, none, or a list of option names")
	}
	for _, option := range c.SSHDefaults.Options {
		if strings.TrimSpace(option) == "" {
			return fmt.Errorf("containerlab.config.ssh_defaults list must contain only non-empty option names")
		}
	}
	return nil
}

func (c *InventoryConfig) syncAliasFields() {
	if c.Providers == nil && c.Provider != nil {
		c.Providers = c.Provider
	}
	if c.Provider == nil && c.Providers != nil {
		c.Provider = c.Providers
	}
	if c.Providers == nil {
		c.Providers = make(map[string]InventoryProviderConfig)
	}
	if c.Provider == nil {
		c.Provider = c.Providers
	}
	if c.Groups == nil && c.Group != nil {
		c.Groups = c.Group
	}
	if c.Group == nil && c.Groups != nil {
		c.Group = c.Groups
	}
	if c.Hosts == nil && c.Host != nil {
		c.Hosts = c.Host
	}
	if c.Host == nil && c.Hosts != nil {
		c.Host = c.Hosts
	}
	for name, provider := range c.Provider {
		provider.syncAliasFields()
		c.Provider[name] = provider
		c.Providers[name] = provider
	}
	for name, provider := range c.Providers {
		provider.syncAliasFields()
		c.Providers[name] = provider
		c.Provider[name] = provider
	}
}

func (c *InventoryProviderConfig) syncAliasFields() {
	if c.Groups == nil && c.Group != nil {
		c.Groups = c.Group
	}
	if c.Group == nil && c.Groups != nil {
		c.Group = c.Groups
	}
	if c.Groups == nil {
		c.Groups = make(map[string]GroupConfig)
	}
	if c.Group == nil {
		c.Group = c.Groups
	}
	if c.Hosts == nil {
		c.Hosts = make(map[string]InventoryHostConfig)
	}
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
	c.syncSchemaAliases()
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
	if provider, ok := c.Inventory.Provider[providerName]; ok {
		if host, ok := provider.Hosts[ctx.Host]; ok {
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
	if auth.Mode != "" {
		res.AuthMode = auth.Mode
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
		return fmt.Errorf("%s name %q must use only letters, numbers, underscores, and dashes", scope, name)
	}
	return nil
}
