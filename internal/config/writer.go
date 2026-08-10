package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarshalSparse renders a hand-editable YAML config.
func MarshalSparse(cfg *Config) (string, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return marshalConfigTable(sparseConfigTable(cfg))
}

func SaveSparse(path string, cfg *Config) error {
	text, err := MarshalSparse(cfg)
	if err != nil {
		return err
	}
	return writeConfigText(path, text)
}

func SaveInventoryGroup(path string, cfg *Config, groupID string) error {
	targetPath, err := inventoryGroupSavePath(path, cfg, groupID)
	if err != nil {
		return err
	}
	root, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return err
	}
	if err := setInventoryGroupInRoot(root, cfg, groupID); err != nil {
		return err
	}
	return saveInventoryProviderTable(targetPath, root)
}

func SaveInventoryGroupAndHostAuth(path string, cfg *Config, groupID, host string) error {
	targetPath, err := inventoryGroupSavePath(path, cfg, groupID)
	if err != nil {
		return err
	}
	groupRoot, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return err
	}
	if err := setInventoryGroupInRoot(groupRoot, cfg, groupID); err != nil {
		return err
	}
	if sameConfigPath(path, targetPath) {
		if err := setInventoryHostAuthInRoot(groupRoot, path, cfg, host); err != nil {
			return err
		}
		return saveRootTable(path, groupRoot)
	}
	if err := saveInventoryProviderTable(targetPath, groupRoot); err != nil {
		return err
	}
	hostRoot := rootTableForSave(cfg)
	if err := setInventoryHostAuthInRoot(hostRoot, path, cfg, host); err != nil {
		return err
	}
	return saveRootTable(path, hostRoot)
}

func SaveInventoryProviderHost(path string, cfg *Config, providerName, host string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	targetPath, err := inventoryProviderHostSavePath(path, cfg, providerName)
	if err != nil {
		return err
	}
	root, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return err
	}
	if err := setInventoryProviderHostInRoot(root, cfg, providerName, host); err != nil {
		return err
	}
	return saveInventoryProviderTable(targetPath, root)
}

func inventoryGroupSavePath(rootPath string, cfg *Config, groupID string) (string, error) {
	providerName, _, err := ParseInventoryGroupID(groupID)
	if err != nil {
		return "", err
	}
	if cfg != nil {
		if source := cfg.InventoryProviderSource(providerName); source != "" {
			return source, nil
		}
	}
	return rootPath, nil
}

func inventoryProviderHostSavePath(rootPath string, cfg *Config, providerName string) (string, error) {
	if strings.TrimSpace(providerName) == "" {
		return "", fmt.Errorf("inventory provider is required")
	}
	if cfg != nil {
		if source := cfg.InventoryProviderSource(providerName); source != "" {
			return source, nil
		}
	}
	return rootPath, nil
}

func inventoryGroupDeletePath(rootPath string, cfg *Config, providerName, groupName string) string {
	if cfg != nil {
		if source := cfg.InventoryGroupSource(providerName, groupName); source != "" {
			return source
		}
		if source := cfg.InventoryProviderSource(providerName); source != "" {
			return source
		}
	}
	return rootPath
}

func inventoryHostAuthDeletePath(rootPath string, cfg *Config, host string) string {
	if cfg != nil {
		if source := cfg.InventoryHostSource(host); source != "" {
			return source
		}
	}
	return rootPath
}

func rootTableForPath(rootPath string, cfg *Config, targetPath string) (map[string]any, error) {
	if sameConfigPath(rootPath, targetPath) {
		return rootTableForSave(cfg), nil
	}
	table, err := readYAMLMap(targetPath)
	if err != nil {
		return nil, err
	}
	return table, nil
}

func setInventoryGroupInRoot(root map[string]any, cfg *Config, groupID string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	providerName, groupName, err := ParseInventoryGroupID(groupID)
	if err != nil {
		return err
	}
	providerCfg, ok := cfg.Inventory.Provider[providerName]
	if !ok {
		return fmt.Errorf("inventory provider %q is not configured", providerName)
	}
	groupCfg, ok := providerCfg.Group[groupName]
	if !ok {
		return fmt.Errorf("inventory group %q is not configured", groupID)
	}
	setMapPath(root, inventoryProviderGroupPath(root, providerName, groupName), groupTable(groupCfg))
	return nil
}

func inventoryProviderGroupPath(root map[string]any, providerName, groupName string) []string {
	if tablePathDefined(root, "inventory", "providers", providerName) || tablePathDefined(root, "inventory", "providers") {
		return []string{"inventory", "providers", providerName, "groups", groupName}
	}
	if tablePathDefined(root, "provider", providerName) && !tablePathDefined(root, "inventory", "provider", providerName) {
		return []string{"provider", providerName, "group", groupName}
	}
	return []string{"inventory", "provider", providerName, "group", groupName}
}

func inventoryProviderHostPath(root map[string]any, providerName, host string) []string {
	if tablePathDefined(root, "inventory", "providers", providerName) || tablePathDefined(root, "inventory", "providers") {
		return []string{"inventory", "providers", providerName, "hosts", host}
	}
	if tablePathDefined(root, "provider", providerName) && !tablePathDefined(root, "inventory", "provider", providerName) {
		return []string{"provider", providerName, "host", host}
	}
	return []string{"inventory", "provider", providerName, "host", host}
}

func inventoryHostPath(root map[string]any, host string) []string {
	if tablePathDefined(root, "inventory", "hosts", host) || tablePathDefined(root, "inventory", "hosts") {
		return []string{"inventory", "hosts", host}
	}
	if tablePathDefined(root, "host", host) && !tablePathDefined(root, "inventory", "host", host) {
		return []string{"host", host}
	}
	return []string{"inventory", "host", host}
}

func saveInventoryProviderTable(path string, table map[string]any) error {
	return saveRootTable(path, table)
}

func SaveInventoryHostAuth(path string, cfg *Config, host string) error {
	root := rootTableForSave(cfg)
	if err := setInventoryHostAuthInRoot(root, path, cfg, host); err != nil {
		return err
	}
	return saveRootTable(path, root)
}

func InventoryHostAuthConfigText(path string, cfg *Config, host string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is required")
	}
	hostCfg, ok := cfg.Inventory.Host[host]
	if !ok || (!hostCfg.Auth.IsSet() && !hostCfg.AuthDisabled) {
		return "", nil
	}
	targetPath := inventoryHostAuthDeletePath(path, cfg, host)
	root, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return "", err
	}
	out := make(map[string]any)
	setMapPath(out, inventoryHostPath(root, host), inventoryHostTable(hostCfg))
	return marshalConfigTable(out)
}

func DeleteInventoryHostAuth(path string, cfg *Config, host string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	targetPath := inventoryHostAuthDeletePath(path, cfg, host)
	root, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return err
	}
	deleteMapPath(root, inventoryHostPath(root, host))
	return saveInventoryProviderTable(targetPath, root)
}

func setInventoryHostAuthInRoot(root map[string]any, path string, cfg *Config, host string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	hostCfg, ok := cfg.Inventory.Host[host]
	if !ok || (!hostCfg.Auth.IsSet() && !hostCfg.AuthDisabled) {
		if isImportedOnlySource(path, cfg.InventoryHostSource(host)) {
			return fmt.Errorf("inventory host %q auth is imported from %s", host, cfg.InventoryHostSource(host))
		}
		deleteMapPath(root, inventoryHostPath(root, host))
		return nil
	}
	setMapPath(root, inventoryHostPath(root, host), inventoryHostTable(hostCfg))
	return nil
}

func setInventoryProviderHostInRoot(root map[string]any, cfg *Config, providerName, host string) error {
	providerCfg, ok := cfg.Inventory.Provider[providerName]
	if !ok {
		providerCfg, ok = cfg.Inventory.Providers[providerName]
	}
	if !ok {
		return fmt.Errorf("inventory provider %q is not configured", providerName)
	}
	providerCfg.syncAliasFields()
	hostCfg, ok := providerCfg.Hosts[host]
	if !ok {
		return fmt.Errorf("inventory provider %q host %q is not configured", providerName, host)
	}
	setMapPath(root, inventoryProviderHostPath(root, providerName, host), inventoryHostTable(hostCfg))
	return nil
}

func InventoryGroupConfigText(path string, cfg *Config, groupID string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is required")
	}
	providerName, groupName, err := ParseInventoryGroupID(groupID)
	if err != nil {
		return "", err
	}
	groupCfg, ok := cfg.Inventory.ProviderGroup(providerName, groupName)
	if !ok {
		return "", nil
	}
	targetPath := inventoryGroupDeletePath(path, cfg, providerName, groupName)
	root, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return "", err
	}
	out := make(map[string]any)
	setMapPath(out, inventoryProviderGroupPath(root, providerName, groupName), groupTable(groupCfg))
	return marshalConfigTable(out)
}

func DeleteInventoryGroup(path string, cfg *Config, groupID string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	providerName, groupName, err := ParseInventoryGroupID(groupID)
	if err != nil {
		return err
	}
	targetPath := inventoryGroupDeletePath(path, cfg, providerName, groupName)
	root, err := rootTableForPath(path, cfg, targetPath)
	if err != nil {
		return err
	}
	deleteMapPath(root, inventoryProviderGroupPath(root, providerName, groupName))
	return saveInventoryProviderTable(targetPath, root)
}

func saveRootTable(path string, table map[string]any) error {
	stripLegacyIdentityKeys(table)
	text, err := marshalConfigTable(table)
	if err != nil {
		return err
	}
	return writeConfigText(path, text)
}

func writeConfigText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, []byte(text), 0600)
}

func stripLegacyIdentityKeys(root map[string]any) {
	if root == nil {
		return
	}
	if host, ok := asMap(root["host"]); ok {
		if defaults, ok := asMap(host["defaults"]); ok {
			delete(defaults, "default_user")
			if len(defaults) == 0 {
				delete(host, "defaults")
			}
		}
		if len(host) == 0 {
			delete(root, "host")
		}
	}
	inventory, ok := asMap(root["inventory"])
	if !ok {
		return
	}
	groups, _ := asMap(inventory["group"])
	for _, name := range sortedMapKeys(groups) {
		group, _ := asMap(groups[name])
		delete(group, "default_user")
	}
}

func rootTableForSave(cfg *Config) map[string]any {
	if cfg != nil && cfg.document != nil && cfg.document.root != nil {
		return cloneMap(cfg.document.root)
	}
	return make(map[string]any)
}

func isImportedOnlySource(rootPath, source string) bool {
	if source == "" {
		return false
	}
	return !sameConfigPath(rootPath, source)
}

func sameConfigPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return filepath.Clean(absA) == filepath.Clean(absB)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func setMapPath(root map[string]any, parts []string, value any) {
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := asMap(current[part])
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func deleteMapPath(root map[string]any, parts []string) {
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := asMap(current[part])
		if !ok {
			return
		}
		current = next
	}
	delete(current, parts[len(parts)-1])
}

func sparseConfigTable(cfg *Config) map[string]any {
	cfg.syncSchemaAliases()
	table := make(map[string]any)
	if len(cfg.Include) > 0 {
		table["include"] = cfg.Include
	}

	agent := make(map[string]any)
	if cfg.Agent.AutoStart {
		agent["auto_start"] = cfg.Agent.AutoStart
	}
	if cfg.Agent.IdleTimeout.Duration() > 0 {
		agent["idle_timeout"] = cfg.Agent.IdleTimeout.Duration().String()
	}
	if cfg.Agent.ActivityIncrement.Duration() > 0 {
		agent["activity_increment"] = cfg.Agent.ActivityIncrement.Duration().String()
	}
	if cfg.Agent.MaxLifetime.Duration() > 0 {
		agent["max_lifetime"] = cfg.Agent.MaxLifetime.Duration().String()
	}
	if cfg.Agent.ProviderRequestTimeout.Duration() > 0 {
		agent["provider_request_timeout"] = cfg.Agent.ProviderRequestTimeout.Duration().String()
	}
	if len(agent) > 0 {
		table["agent"] = agent
	}

	if len(cfg.Credential.Provider) > 0 {
		providers := make(map[string]any)
		for _, name := range sortedCredentialProviders(cfg.Credential.Provider) {
			providers[name] = credentialProviderTable(cfg.Credential.Provider[name])
		}
		table["credential"] = map[string]any{"provider": providers}
	}

	inventory := make(map[string]any)
	auth := authTable(cfg.Inventory.Auth)
	if len(auth) > 0 {
		inventory["auth"] = auth
	}
	if len(cfg.Inventory.Providers) > 0 {
		providers := make(map[string]any)
		for _, name := range sortedInventoryProviders(cfg.Inventory.Providers) {
			providers[name] = inventoryProviderTable(cfg.Inventory.Providers[name])
		}
		inventory["providers"] = providers
	}
	if len(inventory) > 0 {
		table["inventory"] = inventory
	}

	logging := loggingTable(cfg.Logging)
	if len(logging) > 0 {
		table["logging"] = logging
	}

	ssh := sshTable(cfg.SSH)
	if len(ssh) > 0 {
		table["ssh"] = ssh
	}
	highlight := highlightTable(cfg.Highlight)
	if len(highlight) > 0 {
		table["highlight"] = highlight
	}

	return table
}

func credentialProviderTable(cfg CredentialProviderConfig) map[string]any {
	cfg.syncDetailFields()
	table := make(map[string]any)
	if cfg.Type != "" {
		table["type"] = cfg.Type
	}
	addString(table, "account", cfg.Account)
	addString(table, "vault", cfg.Vault)
	addString(table, "command", cfg.Command)
	addString(table, "prefix", cfg.Prefix)
	addString(table, "file", cfg.File)
	addString(table, "age_key_file", cfg.AgeKeyFile)
	if cfg.WarmSession {
		table["warm_session"] = cfg.WarmSession
	}
	if cfg.Keepalive {
		table["keepalive"] = cfg.Keepalive
	}
	if cfg.KeepaliveInterval.Duration() > 0 {
		table["keepalive_interval"] = cfg.KeepaliveInterval.Duration().String()
	}
	if cfg.KeepaliveTimeout.Duration() > 0 {
		table["keepalive_timeout"] = cfg.KeepaliveTimeout.Duration().String()
	}
	return table
}

func groupTable(cfg GroupConfig) map[string]any {
	table := make(map[string]any)
	if len(cfg.DomainSuffix) > 0 {
		table["domain_suffix"] = cfg.DomainSuffix
	}
	auth := authTable(cfg.Auth)
	if len(auth) > 0 {
		table["auth"] = auth
	}
	if len(cfg.Match) > 0 {
		table["match"] = inventoryMatchTable(cfg.Match)
	}
	ssh := sshHostTable(cfg.SSH)
	if len(ssh) > 0 {
		table["ssh"] = ssh
	}
	highlight := highlightTable(cfg.Highlight)
	if len(highlight) > 0 {
		table["highlight"] = highlight
	}
	return table
}

func inventoryHostTable(cfg InventoryHostConfig) map[string]any {
	table := make(map[string]any)
	addString(table, "group", cfg.Group)
	if len(cfg.Aliases) > 0 {
		table["aliases"] = cfg.Aliases
	}
	if cfg.Port > 0 {
		table["port"] = cfg.Port
	}
	if cfg.AuthDisabled {
		table["auth_disabled"] = cfg.AuthDisabled
	}
	auth := authTable(cfg.Auth)
	if len(auth) > 0 {
		table["auth"] = auth
	}
	ssh := sshHostTable(cfg.SSH)
	if len(ssh) > 0 {
		table["ssh"] = ssh
	}
	highlight := highlightTable(cfg.Highlight)
	if len(highlight) > 0 {
		table["highlight"] = highlight
	}
	return table
}

func highlightTable(cfg HighlightConfig) map[string]any {
	table := make(map[string]any)
	if cfg.Enabled != nil {
		table["enabled"] = *cfg.Enabled
	}
	addString(table, "profile", cfg.Profile)
	return table
}

func authTable(cfg InventoryAuthConfig) map[string]any {
	cfg.Normalize()
	table := make(map[string]any)
	addString(table, "credential_provider", cfg.CredentialProvider)
	addString(table, "password_ref", cfg.PasswordRef)
	addString(table, "username", cfg.Username)
	addString(table, "username_ref", cfg.UsernameRef)
	addString(table, "mode", cfg.Mode)
	return table
}

func inventoryProviderTable(cfg InventoryProviderConfig) map[string]any {
	cfg.syncAliasFields()
	table := make(map[string]any)
	addString(table, "type", cfg.Type)
	auth := authTable(cfg.Auth)
	if len(auth) > 0 {
		table["auth"] = auth
	}
	detail := inventoryProviderDetailTable(cfg.Config)
	if len(detail) > 0 {
		table["config"] = detail
	}
	if len(cfg.Groups) > 0 {
		groups := make(map[string]any)
		for _, name := range sortedGroups(cfg.Groups) {
			groups[name] = groupTable(cfg.Groups[name])
		}
		table["groups"] = groups
	}
	if len(cfg.Hosts) > 0 {
		hosts := make(map[string]any)
		for _, name := range sortedInventoryHosts(cfg.Hosts) {
			hosts[name] = inventoryHostTable(cfg.Hosts[name])
		}
		table["hosts"] = hosts
	}
	return table
}

func inventoryProviderDetailTable(cfg InventoryProviderDetailConfig) map[string]any {
	table := make(map[string]any)
	addString(table, "base_url", cfg.BaseURL)
	addString(table, "url_env", cfg.URLEnv)
	addString(table, "token_env", cfg.TokenEnv)
	addString(table, "env_file", cfg.EnvFile)
	addString(table, "jump_host", cfg.JumpHost)
	if cfg.Sudo {
		table["sudo"] = cfg.Sudo
	}
	if cfg.StrictHostKeyChecking {
		table["strict_host_key_checking"] = cfg.StrictHostKeyChecking
	}
	if !cfg.SSHDefaults.IsZero() {
		if len(cfg.SSHDefaults.Options) > 0 {
			table["ssh_defaults"] = cfg.SSHDefaults.Options
		} else {
			table["ssh_defaults"] = cfg.SSHDefaults.Mode
		}
	}
	return table
}

func inventoryMatchTable(matchConfig InventoryMatch) map[string]any {
	table := make(map[string]any)
	keys := make([]string, 0, len(matchConfig))
	for key := range matchConfig {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		table[key] = matchConfig[key]
	}
	return table
}

func loggingTable(cfg LoggingConfig) map[string]any {
	table := make(map[string]any)
	audit := make(map[string]any)
	if cfg.Audit.Enabled {
		audit["enabled"] = cfg.Audit.Enabled
	}
	addString(audit, "max_size", cfg.Audit.MaxSize)
	if len(audit) > 0 {
		table["audit"] = audit
	}

	export := loggingExportTable(cfg.Export)
	if len(export) > 0 {
		table["export"] = export
	}

	session := make(map[string]any)
	if cfg.Session.Enabled != nil {
		session["enabled"] = *cfg.Session.Enabled
	}
	if cfg.Session.AppendMode != nil {
		session["append_mode"] = *cfg.Session.AppendMode
	}
	addString(session, "asciinema_server_url", cfg.Session.AsciinemaServer)
	addString(session, "dir", cfg.Session.Dir)
	if len(cfg.Session.ExcludeHosts) > 0 {
		session["exclude_hosts"] = cfg.Session.ExcludeHosts
	}
	if cfg.Session.IdleTimeLimit > 0 {
		session["idle_time_limit"] = cfg.Session.IdleTimeLimit
	}
	addString(session, "idle_time_limit_mode", cfg.Session.IdleTimeLimitMode)
	if len(cfg.Session.IncludeHosts) > 0 {
		session["include_hosts"] = cfg.Session.IncludeHosts
	}
	addString(session, "title_format", cfg.Session.TitleFormat)
	if cfg.Session.AutoExportTxt {
		session["auto_export_txt"] = cfg.Session.AutoExportTxt
	}
	archive := sessionArchiveTable(cfg.Session.Archive, DefaultConfig().Logging.Session.Archive)
	if len(archive) > 0 {
		session["archive"] = archive
	}
	if len(session) > 0 {
		table["session"] = session
	}
	return table
}

func loggingExportTable(cfg LoggingExportConfig) map[string]any {
	table := make(map[string]any)
	gif := make(map[string]any)
	addString(gif, "window_size", cfg.GIF.WindowSize)
	if cfg.GIF.FontSize > 0 {
		gif["font_size"] = cfg.GIF.FontSize
	}
	if len(gif) > 0 {
		table["gif"] = gif
	}
	return table
}

func sessionArchiveTable(cfg, defaults SessionArchiveConfig) map[string]any {
	table := make(map[string]any)
	if cfg.Dir != "" && cfg.Dir != defaults.Dir {
		table["dir"] = cfg.Dir
	}
	if cfg.MaxBundles != defaults.MaxBundles {
		table["max_bundles"] = cfg.MaxBundles
	}
	if cfg.MaxRunBytes != defaults.MaxRunBytes {
		table["max_run_bytes"] = cfg.MaxRunBytes
	}
	if cfg.MinAge.Duration() != defaults.MinAge.Duration() {
		table["min_age"] = cfg.MinAge.Duration().String()
	}
	if cfg.Timeout.Duration() != defaults.Timeout.Duration() {
		table["timeout"] = cfg.Timeout.Duration().String()
	}
	return table
}

func sshTable(cfg SSHConfig) map[string]any {
	table := make(map[string]any)
	connection := make(map[string]any)
	if cfg.Connection.IdleTimeout.Duration() > 0 {
		connection["idle_timeout"] = cfg.Connection.IdleTimeout.Duration().String()
	}
	if cfg.Connection.PasswordTimeout.Duration() > 0 {
		connection["password_timeout"] = cfg.Connection.PasswordTimeout.Duration().String()
	}
	if cfg.Connection.Timeout.Duration() > 0 {
		connection["timeout"] = cfg.Connection.Timeout.Duration().String()
	}
	if len(connection) > 0 {
		table["connection"] = connection
	}

	security := make(map[string]any)
	addString(security, "accept_once_mode", cfg.Security.AcceptOnceMode)
	if cfg.Security.CompatPersistProbes {
		security["compat_persist_probes"] = cfg.Security.CompatPersistProbes
	}
	addString(security, "host_key_policy", cfg.Security.HostKeyPolicy)
	if len(security) > 0 {
		table["security"] = security
	}
	defaults := sshHostTable(cfg.Defaults)
	if len(defaults) > 0 {
		table["defaults"] = defaults
	}
	return table
}

func sshHostTable(cfg SSHHostConfig) map[string]any {
	table := make(map[string]any)
	compatibility := make(map[string]any)
	addString(compatibility, "kex", cfg.Compatibility.Kex)
	addString(compatibility, "mac", cfg.Compatibility.MAC)
	addString(compatibility, "host_key", cfg.Compatibility.HostKey)
	addString(compatibility, "public_key", cfg.Compatibility.PublicKey)
	if len(compatibility) > 0 {
		table["compatibility"] = compatibility
	}
	if options := cfg.Options; len(options) > 0 {
		table["options"] = options
	}
	return table
}

func addString(table map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		table[key] = value
	}
}

func marshalConfigTable(table map[string]any) (string, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(table); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func sortedCredentialProviders(in map[string]CredentialProviderConfig) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedGroups(in map[string]GroupConfig) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedInventoryHosts(in map[string]InventoryHostConfig) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedInventoryProviders(in map[string]InventoryProviderConfig) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(in map[string]any) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
