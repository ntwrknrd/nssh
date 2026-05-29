package inventory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

// StateVersion is the current provider state format.
const StateVersion = 1

const (
	LocalProviderName = "local"
	localGroupComment = "# Group: "
)

var stateDirOverride string

// SetStateDir overrides the state directory for tests.
func SetStateDir(dir string) {
	stateDirOverride = dir
}

// ProviderState holds non-secret state for one external inventory provider.
type ProviderState struct {
	Version               int                      `json:"version"`
	Provider              string                   `json:"provider"`
	Type                  string                   `json:"type"`
	StrictHostKeyChecking bool                     `json:"strict_host_key_checking,omitempty"`
	LastRefresh           time.Time                `json:"last_refresh"`
	LastError             string                   `json:"last_error,omitempty"`
	IncludeFile           string                   `json:"include_file"`
	Objects               map[string]*ProviderHost `json:"objects"`
}

// ProviderHost represents one provider-managed SSH target persisted in state.
type ProviderHost struct {
	ObjectID     string              `json:"object_id"`
	Host         string              `json:"host"`
	Patterns     []string            `json:"patterns,omitempty"`
	Group        string              `json:"group,omitempty"`
	HostName     string              `json:"hostname"`
	Username     string              `json:"username,omitempty"`
	Port         int                 `json:"port,omitempty"`
	ProxyJump    string              `json:"proxy_jump,omitempty"`
	CompatFixes  []compat.CompatType `json:"compat_fixes,omitempty"`
	ProviderType string              `json:"provider_type,omitempty"`
}

func providerStateDir() string {
	if stateDirOverride != "" {
		return filepath.Join(stateDirOverride, "inventory", "providers")
	}
	return filepath.Join(config.DefaultPaths().StateDir, "inventory", "providers")
}

func providerStatePath(provider string) string {
	return filepath.Join(providerStateDir(), provider+".json")
}

// ProviderIncludeFile returns the deterministic provider-owned SSH config file.
func ProviderIncludeFile(provider string) string {
	return filepath.Join("nssh.d", "provider_"+provider+".conf")
}

// LocalProviderIncludeFile returns the implicit local provider SSH config file.
func LocalProviderIncludeFile() string {
	return ProviderIncludeFile(LocalProviderName)
}

// SetLocalHostGroup records the logical local group inside a Host block.
func SetLocalHostGroup(host *sshconfig.HostEntry, group string) {
	if host == nil || strings.TrimSpace(group) == "" {
		return
	}
	lines := make([]string, 0, len(host.Lines)+1)
	for _, line := range host.Lines {
		if strings.HasPrefix(strings.TrimSpace(line), localGroupComment) {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		patterns := host.Patterns
		if len(patterns) == 0 && host.Host != "" {
			patterns = []string{host.Host}
		}
		lines = append(lines, "Host "+strings.Join(patterns, " ")+"\n")
	}
	comment := "  " + localGroupComment + strings.TrimSpace(group) + "\n"
	insertAt := 1
	if len(lines) < insertAt {
		insertAt = len(lines)
	}
	lines = append(lines[:insertAt], append([]string{comment}, lines[insertAt:]...)...)
	host.Lines = lines
}

// LocalHostGroup returns the group marker from a local-provider Host block.
func LocalHostGroup(host *sshconfig.HostEntry, fallback string) string {
	if host == nil {
		return fallback
	}
	for _, line := range host.Lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, localGroupComment) {
			group := strings.TrimSpace(strings.TrimPrefix(trimmed, localGroupComment))
			if group != "" {
				return group
			}
		}
	}
	return fallback
}

// LoadProviderState loads state for a provider. Missing state returns nil.
func LoadProviderState(provider string) (*ProviderState, error) {
	path := providerStatePath(provider)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}

	var state ProviderState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	if state.Version != StateVersion {
		return nil, fmt.Errorf("unsupported provider state version %d in %s (expected %d)", state.Version, path, StateVersion)
	}
	if state.IncludeFile == "" {
		state.IncludeFile = ProviderIncludeFile(state.Provider)
	}
	if state.Objects == nil {
		state.Objects = make(map[string]*ProviderHost)
	}
	return &state, nil
}

// SaveProviderState atomically writes provider state to disk.
func SaveProviderState(state *ProviderState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal provider state: %w", err)
	}
	return atomicWriteFile(providerStatePath(state.Provider), data, 0600)
}

// DeleteProviderState removes provider state.
func DeleteProviderState(provider string) error {
	path := providerStatePath(provider)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state %s: %w", path, err)
	}
	return nil
}

// ListProviderStates returns provider names with state files.
func ListProviderStates() ([]string, error) {
	entries, err := os.ReadDir(providerStateDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read provider state dir: %w", err)
	}
	var providers []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".json" {
			providers = append(providers, name[:len(name)-len(".json")])
		}
	}
	return providers, nil
}

// LoadProviderStateByIncludeFile locates the provider state that owns includeFile.
func LoadProviderStateByIncludeFile(includeFile string) (*ProviderState, error) {
	targetBase := filepath.Base(includeFile)
	providers, err := ListProviderStates()
	if err != nil {
		return nil, err
	}
	for _, provider := range providers {
		state, err := LoadProviderState(provider)
		if err != nil {
			return nil, err
		}
		if state != nil && filepath.Base(state.IncludeFile) == targetBase {
			return state, nil
		}
	}
	return nil, nil
}

// FindHost locates a provider host by primary host name or emitted pattern.
func (s *ProviderState) FindHost(pattern string) *ProviderHost {
	if s == nil {
		return nil
	}
	for _, host := range s.Objects {
		if host.Host == pattern || slices.Contains(host.Patterns, pattern) {
			return host
		}
	}
	return nil
}

// Hosts returns state objects as a slice for rendering.
func (s *ProviderState) Hosts() []*ProviderHost {
	if s == nil {
		return nil
	}
	hosts := make([]*ProviderHost, 0, len(s.Objects))
	for _, host := range s.Objects {
		hosts = append(hosts, host)
	}
	return hosts
}

// HostInfo is a reverse lookup entry for provider-owned hosts.
type HostInfo struct {
	Provider string
	Group    string
}

// BuildProviderIndex scans provider states and maps host patterns to metadata.
func BuildProviderIndex() (map[string]*HostInfo, error) {
	providers, err := ListProviderStates()
	if err != nil {
		return nil, err
	}
	index := make(map[string]*HostInfo)
	for _, provider := range providers {
		state, err := LoadProviderState(provider)
		if err != nil {
			slog.Warn("skip corrupt provider state", "provider", provider, "err", err)
			continue
		}
		if state == nil {
			continue
		}
		for _, host := range state.Objects {
			info := &HostInfo{Provider: provider, Group: host.Group}
			index[host.Host] = info
			for _, pattern := range host.Patterns {
				index[pattern] = info
			}
		}
	}
	return index, nil
}

// PersistCompatFixes updates provider state and rewrites provider-owned output.
func PersistCompatFixes(includeFile, hostPattern string, fixes []compat.CompatType) error {
	if len(fixes) == 0 {
		return nil
	}
	current, err := LoadProviderStateByIncludeFile(includeFile)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("no provider state found for include file %q", includeFile)
	}
	next := cloneProviderState(current)
	host := next.FindHost(hostPattern)
	if host == nil {
		return fmt.Errorf("host %q not found in provider state for %q", hostPattern, includeFile)
	}
	merged := mergeCompatFixes(host.CompatFixes, fixes)
	if slices.Equal(host.CompatFixes, merged) {
		return nil
	}
	host.CompatFixes = merged
	if err := SaveProviderState(next); err != nil {
		return err
	}
	if err := WriteProviderSSHConfig(next.IncludeFile, next.Hosts(), next.Provider, next.Type, next.StrictHostKeyChecking); err != nil {
		if rollbackErr := SaveProviderState(current); rollbackErr != nil {
			return fmt.Errorf("write provider config: %w (rollback state: %v)", err, rollbackErr)
		}
		return fmt.Errorf("write provider config: %w", err)
	}
	return nil
}

func cloneProviderState(state *ProviderState) *ProviderState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.Objects = make(map[string]*ProviderHost, len(state.Objects))
	for id, host := range state.Objects {
		hostClone := *host
		hostClone.Patterns = slices.Clone(host.Patterns)
		hostClone.CompatFixes = slices.Clone(host.CompatFixes)
		clone.Objects[id] = &hostClone
	}
	return &clone
}

func mergeCompatFixes(existing, added []compat.CompatType) []compat.CompatType {
	if len(existing) == 0 && len(added) == 0 {
		return nil
	}
	seen := make(map[compat.CompatType]bool, len(existing)+len(added))
	for _, ct := range existing {
		seen[ct] = true
	}
	for _, ct := range added {
		seen[ct] = true
	}
	merged := make([]compat.CompatType, 0, len(seen))
	for _, ct := range compat.AllCompatTypes() {
		if seen[ct] {
			merged = append(merged, ct)
			delete(seen, ct)
		}
	}
	for _, ct := range existing {
		if seen[ct] {
			merged = append(merged, ct)
			delete(seen, ct)
		}
	}
	for _, ct := range added {
		if seen[ct] {
			merged = append(merged, ct)
			delete(seen, ct)
		}
	}
	return merged
}
