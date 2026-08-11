package inventory

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestStateDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	SetStateDir(tmp)
	t.Cleanup(func() { SetStateDir("") })
	return tmp
}

func TestProviderStateRoundTripUsesInventoryPath(t *testing.T) {
	tmp := setupTestStateDir(t)

	state := &ProviderState{
		Version:     StateVersion,
		Provider:    "netbox-prod",
		Type:        "netbox",
		LastRefresh: time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
		IncludeFile: ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*ProviderHost{
			"device:1": {
				ObjectID: "device:1",
				Host:     "edge01",
				Patterns: []string{"edge01"},
				Group:    "customer",
				HostName: "edge01.customer.local",
			},
		},
	}
	if err := SaveProviderState(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	wantPath := filepath.Join(tmp, "inventory", "providers", "netbox-prod.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected state at %s: %v", wantPath, err)
	}

	loaded, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Provider != "netbox-prod" {
		t.Fatalf("provider = %q", loaded.Provider)
	}
	if loaded.Objects["device:1"].Group != "customer" {
		t.Fatalf("group = %q", loaded.Objects["device:1"].Group)
	}
}

func TestProviderStateSerializesModeKey(t *testing.T) {
	tmp := setupTestStateDir(t)

	state := &ProviderState{
		Version:     StateVersion,
		Provider:    "netbox-prod",
		Type:        "netbox",
		LastRefresh: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
		IncludeFile: ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*ProviderHost{
			"device:1": {
				ObjectID: "device:1",
				Host:     "edge01",
				HostName: "edge01.customer.local",
				AuthMode: "password",
			},
		},
	}
	if err := SaveProviderState(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "inventory", "providers", "netbox-prod.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"mode": "password"`) {
		t.Fatalf("saved state missing mode key:\n%s", text)
	}
	if strings.Contains(text, "auth_mode") {
		t.Fatalf("saved state should not use auth_mode:\n%s", text)
	}
}

func TestProviderIncludeFileUsesNSSHDirectory(t *testing.T) {
	if got := ProviderIncludeFile("netbox-prod"); got != filepath.Join("nssh.d", "provider_netbox-prod.conf") {
		t.Fatalf("include file = %q", got)
	}
}

func TestBuildProviderIndexSkipsUnsupportedStateVersionWithoutWarning(t *testing.T) {
	setupTestStateDir(t)
	if err := os.MkdirAll(filepath.Dir(providerStatePath("netbox-prod")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerStatePath("netbox-prod"), []byte(`{"version":1,"provider":"netbox-prod","objects":{}}`), 0600); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	index, err := BuildProviderIndex()
	if err != nil {
		t.Fatalf("BuildProviderIndex: %v", err)
	}
	if len(index) != 0 {
		t.Fatalf("index = %+v, want empty for stale state", index)
	}
	if strings.Contains(logs.String(), "skip corrupt provider state") {
		t.Fatalf("stale state was logged as corrupt: %s", logs.String())
	}
}
