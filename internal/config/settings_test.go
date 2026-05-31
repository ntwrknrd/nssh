package config

import (
	"os"
	"path/filepath"
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
	if cfg.Logging.Audit.MaxBackupFiles != 10 {
		t.Errorf("default logging.audit.max_backup_files = %d, want 10", cfg.Logging.Audit.MaxBackupFiles)
	}
	if !cfg.Agent.AutoStart {
		t.Error("default agent.auto_start = false, want true")
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for nonexistent file", err)
	}
	// Should return defaults
	if cfg.SSH.Connection.Timeout.Duration() != 30*time.Second {
		t.Errorf("got timeout = %v, want default 30s", cfg.SSH.Connection.Timeout.Duration())
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := `
[ssh.connection]
timeout = "60s"
password_timeout = "20s"

[logging.audit]
max_backup_files = 20

[agent]
auto_start = false
`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

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
	if cfg.Logging.Audit.MaxBackupFiles != 20 {
		t.Errorf("logging.audit.max_backup_files = %d, want 20", cfg.Logging.Audit.MaxBackupFiles)
	}
	if cfg.Agent.AutoStart {
		t.Error("agent.auto_start = true, want false")
	}
}

func TestLoad_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := `
[ssh.connection]
timeout = "not a duration"
`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

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
				IdleTimeout:       Duration(time.Hour),
				ActivityIncrement: Duration(15 * time.Minute),
				MaxLifetime:       Duration(24 * time.Hour),
			},
			wantErr: "",
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
			name: "max_lifetime too long",
			config: AgentConfig{
				IdleTimeout:       Duration(time.Hour),
				ActivityIncrement: Duration(15 * time.Minute),
				MaxLifetime:       Duration(169 * time.Hour),
			},
			wantErr: "max_lifetime must be <= 168h",
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
		{
			name: "boundary values - minimum valid",
			config: AgentConfig{
				IdleTimeout:       Duration(time.Second),
				ActivityIncrement: Duration(time.Second),
				MaxLifetime:       Duration(time.Minute),
			},
			wantErr: "",
		},
		{
			name: "boundary values - maximum valid",
			config: AgentConfig{
				IdleTimeout:       Duration(24 * time.Hour),
				ActivityIncrement: Duration(24 * time.Hour),
				MaxLifetime:       Duration(168 * time.Hour),
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestLoad_ValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := `
[agent]
idle_timeout = "500ms"
`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() should return error for invalid idle_timeout")
	}
	if !contains(err.Error(), "idle_timeout must be >= 1s") {
		t.Errorf("Load() error = %v, expected idle_timeout validation error", err)
	}
}

func TestLoad_EnvOverrideValidation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := ""
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Set invalid env override (below 1s minimum)
	t.Setenv("NSSH_AGENT_IDLE_TIMEOUT", "500ms")

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() should return error for invalid idle_timeout from env")
	}
	if !contains(err.Error(), "idle_timeout must be >= 1s") {
		t.Errorf("Load() error = %v, expected idle_timeout validation error", err)
	}
}

func TestSSHSecurityConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  SSHSecurityConfig
		wantErr string
	}{
		{
			name: "valid defaults",
			config: SSHSecurityConfig{
				HostKeyPolicy:  "tofu",
				AcceptOnceMode: "pin",
			},
			wantErr: "",
		},
		{
			name: "valid host_key_policy - pin",
			config: SSHSecurityConfig{
				HostKeyPolicy:  "pin",
				AcceptOnceMode: "pin",
			},
			wantErr: "",
		},
		{
			name: "invalid host_key_policy",
			config: SSHSecurityConfig{
				HostKeyPolicy:  "invalid",
				AcceptOnceMode: "pin",
			},
			wantErr: "host_key_policy must be 'pin' or 'tofu'",
		},
		{
			name: "valid accept_once_mode - accept-new",
			config: SSHSecurityConfig{
				HostKeyPolicy:  "tofu",
				AcceptOnceMode: "accept-new",
			},
			wantErr: "",
		},
		{
			name: "invalid accept_once_mode",
			config: SSHSecurityConfig{
				HostKeyPolicy:  "pin",
				AcceptOnceMode: "invalid",
			},
			wantErr: "accept_once_mode must be 'pin' or 'accept-new'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
