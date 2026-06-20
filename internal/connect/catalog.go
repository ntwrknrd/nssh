package connect

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

type HostCatalog struct {
	hosts   map[string]*ResolvedHostData
	aliases map[string]string
}

type ResolvedHostData struct {
	Query     string
	Canonical string
	Hostname  string
	Aliases   []string
	Provider  string
	Group     string
	Port      int
	Username  string
	Auth      config.InventoryAuthConfig
	SSH       config.SSHHostConfig
}

func BuildHostCatalog(cfg *config.Config) (*HostCatalog, error) {
	states, err := loadProviderStates()
	if err != nil {
		return nil, err
	}
	return buildHostCatalog(cfg, states)
}

func loadProviderStates() ([]*inventory.ProviderState, error) {
	names, err := inventory.ListProviderStates()
	if err != nil {
		return nil, err
	}
	states := make([]*inventory.ProviderState, 0, len(names))
	for _, name := range names {
		state, err := inventory.LoadProviderState(name)
		if err != nil {
			if inventory.IsUnsupportedStateVersion(err) {
				continue
			}
			return nil, err
		}
		if state != nil {
			states = append(states, state)
		}
	}
	return states, nil
}

func buildHostCatalog(cfg *config.Config, states []*inventory.ProviderState) (*HostCatalog, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	cat := &HostCatalog{hosts: make(map[string]*ResolvedHostData), aliases: make(map[string]string)}
	providers := cfg.Inventory.Providers
	if len(providers) == 0 {
		providers = cfg.Inventory.Provider
	}
	for providerName, provider := range providers {
		provider = normalizeCatalogProvider(provider)
		if provider.Type == config.ProviderLocal {
			if err := cat.addLocalHosts(cfg, providerName, provider); err != nil {
				return nil, err
			}
		}
	}
	for _, state := range states {
		if state == nil {
			continue
		}
		provider, ok := providers[state.Provider]
		if !ok {
			continue
		}
		provider = normalizeCatalogProvider(provider)
		for _, host := range state.Objects {
			if host == nil {
				continue
			}
			data := resolvedHostFromState(cfg, state.Provider, provider, host, cat)
			cat.add(data)
		}
	}
	return cat, nil
}

func (c *HostCatalog) addLocalHosts(cfg *config.Config, providerName string, provider config.InventoryProviderConfig) error {
	for name, host := range provider.Hosts {
		if strings.TrimSpace(host.Group) == "" {
			return fmt.Errorf("inventory.providers.%s.hosts.%s.group is required", providerName, name)
		}
		if _, ok := provider.Groups[host.Group]; !ok {
			return fmt.Errorf("inventory.providers.%s.hosts.%s.group references unknown group %q", providerName, name, host.Group)
		}
		auth := cfg.ResolveInventoryAuth(config.InventoryAuthContext{Host: name, Provider: providerName, Group: config.FormatInventoryGroupID(providerName, host.Group)})
		ssh := mergeCatalogSSH(cfg, provider, host.Group, host.SSH)
		c.add(&ResolvedHostData{
			Canonical: name,
			Hostname:  name,
			Aliases:   slices.Clone(host.Aliases),
			Provider:  providerName,
			Group:     host.Group,
			Port:      host.Port,
			Username:  auth.Username,
			Auth:      host.Auth,
			SSH:       ssh,
		})
	}
	return nil
}

func resolvedHostFromState(cfg *config.Config, providerName string, provider config.InventoryProviderConfig, host *inventory.ProviderHost, cat *HostCatalog) *ResolvedHostData {
	group := shortGroup(host.Group)
	overlay := provider.Hosts[host.HostName]
	if overlay.Group == "" {
		if byHost, ok := provider.Hosts[host.Host]; ok {
			overlay = byHost
		}
	}
	if overlay.Group != "" {
		group = overlay.Group
	}
	auth := cfg.ResolveInventoryAuth(config.InventoryAuthContext{
		Host:     host.HostName,
		Provider: providerName,
		Group:    config.FormatInventoryGroupID(providerName, group),
	})
	aliases := slices.Clone(host.Patterns)
	aliases = appendUnique(aliases, overlay.Aliases...)
	ssh := mergeCatalogSSH(cfg, provider, group, overlay.SSH)
	ssh = applyProviderStateSSH(ssh, host, cat)
	canonical := firstNonEmpty(host.Host, firstString(aliases), host.HostName)
	return &ResolvedHostData{
		Canonical: canonical,
		Hostname:  firstNonEmpty(host.HostName, canonical),
		Aliases:   aliases,
		Provider:  providerName,
		Group:     group,
		Port:      firstNonZero(overlay.Port, host.Port),
		Username:  auth.Username,
		Auth:      overlay.Auth,
		SSH:       ssh,
	}
}

func mergeCatalogSSH(cfg *config.Config, provider config.InventoryProviderConfig, group string, host config.SSHHostConfig) config.SSHHostConfig {
	ssh := config.SSHHostConfig{}
	if cfg != nil {
		ssh = config.MergeSSH(ssh, cfg.SSH.Defaults)
	}
	if groupCfg, ok := provider.Groups[group]; ok {
		ssh = config.MergeSSH(ssh, groupCfg.SSH)
	}
	return config.MergeSSH(ssh, host)
}

func applyProviderStateSSH(ssh config.SSHHostConfig, host *inventory.ProviderHost, cat *HostCatalog) config.SSHHostConfig {
	if host == nil || strings.TrimSpace(host.ProxyJump) == "" || hasSSHOption(ssh.Options, "ProxyJump") {
		return ssh
	}
	proxyJump := strings.TrimSpace(host.ProxyJump)
	if jump, ok := cat.Find(proxyJump); ok {
		proxyJump = formatProxyJumpTarget(jump)
	}
	if ssh.Options == nil {
		ssh.Options = make(config.SSHOptions)
	}
	ssh.Options["ProxyJump"] = config.NewSSHOptionString(proxyJump)
	return ssh
}

func formatProxyJumpTarget(host *ResolvedHostData) string {
	if host == nil {
		return ""
	}
	target := firstNonEmpty(host.Hostname, host.Canonical)
	if target == "" {
		return ""
	}
	if host.Port != 0 && host.Port != 22 {
		if strings.Contains(target, ":") && !strings.HasPrefix(target, "[") {
			target = "[" + target + "]"
		}
		target = fmt.Sprintf("%s:%d", target, host.Port)
	}
	if host.Username != "" {
		target = host.Username + "@" + target
	}
	return target
}

func hasSSHOption(options config.SSHOptions, key string) bool {
	for existing := range options {
		if strings.EqualFold(existing, key) {
			return true
		}
	}
	return false
}

func (c *HostCatalog) add(host *ResolvedHostData) {
	if host == nil || strings.TrimSpace(host.Canonical) == "" {
		return
	}
	if host.Port == 0 {
		host.Port = 22
	}
	canonical := strings.TrimSpace(host.Canonical)
	host.Canonical = canonical
	c.hosts[canonical] = host
	c.aliases[canonical] = canonical
	if short := shortName(canonical); short != "" {
		c.aliases[short] = canonical
	}
	if short := shortName(host.Hostname); short != "" {
		c.aliases[short] = canonical
	}
	if hostname := strings.TrimSpace(host.Hostname); hostname != "" {
		c.aliases[hostname] = canonical
	}
	for _, alias := range host.Aliases {
		alias = strings.TrimSpace(alias)
		if alias != "" {
			c.aliases[alias] = canonical
		}
	}
}

func (c *HostCatalog) Find(query string) (*ResolvedHostData, bool) {
	if c == nil {
		return nil, false
	}
	canonical, ok := c.aliases[strings.TrimSpace(query)]
	if !ok {
		return nil, false
	}
	host, ok := c.hosts[canonical]
	if ok {
		copy := *host
		copy.Query = query
		return &copy, true
	}
	return nil, false
}

func (c *HostCatalog) ResolveQuery(query string) (string, error) {
	host, ok := c.Find(query)
	if !ok {
		if strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("no hosts found in nssh inventory")
		}
		return "", &HostNotFoundError{Hostname: query}
	}
	return host.Canonical, nil
}

func shortGroup(group string) string {
	_, short, err := config.ParseInventoryGroupID(group)
	if err == nil {
		return short
	}
	return strings.TrimSpace(group)
}

func shortName(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" || !strings.Contains(host, ".") {
		return ""
	}
	short, _, _ := strings.Cut(host, ".")
	return short
}

func appendUnique(base []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(base, value) {
			base = append(base, value)
		}
	}
	return base
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func normalizeCatalogProvider(provider config.InventoryProviderConfig) config.InventoryProviderConfig {
	if provider.Groups == nil && provider.Group != nil {
		provider.Groups = provider.Group
	}
	if provider.Group == nil && provider.Groups != nil {
		provider.Group = provider.Groups
	}
	if provider.Hosts == nil {
		provider.Hosts = make(map[string]config.InventoryHostConfig)
	}
	if provider.Groups == nil {
		provider.Groups = make(map[string]config.GroupConfig)
	}
	if provider.Group == nil {
		provider.Group = provider.Groups
	}
	return provider
}

func (c *HostCatalog) Suggestions(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]bool, len(c.hosts))
	out := make([]string, 0, len(c.hosts))
	for canonical, host := range c.hosts {
		if query == "" || hostMatchesQuery(host, query) {
			if !seen[canonical] {
				seen[canonical] = true
				out = append(out, canonical)
			}
		}
	}
	slices.Sort(out)
	return out
}

func hostMatchesQuery(host *ResolvedHostData, query string) bool {
	if host == nil {
		return false
	}
	values := make([]string, 0, 2+len(host.Aliases))
	values = append(values, host.Canonical, host.Hostname)
	values = append(values, host.Aliases...)
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), query) {
			return true
		}
	}
	return false
}

func (c *HostCatalog) All() []*ResolvedHostData {
	out := make([]*ResolvedHostData, 0, len(c.hosts))
	for _, host := range c.hosts {
		copy := *host
		out = append(out, &copy)
	}
	slices.SortFunc(out, func(a, b *ResolvedHostData) int {
		return strings.Compare(a.Canonical, b.Canonical)
	})
	return out
}
