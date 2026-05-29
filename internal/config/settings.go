package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration structure loaded from config.toml.
type Config struct {
	Agent      AgentConfig      `toml:"agent"`
	Credential CredentialConfig `toml:"credential"`
	Host       HostConfig       `toml:"host"`
	Inventory  InventoryConfig  `toml:"inventory"`
	Logging    LoggingConfig    `toml:"logging"`
	SSH        SSHConfig        `toml:"ssh"`
	Sync       SyncConfig       `toml:"-"`
}

// ============================================================================
// Agent Configuration
// ============================================================================

// AgentConfig holds agent daemon lifecycle settings.
type AgentConfig struct {
	// IdleTimeout is how long the agent waits without activity before terminating (default 1h)
	IdleTimeout Duration `toml:"idle_timeout"`
	// ActivityIncrement is how much to extend the idle deadline on activity (default 15m)
	ActivityIncrement Duration `toml:"activity_increment"`
	// MaxLifetime is the maximum lifetime of the agent regardless of activity (default 24h)
	MaxLifetime Duration `toml:"max_lifetime"`
	// Security holds credential protection settings
	Security AgentSecurityConfig `toml:"security"`
}

// AgentSecurityConfig holds credential protection settings.
// Note: Security mode is detected from filesystem state (age.key.enc), not from
// config. See vault.DetectSecurityMode().
type AgentSecurityConfig struct {
	// Software holds settings specific to software mode
	Software SoftwareSecurityConfig `toml:"software"`
}

// SoftwareSecurityConfig holds settings for software-based credential protection.
type SoftwareSecurityConfig struct {
	// LockoutDuration is the initial lockout duration (default: 5m)
	LockoutDuration Duration `toml:"lockout_duration"`
	// LockoutThreshold is the number of failed attempts before lockout (default: 10)
	LockoutThreshold int `toml:"lockout_threshold"`
	// MaxLockoutDuration is the maximum lockout duration with exponential backoff (default: 1h)
	MaxLockoutDuration Duration `toml:"max_lockout_duration"`
	// PassphraseMinLength enforces minimum length for credential passphrases (default: 12)
	PassphraseMinLength int `toml:"passphrase_min_length"`
	// ScryptWorkFactor is the scrypt work factor (2^N iterations, default 18)
	ScryptWorkFactor int `toml:"scrypt_work_factor"`
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
	// DefaultUser is the default SSH username for new hosts
	DefaultUser string `toml:"default_user"`
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
	// MaxBackupFiles is the maximum number of backup files to retain (default: 10)
	MaxBackupFiles int `toml:"max_backup_files"`
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
			IdleTimeout:       Duration(1 * time.Hour),
			ActivityIncrement: Duration(15 * time.Minute),
			MaxLifetime:       Duration(24 * time.Hour),
			Security: AgentSecurityConfig{
				Software: SoftwareSecurityConfig{
					LockoutDuration:     Duration(5 * time.Minute),
					LockoutThreshold:    10,
					MaxLockoutDuration:  Duration(1 * time.Hour),
					PassphraseMinLength: 12,
					ScryptWorkFactor:    18,
				},
			},
		},
		Host: HostConfig{
			Defaults: HostDefaultsConfig{
				DefaultContext: "",
				DefaultUser:    "",
			},
		},
		Credential: CredentialConfig{
			Type: CredentialProviderAge,
		},
		Inventory: InventoryConfig{
			DefaultGroup: "default",
			Group: map[string]GroupConfig{
				"default": {},
			},
		},
		Logging: LoggingConfig{
			Audit: AuditConfig{
				Enabled:        true,
				MaxBackupFiles: 10,
				MaxSize:        "10MB",
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

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			if err := cfg.Validate(); err != nil {
				return nil, fmt.Errorf("validate config: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if md.IsDefined("sync", "sources") {
		return nil, fmt.Errorf("validate config %s: %w", path, legacySyncSourcesError())
	}
	pruneImplicitInventoryDefaults(md, cfg)

	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %s: %w", path, err)
	}

	return cfg, nil
}

func pruneImplicitInventoryDefaults(md toml.MetaData, cfg *Config) {
	if cfg == nil || cfg.Inventory.Group == nil {
		return
	}
	defaultGroupExplicit := md.IsDefined("inventory", "group", "default")
	configDefinesGroups := md.IsDefined("inventory", "group")
	configChangesDefaultGroup := md.IsDefined("inventory", "default_group") && cfg.Inventory.DefaultGroup != "default"
	if !defaultGroupExplicit && (configDefinesGroups || configChangesDefaultGroup) {
		delete(cfg.Inventory.Group, "default")
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(cfg)
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
	if err := c.Logging.Audit.Validate(); err != nil {
		return err
	}
	if err := c.Logging.Session.Archive.Validate(); err != nil {
		return err
	}
	if err := c.SSH.Security.Validate(); err != nil {
		return err
	}
	if len(c.Sync.Sources) > 0 {
		return legacySyncSourcesError()
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

	return c.Security.Validate()
}

// Validate checks AgentSecurityConfig values are within acceptable bounds.
func (c *AgentSecurityConfig) Validate() error {
	return c.Software.Validate()
}

// Validate checks SoftwareSecurityConfig values are within acceptable bounds.
func (c *SoftwareSecurityConfig) Validate() error {
	// Default passphrase min length if unset
	if c.PassphraseMinLength == 0 {
		c.PassphraseMinLength = 12
	}

	// Validate scrypt work factor (min 14, max 22)
	if c.ScryptWorkFactor < 14 {
		return fmt.Errorf("agent.security.software.scrypt_work_factor must be >= 14 (got %d)", c.ScryptWorkFactor)
	}
	if c.ScryptWorkFactor > 22 {
		return fmt.Errorf("agent.security.software.scrypt_work_factor must be <= 22 (got %d)", c.ScryptWorkFactor)
	}

	// Validate passphrase minimum length
	if c.PassphraseMinLength < 8 {
		return fmt.Errorf("agent.security.software.passphrase_min_length must be >= 8 (got %d)", c.PassphraseMinLength)
	}
	if c.PassphraseMinLength > 128 {
		return fmt.Errorf("agent.security.software.passphrase_min_length must be <= 128 (got %d)", c.PassphraseMinLength)
	}

	// Validate lockout threshold (min 3, max 100)
	if c.LockoutThreshold < 3 {
		return fmt.Errorf("agent.security.software.lockout_threshold must be >= 3 (got %d)", c.LockoutThreshold)
	}
	if c.LockoutThreshold > 100 {
		return fmt.Errorf("agent.security.software.lockout_threshold must be <= 100 (got %d)", c.LockoutThreshold)
	}

	// Validate lockout duration (min 1m, max 1h)
	lockoutDuration := c.LockoutDuration.Duration()
	if lockoutDuration < time.Minute {
		return fmt.Errorf("agent.security.software.lockout_duration must be >= 1m (got %v)", lockoutDuration)
	}
	if lockoutDuration > time.Hour {
		return fmt.Errorf("agent.security.software.lockout_duration must be <= 1h (got %v)", lockoutDuration)
	}

	// Validate max lockout duration (min 5m, max 24h)
	maxLockoutDuration := c.MaxLockoutDuration.Duration()
	if maxLockoutDuration < 5*time.Minute {
		return fmt.Errorf("agent.security.software.max_lockout_duration must be >= 5m (got %v)", maxLockoutDuration)
	}
	if maxLockoutDuration > 24*time.Hour {
		return fmt.Errorf("agent.security.software.max_lockout_duration must be <= 24h (got %v)", maxLockoutDuration)
	}

	// Logical constraint: max_lockout_duration >= lockout_duration
	if maxLockoutDuration < lockoutDuration {
		return fmt.Errorf("agent.security.software.max_lockout_duration (%v) must be >= lockout_duration (%v)", maxLockoutDuration, lockoutDuration)
	}

	return nil
}

// Validate checks AuditConfig values are within acceptable bounds.
func (c *AuditConfig) Validate() error {
	// Validate max backup files (reasonable bounds)
	if c.MaxBackupFiles < 1 {
		return fmt.Errorf("logging.audit.max_backup_files must be >= 1 (got %d)", c.MaxBackupFiles)
	}
	if c.MaxBackupFiles > 100 {
		return fmt.Errorf("logging.audit.max_backup_files must be <= 100 (got %d)", c.MaxBackupFiles)
	}
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
