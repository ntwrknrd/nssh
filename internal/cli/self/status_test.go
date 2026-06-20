package self

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestRunStatusDoesNotShowAgentRuntimeSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	got := captureStdout(t, func() {
		if err := runStatus(); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{"Version", "Dependencies", "Configuration", "Logging"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
	for _, reject := range []string{"NSSH STATUS", "Session", "provider runtime active", "Idle in", "Ends in"} {
		if strings.Contains(got, reject) {
			t.Fatalf("self status should not include agent runtime %q:\n%s", reject, got)
		}
	}
}

func TestRunStatusShowsCredentialProviderReadiness(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "config")
	binDir := filepath.Join(home, "bin")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("PATH", binDir)
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "pass"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "gpg"), []byte("#!/bin/sh\nprintf 'sec:u:4096:1:ABCDEF:0:0:::::::\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "gpgconf"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(home, ".password-store")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, ".gpg-id"), []byte("ABCDEF\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	paths := config.DefaultPaths()
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		t.Fatal(err)
	}

	got := captureStdout(t, func() {
		if err := runStatus(); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{"Credential Providers", "pass", "ready"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
}
