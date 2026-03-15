package sync

import (
	"os"
	"path/filepath"
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

func TestSourceStateRoundTrip(t *testing.T) {
	setupTestStateDir(t)

	state := &SourceState{
		Version:  StateVersion,
		Source:   "test-lab",
		Provider: "containerlab",
		LastSync: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC),
		Objects: map[string]*ManagedHost{
			"lab1/core01": {
				ObjectID:        "lab1/core01",
				Host:            "clab-core01",
				Patterns:        []string{"clab-core01", "core01"},
				Context:         "lab",
				IncludeFile:     "conf.d/sync_test-lab",
				HostName:        "172.20.0.2",
				ProxyJump:       "nre-netlab01",
				UsesPassword:    true,
				CredentialClass: "ceos",
			},
		},
	}

	if err := SaveSourceState(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadSourceState("test-lab")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded state is nil")
	} else if loaded.Version != StateVersion {
		t.Errorf("version = %d, want %d", loaded.Version, StateVersion)
	}
	if loaded.Source != "test-lab" {
		t.Errorf("source = %q", loaded.Source)
	}
	if loaded.Provider != "containerlab" {
		t.Errorf("provider = %q", loaded.Provider)
	}
	if !loaded.LastSync.Equal(state.LastSync) {
		t.Errorf("last_sync = %v", loaded.LastSync)
	}
	if len(loaded.Objects) != 1 {
		t.Fatalf("objects count = %d", len(loaded.Objects))
	}

	obj := loaded.Objects["lab1/core01"]
	if obj == nil {
		t.Fatal("object lab1/core01 not found")
	} else if obj.Host != "clab-core01" {
		t.Errorf("host = %q", obj.Host)
	}
	if len(obj.Patterns) != 2 {
		t.Errorf("patterns = %v", obj.Patterns)
	}
	if obj.CredentialClass != "ceos" {
		t.Errorf("credential_class = %q", obj.CredentialClass)
	}
}

func TestLoadSourceStateMissing(t *testing.T) {
	setupTestStateDir(t)

	state, err := LoadSourceState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Fatalf("expected nil state for nonexistent source, got %+v", state)
	}
}

func TestListSourceStates(t *testing.T) {
	setupTestStateDir(t)

	// Save two sources
	for _, name := range []string{"alpha", "bravo"} {
		s := &SourceState{
			Version: StateVersion,
			Source:  name,
			Objects: make(map[string]*ManagedHost),
		}
		if err := SaveSourceState(s); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}

	sources, err := ListSourceStates()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d: %v", len(sources), sources)
	}
}

func TestDeleteSourceState(t *testing.T) {
	tmp := setupTestStateDir(t)

	s := &SourceState{
		Version: StateVersion,
		Source:  "doomed",
		Objects: make(map[string]*ManagedHost),
	}
	if err := SaveSourceState(s); err != nil {
		t.Fatal(err)
	}

	// Verify it exists
	path := filepath.Join(tmp, "sync", "sources", "doomed.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file should exist: %v", err)
	}

	if err := DeleteSourceState("doomed"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("state file should be removed")
	}
}

func TestBuildSyncIndex(t *testing.T) {
	setupTestStateDir(t)

	// Save test state
	state := &SourceState{
		Version:  StateVersion,
		Source:   "test-lab",
		Provider: "containerlab",
		Objects: map[string]*ManagedHost{
			"dfz/core01": {
				ObjectID:        "dfz/core01",
				Host:            "clab-dfz-core01",
				Patterns:        []string{"clab-dfz-core01", "dfz-core01"},
				Context:         "lab",
				IncludeFile:     "conf.d/sync_test-lab",
				CredentialClass: "ceos",
			},
		},
	}

	if err := SaveSourceState(state); err != nil {
		t.Fatal(err)
	}

	index, err := BuildSyncIndex()
	if err != nil {
		t.Fatalf("build index: %v", err)
	}

	// Check primary host
	info, ok := index["clab-dfz-core01"]
	if !ok || info == nil {
		t.Fatal("primary host not in index")
	}
	if info.Source != "test-lab" {
		t.Errorf("source = %q", info.Source)
	}
	if info.Context != "lab" {
		t.Errorf("context = %q", info.Context)
	}
	if info.CredentialClass != "ceos" {
		t.Errorf("credential_class = %q", info.CredentialClass)
	}

}

func TestBuildSyncIndexEmpty(t *testing.T) {
	setupTestStateDir(t)

	index, err := BuildSyncIndex()
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	if len(index) != 0 {
		t.Errorf("expected empty index, got %d entries", len(index))
	}
}
