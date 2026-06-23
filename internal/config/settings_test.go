package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDuration_UnmarshalText(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"1h", time.Hour, false},
		{"100ms", 100 * time.Millisecond, false},
		{"0", 0, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalText([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && d.Duration() != tt.want {
				t.Errorf("UnmarshalText() = %v, want %v", d.Duration(), tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SSH.Connection.Timeout.Duration() != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", cfg.SSH.Connection.Timeout.Duration())
	}
	if cfg.SSH.Connection.PasswordTimeout.Duration() != 10*time.Second {
		t.Errorf("default password_timeout = %v, want 10s", cfg.SSH.Connection.PasswordTimeout.Duration())
	}
	if !cfg.Agent.AutoStart {
		t.Error("default agent.auto_start = false, want true")
	}
	provider, ok := cfg.Credential.Provider["sops"]
	if !ok {
		t.Fatal("default credentials missing sops")
	}
	if provider.Type != CredentialProviderSOPSAge {
		t.Fatalf("default sops type = %q, want %q", provider.Type, CredentialProviderSOPSAge)
	}
	if provider.File != "~/.local/share/nssh/credentials.sops.yaml" {
		t.Fatalf("default sops file = %q", provider.File)
	}
	if _, ok := cfg.Credential.Provider["pass"]; ok {
		t.Fatal("default credentials still include pass")
	}
	if _, ok := cfg.Inventory.Providers[ProviderLocal]; !ok {
		t.Fatal("default inventory missing local provider")
	}
	group := cfg.Inventory.Providers[ProviderLocal].Groups["default"]
	if group.Auth.IsSet() {
		t.Fatalf("default local group auth = %+v, want unset", group.Auth)
	}
}

func TestHighlightConfigValidate(t *testing.T) {
	trueValue := true
	tests := []struct {
		name      string
		highlight HighlightConfig
		wantErr   string
	}{
		{name: "none disabled", highlight: HighlightConfig{Profile: HighlightProfileNone}},
		{name: "junos enabled", highlight: HighlightConfig{Enabled: &trueValue, Profile: HighlightProfileJunos}},
		{name: "unknown profile", highlight: HighlightConfig{Profile: "slow-regex"}, wantErr: `unsupported highlight profile "slow-regex"`},
		{name: "enabled none rejected", highlight: HighlightConfig{Enabled: &trueValue, Profile: HighlightProfileNone}, wantErr: "highlight.profile must not be none when highlight.enabled is true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.highlight.Validate("highlight")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestMergeHighlightAllowsHostDisableOverride(t *testing.T) {
	trueValue := true
	falseValue := false

	got := MergeHighlight(
		HighlightConfig{Profile: HighlightProfileNone},
		HighlightConfig{Enabled: &trueValue, Profile: HighlightProfileJunos},
	)
	got = MergeHighlight(got, HighlightConfig{Enabled: &falseValue})

	if got.Enabled == nil || *got.Enabled {
		t.Fatalf("enabled = %v, want explicit false", got.Enabled)
	}
	if got.Profile != HighlightProfileJunos {
		t.Fatalf("profile = %q, want %q", got.Profile, HighlightProfileJunos)
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for nonexistent file", err)
	}
	if cfg.SSH.Connection.Timeout.Duration() != 30*time.Second {
		t.Errorf("got timeout = %v, want default 30s", cfg.SSH.Connection.Timeout.Duration())
	}
}

func TestLoad_ValidYAMLConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `
ssh:
  connection:
    timeout: 60s
    password_timeout: 20s
logging:
  audit:
    max_size: 20MB
agent:
  auto_start: false
`)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.SSH.Connection.Timeout.Duration() != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", cfg.SSH.Connection.Timeout.Duration())
	}
	if cfg.SSH.Connection.PasswordTimeout.Duration() != 20*time.Second {
		t.Errorf("password_timeout = %v, want 20s", cfg.SSH.Connection.PasswordTimeout.Duration())
	}
	if cfg.Logging.Audit.MaxSize != "20MB" {
		t.Errorf("logging.audit.max_size = %q, want 20MB", cfg.Logging.Audit.MaxSize)
	}
	if cfg.Agent.AutoStart {
		t.Error("agent.auto_start = true, want false")
	}
}

func TestLoad_AcceptsArchiveTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `
logging:
  session:
    archive:
      timeout: 45s
`)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Logging.Session.Archive.Timeout.Duration(); got != 45*time.Second {
		t.Fatalf("logging.session.archive.timeout = %v, want 45s", got)
	}
}

func TestLoad_RejectsObsoleteArchiveSchedulerFields(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{name: "enabled", field: "enabled: true"},
		{name: "jitter", field: "jitter: 5m"},
		{name: "min_interval", field: "min_interval: 24h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			writeConfigFile(t, configPath, `
logging:
  session:
    archive:
      `+tt.field+`
`)

			_, err := Load(configPath)
			if err == nil {
				t.Fatalf("Load() should reject obsolete archive field %q", tt.name)
			}
			if !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("Load() error = %v, want %q", err, tt.name)
			}
		})
	}
}

func TestLoad_RejectsObsoleteMaxBackupFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `
logging:
  audit:
    max_backup_files: 20
`)

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() should reject obsolete max_backup_files")
	}
	if !strings.Contains(err.Error(), "max_backup_files") {
		t.Fatalf("Load() error = %v, want unknown max_backup_files", err)
	}
}

func TestLoad_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `
ssh:
  connection:
    timeout: not a duration
`)

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() should return error for invalid duration")
	}
}

func TestAgentConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  AgentConfig
		wantErr string
	}{
		{
			name: "valid defaults",
			config: AgentConfig{
				IdleTimeout:            Duration(time.Hour),
				ActivityIncrement:      Duration(15 * time.Minute),
				MaxLifetime:            Duration(24 * time.Hour),
				ProviderRequestTimeout: Duration(2 * time.Minute),
			},
		},
		{
			name: "provider_request_timeout zero uses default",
			config: AgentConfig{
				IdleTimeout:       Duration(time.Hour),
				ActivityIncrement: Duration(15 * time.Minute),
				MaxLifetime:       Duration(24 * time.Hour),
			},
		},
		{
			name: "provider_request_timeout too short",
			config: AgentConfig{
				IdleTimeout:            Duration(time.Hour),
				ActivityIncrement:      Duration(15 * time.Minute),
				MaxLifetime:            Duration(24 * time.Hour),
				ProviderRequestTimeout: Duration(4 * time.Second),
			},
			wantErr: "provider_request_timeout must be >= 5s",
		},
		{
			name: "provider_request_timeout too long",
			config: AgentConfig{
				IdleTimeout:            Duration(time.Hour),
				ActivityIncrement:      Duration(15 * time.Minute),
				MaxLifetime:            Duration(24 * time.Hour),
				ProviderRequestTimeout: Duration(11 * time.Minute),
			},
			wantErr: "provider_request_timeout must be <= 10m",
		},
		{
			name: "idle_timeout too short",
			config: AgentConfig{
				IdleTimeout:       Duration(500 * time.Millisecond),
				ActivityIncrement: Duration(15 * time.Minute),
				MaxLifetime:       Duration(24 * time.Hour),
			},
			wantErr: "idle_timeout must be >= 1s",
		},
		{
			name: "idle_timeout too long",
			config: AgentConfig{
				IdleTimeout:       Duration(25 * time.Hour),
				ActivityIncrement: Duration(15 * time.Minute),
				MaxLifetime:       Duration(168 * time.Hour),
			},
			wantErr: "idle_timeout must be <= 24h",
		},
		{
			name: "max_lifetime too short",
			config: AgentConfig{
				IdleTimeout:       Duration(30 * time.Second),
				ActivityIncrement: Duration(10 * time.Second),
				MaxLifetime:       Duration(30 * time.Second),
			},
			wantErr: "max_lifetime must be >= 1m",
		},
		{
			name: "idle_timeout exceeds max_lifetime",
			config: AgentConfig{
				IdleTimeout:       Duration(2 * time.Hour),
				ActivityIncrement: Duration(15 * time.Minute),
				MaxLifetime:       Duration(1 * time.Hour),
			},
			wantErr: "agent.idle_timeout (2h0m0s) must be <= agent.max_lifetime (1h0m0s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_AcceptsProviderRequestTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `
agent:
  provider_request_timeout: 90s
`)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Agent.ProviderRequestTimeout.Duration(); got != 90*time.Second {
		t.Fatalf("agent.provider_request_timeout = %v, want 90s", got)
	}
}

func TestLoad_ValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `
agent:
  idle_timeout: 500ms
`)

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() should return error for invalid idle_timeout")
	}
	if !strings.Contains(err.Error(), "idle_timeout must be >= 1s") {
		t.Errorf("Load() error = %v, expected idle_timeout validation error", err)
	}
}

func TestLoad_EnvOverrideValidation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfigFile(t, configPath, `{}`)

	t.Setenv("NSSH_AGENT_IDLE_TIMEOUT", "500ms")

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() should return error for invalid idle_timeout from env")
	}
	if !strings.Contains(err.Error(), "idle_timeout must be >= 1s") {
		t.Errorf("Load() error = %v, expected idle_timeout validation error", err)
	}
}

func TestSSHSecurityConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  SSHSecurityConfig
		wantErr string
	}{
		{name: "valid defaults", config: SSHSecurityConfig{HostKeyPolicy: "tofu", AcceptOnceMode: "pin"}},
		{name: "valid host_key_policy - pin", config: SSHSecurityConfig{HostKeyPolicy: "pin", AcceptOnceMode: "pin"}},
		{name: "invalid host_key_policy", config: SSHSecurityConfig{HostKeyPolicy: "invalid", AcceptOnceMode: "pin"}, wantErr: "host_key_policy must be 'pin' or 'tofu'"},
		{name: "valid accept_once_mode - accept-new", config: SSHSecurityConfig{HostKeyPolicy: "tofu", AcceptOnceMode: "accept-new"}},
		{name: "invalid accept_once_mode", config: SSHSecurityConfig{HostKeyPolicy: "pin", AcceptOnceMode: "invalid"}, wantErr: "accept_once_mode must be 'pin' or 'accept-new'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
