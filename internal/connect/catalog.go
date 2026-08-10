package connect

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

type HostCatalog struct {
	hosts   map[string]*ResolvedHostData
	aliases map[string]string
}

type ResolvedHostData struct {
	Query        string
	Canonical    string
	Hostname     string
	Aliases      []string
	Provider     string
	Group        string
	Port         int
	Username     string
	Auth         config.InventoryAuthConfig
	SSH          config.SSHHostConfig
	Highlight    config.HighlightConfig
	ManagedProxy *ResolvedHostData
}

func BuildHostCatalog(cfg *config.Config) (*HostCatalog, error) {
	timer := connector.StartTiming(connector.TimingCatalogTotal)
	defer timer.Emit()

	states, err := loadProviderStates()
	if err != nil {
		return nil, err
	}
	return buildHostCatalog(cfg, states)
}

func loadProviderStates() ([]*inventory.ProviderState, error) {
	listTimer := connector.StartTiming(connector.TimingProviderStateList)
	names, err := inventory.ListProviderStates()
	listTimer.Emit()
	if err != nil {
		return nil, err
	}
	loadTimer := connector.StartTiming(connector.TimingProviderStateLoad)
	defer loadTimer.Emit()

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
	localTimer := connector.StartTiming(connector.TimingCatalogLocalHosts)
	for providerName, provider := range providers {
		provider = normalizeCatalogProvider(provider)
		if provider.Type == config.ProviderLocal {
			if err := cat.addLocalHosts(cfg, providerName, provider); err != nil {
				localTimer.Emit()
				return nil, err
			}
		}
	}
	localTimer.Emit()

	providerTimer := connector.StartTiming(connector.TimingCatalogProviderHosts)
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
			data := resolvedHostFromState(cfg, state, provider, host)
			cat.add(data)
		}
	}
	providerTimer.Emit()
	cat.resolveManagedProxies()
	return cat, nil
}

func (c *HostCatalog) addLocalHosts(cfg *config.Config, providerName string, provider config.InventoryProviderConfig) error {
	for name, host := range provider.Hosts {
		if strings.TrimSpace(host.Group) != "" {
			if _, ok := provider.Groups[host.Group]; !ok {
				return fmt.Errorf("inventory.providers.%s.hosts.%s.group references unknown group %q", providerName, name, host.Group)
			}
		}
		auth := cfg.ResolveInventoryAuth(config.InventoryAuthContext{Host: name, Provider: providerName, Group: catalogGroupID(providerName, host.Group)})
		ssh := mergeCatalogSSH(cfg, provider, host.Group, host.SSH)
		ssh = applyAuthModeSSH(ssh, auth.AuthMode)
		highlight := mergeCatalogHighlight(cfg, provider, host.Group, host.Highlight)
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
			Highlight: highlight,
		})
	}
	return nil
}

func catalogGroupID(providerName, group string) string {
	if strings.TrimSpace(group) == "" {
		return ""
	}
	return config.FormatInventoryGroupID(providerName, group)
}

func resolvedHostFromState(cfg *config.Config, state *inventory.ProviderState, provider config.InventoryProviderConfig, host *inventory.ProviderHost) *ResolvedHostData {
	providerName := ""
	if state != nil {
		providerName = state.Provider
	}
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
		Group:    catalogGroupID(providerName, group),
	})
	aliases := slices.Clone(host.Patterns)
	aliases = appendUnique(aliases, overlay.Aliases...)
	ssh := mergeProviderStateSSH(cfg, state, provider, group, overlay.SSH)
	ssh = applyProviderStateSSH(ssh, host, state)
	ssh = applyAuthModeSSH(ssh, auth.AuthMode)
	highlight := mergeCatalogHighlight(cfg, provider, group, overlay.Highlight)
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
		Highlight: highlight,
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

func mergeProviderStateSSH(cfg *config.Config, state *inventory.ProviderState, provider config.InventoryProviderConfig, group string, host config.SSHHostConfig) config.SSHHostConfig {
	if state != nil && state.Type == config.ProviderContainerlab {
		ssh := containerlabInheritedSSHDefaults(cfg, provider.Config)
		if groupCfg, ok := provider.Groups[group]; ok {
			ssh = config.MergeSSH(ssh, groupCfg.SSH)
		}
		return config.MergeSSH(ssh, host)
	}
	return mergeCatalogSSH(cfg, provider, group, host)
}

func containerlabInheritedSSHDefaults(cfg *config.Config, detail config.InventoryProviderDetailConfig) config.SSHHostConfig {
	if cfg == nil {
		return config.SSHHostConfig{}
	}
	if len(detail.SSHDefaults.Options) > 0 {
		return config.SSHHostConfig{
			Options: selectedSSHDefaultOptions(cfg.SSH.Defaults.Options, detail.SSHDefaults.Options),
		}
	}
	switch strings.ToLower(strings.TrimSpace(detail.SSHDefaults.Mode)) {
	case "", "all":
		return config.MergeSSH(config.SSHHostConfig{}, cfg.SSH.Defaults)
	case "none":
		return config.SSHHostConfig{}
	}
	return config.SSHHostConfig{}
}

func selectedSSHDefaultOptions(defaults config.SSHOptions, names []string) config.SSHOptions {
	selected := make(config.SSHOptions)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for key, value := range defaults {
			if strings.EqualFold(key, name) {
				selected[key] = value
				break
			}
		}
	}
	return config.MergeSSH(config.SSHHostConfig{}, config.SSHHostConfig{Options: selected}).Options
}

func applyAuthModeSSH(ssh config.SSHHostConfig, authMode string) config.SSHHostConfig {
	if authMode != config.AuthModePassword {
		return ssh
	}
	if ssh.Options == nil {
		ssh.Options = make(config.SSHOptions)
	}
	ssh.Options["PreferredAuthentications"] = config.NewSSHOptionString("keyboard-interactive,password")
	ssh.Options["PubkeyAuthentication"] = config.NewSSHOptionBool(false)
	return ssh
}

func mergeCatalogHighlight(cfg *config.Config, provider config.InventoryProviderConfig, group string, host config.HighlightConfig) config.HighlightConfig {
	highlight := config.HighlightConfig{}
	if cfg != nil {
		highlight = config.MergeHighlight(highlight, cfg.Highlight)
	}
	if groupCfg, ok := provider.Groups[group]; ok {
		highlight = config.MergeHighlight(highlight, groupCfg.Highlight)
	}
	return config.MergeHighlight(highlight, host)
}

func applyProviderStateSSH(ssh config.SSHHostConfig, host *inventory.ProviderHost, state *inventory.ProviderState) config.SSHHostConfig {
	if state != nil && state.Type == config.ProviderContainerlab && !state.StrictHostKeyChecking {
		if ssh.Options == nil {
			ssh.Options = make(config.SSHOptions)
		}
		setSSHOptionIfAbsent(ssh.Options, "StrictHostKeyChecking", config.NewSSHOptionString("no"))
		setSSHOptionIfAbsent(ssh.Options, "UserKnownHostsFile", config.NewSSHOptionString("/dev/null"))
		setSSHOptionIfAbsent(ssh.Options, "GlobalKnownHostsFile", config.NewSSHOptionString("/dev/null"))
		setSSHOptionIfAbsent(ssh.Options, "LogLevel", config.NewSSHOptionString("ERROR"))
		setSSHOptionIfAbsent(ssh.Options, "WarnWeakCrypto", config.NewSSHOptionString("no-pq-kex"))
	}
	if host == nil || strings.TrimSpace(host.ProxyJump) == "" || hasSSHOption(ssh.Options, "ProxyJump") || hasSSHOption(ssh.Options, "ProxyCommand") {
		return ssh
	}
	if ssh.Options == nil {
		ssh.Options = make(config.SSHOptions)
	}
	ssh.Options["ProxyJump"] = config.NewSSHOptionString(strings.TrimSpace(host.ProxyJump))
	return ssh
}

func (c *HostCatalog) resolveManagedProxies() {
	if c == nil {
		return
	}
	for _, host := range c.hosts {
		if host == nil || hasSSHOption(host.SSH.Options, "ProxyCommand") {
			continue
		}
		_, target, ok := sshOption(host.SSH.Options, "ProxyJump")
		if !ok || strings.TrimSpace(target) == "" || strings.Contains(target, ",") {
			continue
		}
		jump, ok := c.findProxyJumpHost(target)
		if !ok || hasSSHOption(jump.SSH.Options, "ProxyJump") || hasSSHOption(jump.SSH.Options, "ProxyCommand") {
			continue
		}
		host.ManagedProxy = jump
	}
}

func sshOption(options config.SSHOptions, name string) (string, string, bool) {
	for key, value := range options {
		if strings.EqualFold(key, name) {
			return key, value.StringValue(), true
		}
	}
	return "", "", false
}

func (c *HostCatalog) findProxyJumpHost(target string) (*ResolvedHostData, bool) {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, ",") {
		return nil, false
	}
	if jump, ok := c.Find(target); ok {
		return jump, true
	}
	user, host, port := splitProxyJumpTarget(target)
	jump, ok := c.Find(host)
	if !ok {
		return nil, false
	}
	clone := *jump
	clone.Aliases = slices.Clone(jump.Aliases)
	clone.SSH = config.MergeSSH(config.SSHHostConfig{}, jump.SSH)
	jump = &clone
	if user != "" {
		jump.Username = user
	}
	if port != 0 {
		jump.Port = port
	}
	return jump, true
}

func splitProxyJumpTarget(target string) (user, host string, port int) {
	target = strings.TrimSpace(target)
	if at := strings.LastIndex(target, "@"); at != -1 {
		user = strings.TrimSpace(target[:at])
		target = strings.TrimSpace(target[at+1:])
	}
	host = target
	if strings.HasPrefix(target, "[") {
		if end := strings.LastIndex(target, "]"); end != -1 {
			host = target[1:end]
			if len(target) > end+2 && target[end+1] == ':' {
				port = parsePort(target[end+2:])
			}
		}
		return user, host, port
	}
	if colon := strings.LastIndex(target, ":"); colon != -1 && strings.Count(target, ":") == 1 {
		host = strings.TrimSpace(target[:colon])
		port = parsePort(target[colon+1:])
	}
	return user, host, port
}

func parsePort(value string) int {
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &port); err != nil || port <= 0 {
		return 0
	}
	return port
}

func setSSHOptionIfAbsent(options config.SSHOptions, key string, value config.SSHOptionValue) {
	if !hasSSHOption(options, key) {
		options[key] = value
	}
}

func formatManagedProxyCommand(host *ResolvedHostData, forwardHost string, forwardPort int) string {
	if host == nil {
		return ""
	}
	target := formatProxyJumpTarget(host)
	forward := formatProxyForwardTarget(forwardHost, forwardPort)
	if target == "" || forward == "" {
		return ""
	}
	args := connector.RenderSSHOptions(host.SSH, 0)
	argv := make([]string, 0, len(args)+4)
	argv = append(argv, "ssh")
	argv = append(argv, args...)
	argv = append(argv, "-W", forward, target)
	for i := range argv {
		argv[i] = escapeProxyCommandTokenExpansion(argv[i])
	}
	return managedProxyAskpassPrefix + shellJoin(argv)
}

const managedProxyAskpassPrefix = `/bin/sh -c 'exec env SSH_ASKPASS="${NSSH_PROXY_SSH_ASKPASS:-}" SSH_ASKPASS_REQUIRE="${NSSH_PROXY_ASKPASS_REQUIRE:-never}" DISPLAY=nssh-askpass NSSH_ASKPASS_SOCKET="${NSSH_PROXY_ASKPASS_SOCKET:-}" NSSH_ASKPASS_NONCE="${NSSH_PROXY_ASKPASS_NONCE:-}" "$@"' nssh-proxy `

// RenderManagedProxyCommand renders an inventory-resolved host as an OpenSSH
// ProxyCommand. Nested proxy configurations remain OpenSSH-native.
func RenderManagedProxyCommand(host *ResolvedHost, forwardHost string, forwardPort int) string {
	if host == nil || hasSSHOption(host.SSH.Options, "ProxyJump") || hasSSHOption(host.SSH.Options, "ProxyCommand") {
		return ""
	}
	return formatManagedProxyCommand(&ResolvedHostData{
		Canonical: host.Canonical,
		Hostname:  host.Hostname,
		Port:      host.Port,
		Username:  host.Username,
		SSH:       host.SSH,
	}, forwardHost, forwardPort)
}

func formatProxyForwardTarget(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port == 0 {
		port = 22
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%d", host, port)
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

func escapeProxyCommandTokenExpansion(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}

func shellJoin(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\r\n'\"\\$`!*?[]{}()<>|&;#~") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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
