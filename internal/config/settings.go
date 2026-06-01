package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the root configuration structure loaded from config.toml.
type Config struct {
	Agent      AgentConfig      `toml:"agent"`
	Credential CredentialConfig `toml:"credential"`
	Host       HostConfig       `toml:"host"`
	Inventory  InventoryConfig  `toml:"inventory"`
	Logging    LoggingConfig    `toml:"logging"`
	SSH        SSHConfig        `toml:"ssh"`

	document *configDocument
}

// ============================================================================
// Agent Configuration
// ============================================================================

// AgentConfig holds agent daemon lifecycle settings.
type AgentConfig struct {
	// AutoStart starts the runtime agent on first provider-session request (default true)
	AutoStart bool `toml:"auto_start"`
	// IdleTimeout is how long the agent waits without activity before terminating (default 1h)
	IdleTimeout Duration `toml:"idle_timeout"`
	// ActivityIncrement is how much to extend the idle deadline on activity (default 15m)
	ActivityIncrement Duration `toml:"activity_increment"`
	// MaxLifetime is the maximum lifetime of the agent regardless of activity (default 24h)
	MaxLifetime Duration `toml:"max_lifetime"`
}

// ============================================================================
// Host Configuration
// ============================================================================

// HostConfig holds host-related settings.
type HostConfig struct {
	Defaults HostDefaultsConfig `toml:"defaults"`
}

// HostDefaultsConfig holds default values for new hosts.
type HostDefaultsConfig struct {
	// DefaultContext is deprecated; group placement is configured under inventory.
	DefaultContext string `toml:"default_context"`
}

// ============================================================================
// Logging Configuration
// ============================================================================

// LoggingConfig holds audit and session logging settings.
type LoggingConfig struct {
	Audit   AuditConfig   `toml:"audit"`
	Session SessionConfig `toml:"session"`
}

// AuditConfig holds security event logging settings.
type AuditConfig struct {
	// Enabled enables security event logging to file (default: true)
	Enabled bool `toml:"enabled"`
	// MaxSize is the max audit log size before rotation (default: "10MB")
	MaxSize string `toml:"max_size"`
}

// SessionConfig holds session recording settings.
type SessionConfig struct {
	// AppendMode appends to daily session file vs creating separate files
	AppendMode *bool `toml:"append_mode"`
	// AsciinemaServer is a custom asciinema server URL for self-hosted instances
	AsciinemaServer string `toml:"asciinema_server_url"`
	// Dir is the recording storage directory
	Dir string `toml:"dir"`
	// Enabled enables automatic session recording
	Enabled *bool `toml:"enabled"`
	// ExcludeHosts are patterns for hosts to never record
	ExcludeHosts []string `toml:"exclude_hosts"`
	// IdleTimeLimit caps long pauses in recordings (seconds, 0 = disabled)
	IdleTimeLimit float64 `toml:"idle_time_limit"`
	// IdleTimeLimitMode is when to apply idle time limit: "play", "record", or "both"
	IdleTimeLimitMode string `toml:"idle_time_limit_mode"`
	// IncludeHosts are patterns for hosts to record (takes precedence over exclude)
	IncludeHosts []string `toml:"include_hosts"`
	// TitleFormat is a template for recording titles with {host}, {user}, {date}, {time}
	TitleFormat string `toml:"title_format"`
	// WindowSize is fixed terminal dimensions for recordings (cols x rows)
	WindowSize string `toml:"window_size"`
	// AutoExportTxt automatically exports recordings to plain text (.txt) when session ends
	AutoExportTxt bool `toml:"auto_export_txt"`
	// Archive holds automatic archival settings
	Archive SessionArchiveConfig `toml:"archive"`
}

// SessionArchiveConfig holds automatic session archival settings.
type SessionArchiveConfig struct {
	// Dir is where monthly archives are stored (default: ~/.local/state/nssh/archives)
	Dir string `toml:"dir"`
	// Enabled enables automatic recording archiving (default: false)
	Enabled bool `toml:"enabled"`
	// Jitter introduces +/- jitter to the daily schedule (default: 30m)
	Jitter Duration `toml:"jitter"`
	// MaxBundles is how many monthly bundles to retain (default: 12)
	MaxBundles int `toml:"max_bundles"`
	// MaxRunBytes caps bytes processed per maintenance run (default: 0 = unlimited)
	MaxRunBytes int64 `toml:"max_run_bytes"`
	// MinAge is how old a .cast file must be before archiving (default: 30d)
	MinAge Duration `toml:"min_age"`
}

// ============================================================================
// SSH Configuration
// ============================================================================

// SSHConfig holds SSH connection and security settings.
type SSHConfig struct {
	Connection SSHConnectionConfig `toml:"connection"`
	Security   SSHSecurityConfig   `toml:"security"`
}

// SSHConnectionConfig holds timeout settings for SSH connections.
type SSHConnectionConfig struct {
	// IdleTimeout disconnects after inactivity (0 = disabled)
	IdleTimeout Duration `toml:"idle_timeout"`
	// PasswordTimeout is the password prompt timeout (default: 10s)
	PasswordTimeout Duration `toml:"password_timeout"`
	// Timeout is the SSH connection timeout (default: 30s)
	Timeout Duration `toml:"timeout"`
}

// SSHSecurityConfig holds SSH host key policy settings.
type SSHSecurityConfig struct {
	// AcceptOnceMode controls how host-key "Accept once" behaves: "pin" (default) or "accept-new"
	AcceptOnceMode string `toml:"accept_once_mode"`
	// CompatPersistProbes controls whether SSH compatibility probes write to known_hosts
	CompatPersistProbes bool `toml:"compat_persist_probes"`
	// HostKeyPolicy is a higher-level preset: "pin" (default) or "tofu"
	HostKeyPolicy string `toml:"host_key_policy"`
}

// ============================================================================
// Duration Type
// ============================================================================

// Duration wraps time.Duration for TOML string parsing.
// Supports formats like "30s", "5m", "1h".
type Duration time.Duration

// UnmarshalText implements encoding.TextUnmarshaler for Duration.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalText implements encoding.TextMarshaler for Duration.
// This ensures durations are written as strings like "30s" instead of integers.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// ============================================================================
// Default Configuration
// ============================================================================

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	paths := DefaultPaths()
	return &Config{
		Agent: AgentConfig{
			AutoStart:         true,
			IdleTimeout:       Duration(1 * time.Hour),
			ActivityIncrement: Duration(15 * time.Minute),
			MaxLifetime:       Duration(24 * time.Hour),
		},
		Host: HostConfig{
			Defaults: HostDefaultsConfig{
				DefaultContext: "",
			},
		},
		Credential: CredentialConfig{
			Provider: map[string]CredentialProviderConfig{
				"pass-local": {
					Type: CredentialProviderPass,
					Config: CredentialProviderDetailConfig{
						Command: "pass",
						Prefix:  "nssh",
						Session: ProviderSessionExternal,
					},
				},
			},
		},
		Inventory: InventoryConfig{
			Auth: InventoryAuthConfig{},
			Provider: map[string]InventoryProviderConfig{
				ProviderLocal: {
					Type: ProviderLocal,
					Group: map[string]GroupConfig{
						"default": {
							Auth: InventoryAuthConfig{
								CredentialProvider: "pass-local",
								PasswordRef:        "nssh/groups/default",
							},
						},
					},
				},
			},
		},
		Logging: LoggingConfig{
			Audit: AuditConfig{
				Enabled: true,
				MaxSize: "10MB",
			},
			Session: SessionConfig{
				Archive: SessionArchiveConfig{
					Dir:         filepath.Join(paths.StateDir, "archives"),
					Enabled:     false,
					Jitter:      Duration(30 * time.Minute),
					MaxBundles:  12,
					MaxRunBytes: 0,
					MinAge:      Duration(30 * 24 * time.Hour),
				},
			},
		},
		SSH: SSHConfig{
			Connection: SSHConnectionConfig{
				IdleTimeout:     0, // Disabled
				PasswordTimeout: Duration(10 * time.Second),
				Timeout:         Duration(30 * time.Second),
			},
			Security: SSHSecurityConfig{
				AcceptOnceMode:      "pin",
				CompatPersistProbes: false,
				HostKeyPolicy:       "pin",
			},
		},
	}
}

// ============================================================================
// Load and Save
// ============================================================================

// Load reads and parses the config file, merging with defaults.
// Returns default config if file doesn't exist.
// Environment variables override config file values:
//   - NSSH_AGENT_IDLE_TIMEOUT: agent idle timeout (e.g., "15m", "1h")
//   - NSSH_AGENT_ACTIVITY_INCREMENT: activity increment for idle extension (e.g., "15m")
//   - NSSH_AGENT_MAX_LIFETIME: agent max lifetime (e.g., "24h", "8h")
//   - NSSH_ACCEPT_ONCE_MODE: accept once mode ("pin" or "accept-new")
//   - NSSH_HOST_KEY_POLICY: host key policy ("pin" or "tofu")
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
		applyEnvOverrides(cfg)
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("validate config: %w", err)
		}
		return cfg, nil
	}

	doc, err := loadConfigDocument(path)
	if err != nil {
		return nil, err
	}
	if err := decodeConfigDocument(path, doc, cfg); err != nil {
		return nil, err
	}
	migrateLegacyIdentityConfig(cfg)
	pruneImplicitCredentialDefaults(doc.effective, cfg)
	pruneImplicitInventoryDefaults(doc.effective, cfg)

	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %s: %w", path, err)
	}
	cfg.document = doc

	return cfg, nil
}

func pruneImplicitCredentialDefaults(table map[string]any, cfg *Config) {
	if cfg == nil || cfg.Credential.Provider == nil {
		return
	}
	configDefinesProviders := tablePathDefined(table, "credential", "provider")
	passLocalExplicit := tablePathDefined(table, "credential", "provider", "pass-local")
	if configDefinesProviders && !passLocalExplicit {
		delete(cfg.Credential.Provider, "pass-local")
		if defaultGroup, ok := cfg.Inventory.Provider[ProviderLocal].Group["default"]; ok &&
			!tablePathDefined(table, "inventory", "provider", ProviderLocal, "group", "default", "auth") &&
			defaultGroup.Auth.CredentialProvider == "pass-local" {
			defaultGroup.Auth = InventoryAuthConfig{}
			localProvider := cfg.Inventory.Provider[ProviderLocal]
			localProvider.Group["default"] = defaultGroup
			cfg.Inventory.Provider[ProviderLocal] = localProvider
		}
	}
}

func migrateLegacyIdentityConfig(cfg *Config) {}

func pruneImplicitInventoryDefaults(table map[string]any, cfg *Config) {
	if cfg == nil || cfg.Inventory.Provider == nil {
		return
	}
	defaultGroupExplicit := tablePathDefined(table, "inventory", "provider", ProviderLocal, "group", "default")
	configDefinesGroups := tablePathDefined(table, "inventory", "provider")
	if !defaultGroupExplicit && configDefinesGroups {
		if localProvider, ok := cfg.Inventory.Provider[ProviderLocal]; ok {
			delete(localProvider.Group, "default")
			cfg.Inventory.Provider[ProviderLocal] = localProvider
		}
	}
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("NSSH_AGENT_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Agent.IdleTimeout = Duration(d)
		}
	}
	if v := os.Getenv("NSSH_AGENT_ACTIVITY_INCREMENT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Agent.ActivityIncrement = Duration(d)
		}
	}
	if v := os.Getenv("NSSH_AGENT_MAX_LIFETIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Agent.MaxLifetime = Duration(d)
		}
	}
	if v := os.Getenv("NSSH_ACCEPT_ONCE_MODE"); v != "" {
		cfg.SSH.Security.AcceptOnceMode = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("NSSH_HOST_KEY_POLICY"); v != "" {
		cfg.SSH.Security.HostKeyPolicy = strings.ToLower(strings.TrimSpace(v))
	}
}

// LoadDefault loads config from the default path.
func LoadDefault() (*Config, error) {
	return Load(DefaultPaths().ConfigFile)
}

// Save writes the config to the specified path.
func Save(path string, cfg *Config) error {
	return SaveSparse(path, cfg)
}

// ============================================================================
// Validation
// ============================================================================

// Validate checks Config values are within acceptable bounds.
func (c *Config) Validate() error {
	if err := c.Agent.Validate(); err != nil {
		return err
	}
	if err := c.Credential.Validate(); err != nil {
		return err
	}
	if err := c.Inventory.Validate(); err != nil {
		return err
	}
	if err := c.validateInventoryAuthProviders(); err != nil {
		return err
	}
	if err := c.Logging.Audit.Validate(); err != nil {
		return err
	}
	if err := c.Logging.Session.Archive.Validate(); err != nil {
		return err
	}
	if err := c.SSH.Security.Validate(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateInventoryAuthProviders() error {
	if err := validateInventoryAuthProvider("inventory.auth", c.Inventory.Auth, c.Credential); err != nil {
		return err
	}
	for host, cfg := range c.Inventory.Host {
		if err := validateInventoryAuthProvider("inventory.host."+host+".auth", cfg.Auth, c.Credential); err != nil {
			return err
		}
	}
	for providerName, provider := range c.Inventory.Provider {
		if err := validateInventoryAuthProvider("inventory.provider."+providerName+".auth", provider.Auth, c.Credential); err != nil {
			return err
		}
		for groupName, group := range provider.Group {
			if err := validateInventoryAuthProvider("inventory.provider."+providerName+".group."+groupName+".auth", group.Auth, c.Credential); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateInventoryAuthProvider(scope string, auth InventoryAuthConfig, credential CredentialConfig) error {
	if !auth.IsSet() {
		return nil
	}
	auth.Normalize()
	if auth.CredentialProvider == "" && auth.PasswordRef == "" {
		return nil
	}
	provider := strings.TrimSpace(auth.CredentialProvider)
	if provider == "" {
		return fmt.Errorf("%s.credential_provider is required", scope)
	}
	if _, ok := credential.Provider[provider]; !ok {
		return fmt.Errorf("%s.credential_provider references unknown provider %q", scope, provider)
	}
	return nil
}

func legacySyncSourcesError() error {
	return fmt.Errorf("sync.sources is no longer supported; configure inventory.provider instead")
}

// Validate checks AgentConfig values are within acceptable bounds.
func (c *AgentConfig) Validate() error {
	idleTimeout := c.IdleTimeout.Duration()
	activityIncrement := c.ActivityIncrement.Duration()
	maxLifetime := c.MaxLifetime.Duration()

	// Validate idle timeout (min 1s, max 24h)
	if idleTimeout < time.Second {
		return fmt.Errorf("agent.idle_timeout must be >= 1s (got %v)", idleTimeout)
	}
	if idleTimeout > 24*time.Hour {
		return fmt.Errorf("agent.idle_timeout must be <= 24h (got %v)", idleTimeout)
	}

	// Validate activity increment (min 1s, must not exceed idle_timeout)
	if activityIncrement < time.Second {
		return fmt.Errorf("agent.activity_increment must be >= 1s (got %v)", activityIncrement)
	}
	if activityIncrement > idleTimeout {
		return fmt.Errorf("agent.activity_increment (%v) must be <= agent.idle_timeout (%v)", activityIncrement, idleTimeout)
	}

	// Validate max lifetime (min 1m, max 168h/7 days)
	if maxLifetime < time.Minute {
		return fmt.Errorf("agent.max_lifetime must be >= 1m (got %v)", maxLifetime)
	}
	if maxLifetime > 168*time.Hour {
		return fmt.Errorf("agent.max_lifetime must be <= 168h (got %v)", maxLifetime)
	}

	// Logical constraint: idle timeout should be <= max lifetime
	if idleTimeout > maxLifetime {
		return fmt.Errorf("agent.idle_timeout (%v) must be <= agent.max_lifetime (%v)", idleTimeout, maxLifetime)
	}

	return nil
}

// Validate checks AuditConfig values are within acceptable bounds.
func (c *AuditConfig) Validate() error {
	return nil
}

// Validate checks SessionArchiveConfig values are within acceptable bounds.
func (c *SessionArchiveConfig) Validate() error {
	// Default archive directory if unset
	if c.Dir == "" {
		c.Dir = filepath.Join(DefaultPaths().StateDir, "archives")
	}

	// Default min age if unset
	if c.MinAge.Duration() <= 0 {
		c.MinAge = Duration(30 * 24 * time.Hour)
	}

	// Default max bundles if unset
	if c.MaxBundles <= 0 {
		c.MaxBundles = 12
	}

	// Validate max run bytes
	if c.MaxRunBytes < 0 {
		return fmt.Errorf("logging.session.archive.max_run_bytes must be >= 0 (got %d)", c.MaxRunBytes)
	}

	// Validate jitter
	if c.Jitter.Duration() < 0 {
		return fmt.Errorf("logging.session.archive.jitter must be >= 0 (got %v)", c.Jitter.Duration())
	}

	return nil
}

// Validate checks SSHSecurityConfig values are within acceptable bounds.
func (c *SSHSecurityConfig) Validate() error {
	// Apply host_key_policy preset if provided
	hkp := strings.ToLower(strings.TrimSpace(c.HostKeyPolicy))
	switch hkp {
	case "", "pin":
		// default, no-op
	case "tofu":
		// TOFU preset prefers accept-new; also allow probes to persist
		c.AcceptOnceMode = "accept-new"
		c.CompatPersistProbes = true
	default:
		return fmt.Errorf("ssh.security.host_key_policy must be 'pin' or 'tofu' (got %q)", c.HostKeyPolicy)
	}

	// Validate accept once mode
	switch strings.ToLower(c.AcceptOnceMode) {
	case "", "pin":
		c.AcceptOnceMode = "pin"
	case "accept-new":
		// leave as is
	default:
		return fmt.Errorf("ssh.security.accept_once_mode must be 'pin' or 'accept-new' (got %q)", c.AcceptOnceMode)
	}

	return nil
}
