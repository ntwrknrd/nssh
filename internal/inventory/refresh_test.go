package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

type fakeProvider struct {
	objects []Object
	err     error
}

func (p fakeProvider) Discover(context.Context, string, config.InventoryProviderConfig, RemoteRunner) ([]Object, error) {
	return p.objects, p.err
}

func TestRefreshProviderWritesStateWithoutCredentials(t *testing.T) {
	setupTestStateDir(t)
	cfg := config.InventoryProviderConfig{
		Type: config.ProviderNetBox,
		Group: map[string]config.GroupConfig{
			"customer": {},
		},
	}
	now := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)

	result := RefreshProvider(context.Background(), "netbox-prod", cfg, fakeProvider{objects: []Object{{
		ObjectID: "device:1",
		Name:     "edge01",
		HostName: "edge01.customer.local",
	}}}, nil, RefreshOptions{
		Now:            now,
		WriteSSHConfig: false,
	})
	if result.Err != nil {
		t.Fatalf("refresh: %v", result.Err)
	}
	state, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state == nil || len(state.Objects) != 1 {
		t.Fatalf("state = %+v", state)
	}
	if state.Objects["device:1"].Group != "netbox-prod/customer" {
		t.Fatalf("group = %q", state.Objects["device:1"].Group)
	}
	if state.Objects["device:1"].Username != "" {
		t.Fatalf("managed state stored identity username = %q", state.Objects["device:1"].Username)
	}
	data, err := json.Marshal(state.Objects["device:1"])
	if err != nil {
		t.Fatalf("marshal host state: %v", err)
	}
	if strings.Contains(string(data), "route") {
		t.Fatalf("managed state stored legacy route: %s", data)
	}
	if !state.LastRefresh.Equal(now) {
		t.Fatalf("last_refresh = %v", state.LastRefresh)
	}
}

func TestRefreshProviderSelectorConflictDoesNotWriteState(t *testing.T) {
	setupTestStateDir(t)
	cfg := config.InventoryProviderConfig{Type: config.ProviderNetBox, Group: map[string]config.GroupConfig{
		"customer": {Match: config.InventoryMatch{"role": {"router"}}},
		"network":  {Match: config.InventoryMatch{"role": {"router"}}},
	}}

	result := RefreshProvider(context.Background(), "netbox-prod", cfg, fakeProvider{objects: []Object{{
		ObjectID:   "device:1",
		Name:       "edge01",
		HostName:   "edge01.example.com",
		Attributes: map[string][]string{"role": {"router"}},
	}}}, nil, RefreshOptions{
		WriteSSHConfig: false,
	})
	if result.Err == nil {
		t.Fatal("expected selector conflict error")
	}
	if !strings.Contains(result.Err.Error(), "matched multiple groups") {
		t.Fatalf("error %q does not identify selector conflict", result.Err)
	}
	state, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state != nil {
		t.Fatalf("state was written despite selector conflict: %+v", state)
	}
}

func TestRefreshProviderTreatsUnsupportedStateVersionAsMissing(t *testing.T) {
	setupTestStateDir(t)
	if err := os.MkdirAll(filepath.Dir(providerStatePath("netbox-prod")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerStatePath("netbox-prod"), []byte(`{"version":1,"provider":"netbox-prod","objects":{"old":{"object_id":"old","host":"old01","group":"customer","hostname":"old01.example.com"}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	result := RefreshProvider(context.Background(), "netbox-prod", config.InventoryProviderConfig{Type: config.ProviderNetBox, Group: map[string]config.GroupConfig{"customer": {}}}, fakeProvider{objects: []Object{{
		ObjectID: "device:1",
		Name:     "edge01",
		HostName: "edge01.example.com",
	}}}, nil, RefreshOptions{
		WriteSSHConfig: false,
	})
	if result.Err != nil {
		t.Fatalf("refresh: %v", result.Err)
	}
	state, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.Version != StateVersion {
		t.Fatalf("state version = %d, want %d", state.Version, StateVersion)
	}
	if _, ok := state.Objects["device:1"]; !ok {
		t.Fatalf("new state missing refreshed object: %+v", state.Objects)
	}
	if _, ok := state.Objects["old"]; ok {
		t.Fatalf("stale object carried forward from unsupported state: %+v", state.Objects)
	}
}

func TestRefreshProviderFailurePreservesLastKnownGoodState(t *testing.T) {
	setupTestStateDir(t)
	current := &ProviderState{
		Version:     StateVersion,
		Provider:    "netbox-prod",
		Type:        config.ProviderNetBox,
		IncludeFile: ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*ProviderHost{
			"device:1": {ObjectID: "device:1", Host: "edge01", Group: "customer", HostName: "edge01.customer.local"},
		},
	}
	if err := SaveProviderState(current); err != nil {
		t.Fatal(err)
	}

	result := RefreshProvider(context.Background(), "netbox-prod", config.InventoryProviderConfig{Type: config.ProviderNetBox}, fakeProvider{err: errors.New("api down")}, nil, RefreshOptions{WriteSSHConfig: false})
	if result.Err == nil {
		t.Fatal("expected refresh error")
	}
	state, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(state.Objects) != 1 {
		t.Fatalf("last-known-good objects were not preserved: %+v", state.Objects)
	}
	if state.LastError == "" {
		t.Fatal("expected last error to be recorded")
	}
}
