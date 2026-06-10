package repl

import (
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/cli/selection"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

type DefaultTargetResolver struct{}

func (DefaultTargetResolver) ResolveHost(host string) (string, error) {
	if net.ParseIP(host) != nil {
		return host, nil
	}
	resolved, err := connect.ResolveHostname(host)
	if err == nil {
		return resolved, nil
	}
	var notFound *connect.HostNotFoundError
	if errors.As(err, &notFound) {
		return host, nil
	}
	return "", err
}

func (DefaultTargetResolver) SelectHosts(selector string) ([]string, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, err
	}
	hosts, err := sshconfig.NewParser().GetAllHosts()
	if err != nil {
		return nil, err
	}
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		return nil, err
	}
	compiled, err := selection.Compile(selector, []string{"host", "hostname", "id", "user", "port", "provider", "group"})
	if err != nil {
		return nil, err
	}
	paths := config.DefaultPaths()
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Host < hosts[j].Host })
	var matched []string
	for _, host := range hosts {
		meta := replMetadataForHost(host, cfg, paths, index)
		if compiled.Match(selection.Row{
			"host":     host.Host,
			"hostname": host.HostName,
			"id":       sshconfig.DeriveHostID(host.Host),
			"user":     host.User(),
			"port":     host.Port(),
			"provider": meta.provider,
			"group":    meta.group,
		}) {
			matched = append(matched, host.Host)
		}
	}
	return matched, nil
}

func (DefaultTargetResolver) SuggestHosts(prefix string) ([]string, error) {
	hosts, err := sshconfig.NewParser().GetAllHosts()
	if err != nil {
		return nil, err
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	seen := make(map[string]bool, len(hosts))
	var matched []string
	for _, host := range hosts {
		for _, candidate := range suggestableHostNames(host) {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" || seen[candidate] {
				continue
			}
			if prefix == "" || strings.HasPrefix(strings.ToLower(candidate), prefix) {
				seen[candidate] = true
				matched = append(matched, candidate)
			}
		}
	}
	matched = shortestHostSuggestions(matched)
	sort.Slice(matched, func(i, j int) bool {
		return strings.ToLower(matched[i]) < strings.ToLower(matched[j])
	})
	return matched, nil
}

func suggestableHostNames(host *sshconfig.HostEntry) []string {
	if host == nil {
		return nil
	}
	names := make([]string, 0, (len(host.Patterns)+1)*2)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || slicesContains(names, name) {
			return
		}
		names = append(names, name)
		if short := shortHostSuggestion(name); short != "" && !slicesContains(names, short) {
			names = append(names, short)
		}
	}
	if host.Host != "" {
		add(host.Host)
	}
	for _, pattern := range host.Patterns {
		if strings.HasPrefix(pattern, "*") || strings.HasPrefix(pattern, "?") {
			continue
		}
		add(pattern)
	}
	return names
}

func shortestHostSuggestions(values []string) []string {
	byKey := make(map[string]string, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := hostSuggestionKey(value)
		if prev, ok := byKey[key]; !ok || len(value) < len(prev) || (len(value) == len(prev) && strings.ToLower(value) < strings.ToLower(prev)) {
			byKey[key] = value
		}
	}
	result := make([]string, 0, len(byKey))
	for _, value := range byKey {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func hostSuggestionKey(value string) string {
	if short := shortHostSuggestion(value); short != "" {
		return strings.ToLower(short)
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func shortHostSuggestion(value string) string {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	if value == "" || !strings.Contains(value, ".") || strings.Contains(value, ":") {
		return ""
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return ""
	}
	short, _, ok := strings.Cut(value, ".")
	if !ok || short == "" {
		return ""
	}
	return short
}

func slicesContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

type replHostMetadata struct {
	provider string
	group    string
}

func replMetadataForHost(
	host *sshconfig.HostEntry,
	cfg *config.Config,
	paths *config.Paths,
	index map[string]*inventory.HostInfo,
) replHostMetadata {
	if host == nil {
		return replHostMetadata{provider: inventory.LocalProviderName, group: "-"}
	}
	if index != nil {
		if info := index[host.Host]; info != nil {
			return replHostMetadata{provider: info.Provider, group: info.Group}
		}
		for _, pattern := range host.Patterns {
			if info := index[pattern]; info != nil {
				return replHostMetadata{provider: info.Provider, group: info.Group}
			}
		}
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	for provider := range cfg.Inventory.Provider {
		if provider == inventory.LocalProviderName {
			continue
		}
		if samePath(host.SourceFile, filepath.Join(paths.SSHConfigDir, inventory.ProviderIncludeFile(provider))) {
			return replHostMetadata{provider: provider, group: "-"}
		}
	}
	if samePath(host.SourceFile, filepath.Join(paths.SSHConfigDir, inventory.LocalProviderIncludeFile())) {
		return replHostMetadata{provider: inventory.LocalProviderName, group: inventory.LocalHostGroup(host, "-")}
	}
	return replHostMetadata{provider: inventory.LocalProviderName, group: "-"}
}

func samePath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return filepath.Clean(absA) == filepath.Clean(absB)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
