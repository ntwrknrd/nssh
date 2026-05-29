package inventory

import (
	"context"
	"errors"
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
		Route: []config.InventoryRouteConfig{{
			Group: "custcbb",
		}},
	}
	now := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)

	result := RefreshProvider(context.Background(), "netbox-prod", cfg, fakeProvider{objects: []Object{{
		ObjectID: "device:1",
		Name:     "edge01",
		HostName: "edge01.custcbb.local",
	}}}, nil, RefreshOptions{Now: now, WriteSSHConfig: false})
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
	if state.Objects["device:1"].Group != "custcbb" {
		t.Fatalf("group = %q", state.Objects["device:1"].Group)
	}
	if !state.LastRefresh.Equal(now) {
		t.Fatalf("last_refresh = %v", state.LastRefresh)
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
			"device:1": {ObjectID: "device:1", Host: "edge01", Group: "custcbb", HostName: "edge01.custcbb.local"},
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
