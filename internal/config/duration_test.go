package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurationRoundTrip(t *testing.T) {
	// Create test config with explicit durations
	cfg := DefaultConfig()
	cfg.Agent.IdleTimeout = Duration(2 * time.Hour)
	cfg.Agent.MaxLifetime = Duration(48 * time.Hour)
	cfg.Agent.Security.Software.LockoutDuration = Duration(10 * time.Minute)
	cfg.Agent.Security.Software.MaxLockoutDuration = Duration(2 * time.Hour)

	// Save to temp file
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(tmpFile, cfg); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Verify file contains string durations, not integers
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	content := string(data)
	t.Logf("Written config:\n%s", content)

	// Load it back
	loaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Verify values
	if loaded.Agent.IdleTimeout.Duration() != 2*time.Hour {
		t.Errorf("IdleTimeout = %v, want 2h", loaded.Agent.IdleTimeout.Duration())
	}
	if loaded.Agent.MaxLifetime.Duration() != 48*time.Hour {
		t.Errorf("MaxLifetime = %v, want 48h", loaded.Agent.MaxLifetime.Duration())
	}
	if loaded.Agent.Security.Software.LockoutDuration.Duration() != 10*time.Minute {
		t.Errorf("LockoutDuration = %v, want 10m", loaded.Agent.Security.Software.LockoutDuration.Duration())
	}
	if loaded.Agent.Security.Software.MaxLockoutDuration.Duration() != 2*time.Hour {
		t.Errorf("MaxLockoutDuration = %v, want 2h", loaded.Agent.Security.Software.MaxLockoutDuration.Duration())
	}
}
