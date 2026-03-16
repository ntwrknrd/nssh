package sync

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

// StateVersion is the current version of the state file format.
const StateVersion = 1

// stateDirOverride allows tests to override the state directory.
var stateDirOverride string

// SetStateDir overrides the state directory (for testing).
// Pass empty string to revert to default.
func SetStateDir(dir string) {
	stateDirOverride = dir
}

// SourceState holds non-secret sync state for a single source.
// Stored per-source at ~/.local/state/nssh/sync/sources/<source>.json.
type SourceState struct {
	Version     int                     `json:"version"`
	Source      string                  `json:"source"`
	Provider    string                  `json:"provider"`
	LastSync    time.Time               `json:"last_sync"`
	IncludeFile string                  `json:"include_file"`
	Objects     map[string]*ManagedHost `json:"objects"`
}

// ManagedHost represents one sync-managed SSH target persisted in state.
type ManagedHost struct {
	ObjectID        string   `json:"object_id"`
	Host            string   `json:"host"`
	Patterns        []string `json:"patterns,omitempty"`
	Context         string   `json:"context,omitempty"`
	HostName        string   `json:"hostname"`
	Port            int      `json:"port,omitempty"`
	ProxyJump       string   `json:"proxy_jump,omitempty"`
	UsesPassword    bool     `json:"uses_password,omitempty"`
	CredentialClass string   `json:"credential_class,omitempty"`
}

// syncStateDir returns the per-source state directory.
func syncStateDir() string {
	if stateDirOverride != "" {
		return filepath.Join(stateDirOverride, "sync", "sources")
	}
	return filepath.Join(config.DefaultPaths().StateDir, "sync", "sources")
}

// syncLockDir returns the advisory lock directory.
func syncLockDir() string {
	if stateDirOverride != "" {
		return filepath.Join(stateDirOverride, "sync", "locks")
	}
	return filepath.Join(config.DefaultPaths().StateDir, "sync", "locks")
}

// stateFilePath returns the path for a source's state file.
func stateFilePath(source string) string {
	return filepath.Join(syncStateDir(), source+".json")
}

// SourceIncludeFile returns the deterministic sync-owned SSH config file for a
// source.
func SourceIncludeFile(source string) string {
	return filepath.Join("conf.d", "sync_"+source)
}

// LoadSourceState loads state for the named source.
// Returns nil state (not an error) if the state file does not exist.
func LoadSourceState(source string) (*SourceState, error) {
	path := stateFilePath(source)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}

	var state SourceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}

	if state.Version != StateVersion {
		return nil, fmt.Errorf("unsupported state version %d in %s (expected %d)", state.Version, path, StateVersion)
	}
	if state.IncludeFile == "" {
		state.IncludeFile = SourceIncludeFile(state.Source)
	}

	if state.Objects == nil {
		state.Objects = make(map[string]*ManagedHost)
	}

	return &state, nil
}

// SaveSourceState atomically writes the source state to disk.
func SaveSourceState(state *SourceState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	return atomicWriteFile(stateFilePath(state.Source), data, 0600)
}

// ListSourceStates returns the names of all sources that have state files.
func ListSourceStates() ([]string, error) {
	dir := syncStateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state dir: %w", err)
	}

	var sources []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if ext := filepath.Ext(name); ext == ".json" {
			sources = append(sources, name[:len(name)-len(ext)])
		}
	}
	return sources, nil
}

// DeleteSourceState removes a source's state file.
func DeleteSourceState(source string) error {
	path := stateFilePath(source)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state %s: %w", path, err)
	}
	return nil
}

// SyncHostInfo holds the sync-managed metadata for a single host pattern.
type SyncHostInfo struct {
	Source          string
	Context         string
	CredentialClass string
}

// BuildSyncIndex scans all per-source state files and builds a reverse index
// from host/pattern to sync host info. This is called at connect time.
func BuildSyncIndex() (map[string]*SyncHostInfo, error) {
	sources, err := ListSourceStates()
	if err != nil {
		return nil, err
	}

	index := make(map[string]*SyncHostInfo)
	for _, sourceName := range sources {
		state, err := LoadSourceState(sourceName)
		if err != nil {
			slog.Warn("skip corrupt sync state", "source", sourceName, "err", err)
			continue
		}
		if state == nil {
			continue
		}

			for _, mh := range state.Objects {
				info := &SyncHostInfo{
					Source:          sourceName,
					Context:         mh.Context,
					CredentialClass: mh.CredentialClass,
				}

			index[mh.Host] = info
			for _, p := range mh.Patterns {
				index[p] = info
			}
		}
	}

	return index, nil
}
