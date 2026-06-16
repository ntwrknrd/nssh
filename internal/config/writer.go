package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	if !isInventoryScopedTable(table) {
		return saveRootTable(path, table)
	}
	stripLegacyIdentityKeys(table)
	text, err := marshalInventoryScopedTable(table)
	if err != nil {
		return err
	}
	return writeConfigText(path, text)
}

func isInventoryScopedTable(table map[string]any) bool {
	if tablePathDefined(table, "provider") && !tablePathDefined(table, "inventory", "provider") {
		return true
	}
	return tablePathDefined(table, "host") && !tablePathDefined(table, "inventory", "host")
}

func marshalInventoryScopedTable(table map[string]any) (string, error) {
	var b bytes.Buffer
	writeRootScalars(&b, table)
	hosts, _ := asMap(table["host"])
	for _, name := range sortedMapKeys(hosts) {
		path := "host." + name
		host, _ := asMap(hosts[name])
		writeTableHeader(&b, path)
		writeOptionIfPresent(&b, path, host, "auth_disabled")
		writeInlineMapIfPresent(&b, path, host, "auth")
	}
	providers, _ := asMap(table["provider"])
	for _, name := range sortedMapKeys(providers) {
		path := "provider." + name
		provider, _ := asMap(providers[name])
		writeTableHeader(&b, path)
		writeOptionIfPresent(&b, path, provider, "type")
		writeInlineMapIfPresent(&b, path, provider, "auth")
		writeInlineMapIfPresent(&b, path, provider, "config")
		groups, _ := asMap(provider["group"])
		for _, groupName := range sortedMapKeys(groups) {
			groupPath := path + ".group." + groupName
			group, _ := asMap(groups[groupName])
			writeTableHeader(&b, groupPath)
			writeOptionIfPresent(&b, groupPath, group, "domain_suffix")
			writeInlineMapIfPresent(&b, groupPath, group, "auth")
			if match, ok := asMap(group["match"]); ok && len(match) > 0 {
				matchPath := groupPath + ".match"
				writeTableHeader(&b, matchPath)
				for _, key := range sortedMapKeys(match) {
					writeOption(&b, matchPath, key, match[key])
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
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
	var b bytes.Buffer
	hostPath := strings.Join(inventoryHostPath(root, host), ".")
	hostTable := inventoryHostTable(hostCfg)
	writeTableHeader(&b, hostPath)
	writeRawOptionIfPresent(&b, hostTable, "auth_disabled")
	writeRawOptionIfPresent(&b, hostTable, "auth")
	return strings.TrimRight(b.String(), "\n") + "\n", nil
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
	var b bytes.Buffer
	groupPath := strings.Join(inventoryProviderGroupPath(root, providerName, groupName), ".")
	group := groupTable(groupCfg)
	writeTableHeader(&b, groupPath)
	writeRawOptionIfPresent(&b, group, "domain_suffix")
	writeRawOptionIfPresent(&b, group, "auth")
	if match, ok := asMap(group["match"]); ok && len(match) > 0 {
		matchPath := groupPath + ".match"
		writeTableHeader(&b, matchPath)
		for _, key := range sortedMapKeys(match) {
			writeRawOption(&b, key, match[key])
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
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
	if len(agent) > 0 {
		table["agent"] = agent
	}

	if len(cfg.Credentials) > 0 {
		credentials := make(map[string]any)
		for _, name := range sortedCredentialProviders(cfg.Credentials) {
			credentials[name] = credentialProviderTable(cfg.Credentials[name])
		}
		table["credentials"] = credentials
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

	return table
}

func credentialProviderTable(cfg CredentialProviderConfig) map[string]any {
	cfg.syncDetailFields()
	table := make(map[string]any)
	if cfg.Type != "" {
		table["type"] = cfg.Type
	}
	addString(table, "session", cfg.Session)
	addString(table, "account", cfg.Account)
	addString(table, "vault", cfg.Vault)
	addString(table, "command", cfg.Command)
	addString(table, "prefix", cfg.Prefix)
	return table
}

func providerDetailTable(cfg CredentialProviderDetailConfig) map[string]any {
	table := make(map[string]any)
	addString(table, "account", cfg.Account)
	addString(table, "vault", cfg.Vault)
	addString(table, "command", cfg.Command)
	addString(table, "prefix", cfg.Prefix)
	addString(table, "session", cfg.Session)
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
	return table
}

func inventoryHostTable(cfg InventoryHostConfig) map[string]any {
	table := make(map[string]any)
	addString(table, "group", cfg.Group)
	addString(table, "hostname", cfg.Hostname)
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
	return table
}

func authTable(cfg InventoryAuthConfig) map[string]any {
	cfg.Normalize()
	table := make(map[string]any)
	addString(table, "credential_provider", cfg.CredentialProvider)
	addString(table, "password_ref", cfg.PasswordRef)
	addString(table, "username", cfg.Username)
	addString(table, "username_ref", cfg.UsernameRef)
	addString(table, "mode", cfg.AuthMode)
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
	addString(session, "window_size", cfg.Session.WindowSize)
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

func sessionArchiveTable(cfg, defaults SessionArchiveConfig) map[string]any {
	table := make(map[string]any)
	if cfg.Enabled != defaults.Enabled {
		table["enabled"] = cfg.Enabled
	}
	if cfg.Dir != "" && cfg.Dir != defaults.Dir {
		table["dir"] = cfg.Dir
	}
	if cfg.Jitter.Duration() != defaults.Jitter.Duration() {
		table["jitter"] = cfg.Jitter.Duration().String()
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
	addString(table, "proxy_jump", cfg.ProxyJump)
	addString(table, "proxy_command", cfg.ProxyCommand)
	if cfg.IdentitiesOnly != nil {
		table["identities_only"] = *cfg.IdentitiesOnly
	}
	if cfg.IdentityAgent.Path != "" {
		table["identity_agent"] = map[string]any{"path": cfg.IdentityAgent.Path}
	}
	if len(cfg.IdentityFiles) > 0 {
		table["identity_files"] = cfg.IdentityFiles
	}
	if len(cfg.CertificateFiles) > 0 {
		table["certificate_files"] = cfg.CertificateFiles
	}
	if cfg.ForwardAgent != nil {
		table["forward_agent"] = *cfg.ForwardAgent
	}
	if len(cfg.LocalForwards) > 0 {
		table["local_forwards"] = cfg.LocalForwards
	}
	if len(cfg.RemoteForwards) > 0 {
		table["remote_forwards"] = cfg.RemoteForwards
	}
	if len(cfg.SetEnv) > 0 {
		table["set_env"] = cfg.SetEnv
	}
	addString(table, "remote_command", cfg.RemoteCommand)
	if cfg.ServerAliveInterval.Duration() > 0 {
		table["server_alive_interval"] = cfg.ServerAliveInterval.Duration().String()
	}
	if cfg.ServerAliveCountMax > 0 {
		table["server_alive_count_max"] = cfg.ServerAliveCountMax
	}
	if cfg.ConnectionTimeout.Duration() > 0 {
		table["connection_timeout"] = cfg.ConnectionTimeout.Duration().String()
	}
	addString(table, "control_master", cfg.ControlMaster)
	if cfg.ControlPersist.Duration() > 0 {
		table["control_persist"] = cfg.ControlPersist.Duration().String()
	}
	addString(table, "control_path", cfg.ControlPath)
	if len(cfg.Ciphers) > 0 {
		table["ciphers"] = cfg.Ciphers
	}
	if len(cfg.MACs) > 0 {
		table["macs"] = cfg.MACs
	}
	if len(cfg.KexAlgorithms) > 0 {
		table["kex_algorithms"] = cfg.KexAlgorithms
	}
	if len(cfg.HostKeyAlgorithms) > 0 {
		table["host_key_algorithms"] = cfg.HostKeyAlgorithms
	}
	if len(cfg.PubkeyAcceptedAlgorithms) > 0 {
		table["pubkey_accepted_algorithms"] = cfg.PubkeyAcceptedAlgorithms
	}
	if len(cfg.Compat) > 0 {
		table["compat"] = cfg.Compat
	}
	if len(cfg.Options) > 0 {
		table["options"] = cfg.Options
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

func writeRootScalars(b *bytes.Buffer, table map[string]any) {
	if include, ok := table["include"]; ok {
		writeOption(b, "", "include", include)
		b.WriteString("\n")
	}
}

func writeKnownSections(b *bytes.Buffer, table map[string]any) {
	writeSimpleTable(b, table, "agent")
	writeCredential(b, table)
	writeInventory(b, table)
	writeLogging(b, table)
	writeSSH(b, table)
}

func writeSimpleTable(b *bytes.Buffer, parent map[string]any, name string) {
	table, ok := asMap(parent[name])
	if !ok || len(table) == 0 {
		return
	}
	writeTableHeader(b, name)
	for _, key := range orderedKeys(table, optionOrder(name)) {
		if isNestedValue(table[key]) {
			continue
		}
		writeOption(b, name, key, table[key])
	}
}

func writeCredential(b *bytes.Buffer, root map[string]any) {
	credential, ok := asMap(root["credential"])
	if !ok || len(credential) == 0 {
		return
	}
	writeTableHeader(b, "credential")
	for _, key := range []string{"include"} {
		if value, ok := credential[key]; ok {
			writeOption(b, "credential", key, value)
		}
	}
	providers, _ := asMap(credential["provider"])
	for _, name := range sortedMapKeys(providers) {
		path := "credential.provider." + name
		provider, _ := asMap(providers[name])
		writeTableHeader(b, path)
		writeOptionIfPresent(b, path, provider, "type")
		if cfg, ok := asMap(provider["config"]); ok && len(cfg) > 0 {
			cfgPath := path + ".config"
			writeTableHeader(b, cfgPath)
			for _, key := range orderedKeys(cfg, optionOrder(cfgPath)) {
				writeOption(b, cfgPath, key, cfg[key])
			}
		}
	}
}

func writeInventory(b *bytes.Buffer, root map[string]any) {
	inventory, ok := asMap(root["inventory"])
	if !ok || len(inventory) == 0 {
		return
	}
	writeTableHeader(b, "inventory")
	for _, key := range []string{"include"} {
		if value, ok := inventory[key]; ok {
			writeOption(b, "inventory", key, value)
		}
	}
	writeInlineMapIfPresent(b, "inventory", inventory, "auth")
	hosts, _ := asMap(inventory["host"])
	for _, name := range sortedMapKeys(hosts) {
		path := "inventory.host." + name
		host, _ := asMap(hosts[name])
		writeTableHeader(b, path)
		writeOptionIfPresent(b, path, host, "auth_disabled")
		writeInlineMapIfPresent(b, path, host, "auth")
	}
	providers, _ := asMap(inventory["provider"])
	for _, name := range sortedMapKeys(providers) {
		path := "inventory.provider." + name
		provider, _ := asMap(providers[name])
		writeTableHeader(b, path)
		writeOptionIfPresent(b, path, provider, "type")
		writeInlineMapIfPresent(b, path, provider, "auth")
		writeInlineMapIfPresent(b, path, provider, "config")
		groups, _ := asMap(provider["group"])
		for _, groupName := range sortedMapKeys(groups) {
			groupPath := path + ".group." + groupName
			group, _ := asMap(groups[groupName])
			writeTableHeader(b, groupPath)
			writeOptionIfPresent(b, groupPath, group, "domain_suffix")
			writeInlineMapIfPresent(b, groupPath, group, "auth")
			if match, ok := asMap(group["match"]); ok && len(match) > 0 {
				matchPath := groupPath + ".match"
				writeTableHeader(b, matchPath)
				for _, key := range sortedMapKeys(match) {
					writeOption(b, matchPath, key, match[key])
				}
			}
		}
	}
}

func writeLogging(b *bytes.Buffer, root map[string]any) {
	logging, ok := asMap(root["logging"])
	if !ok {
		return
	}
	for _, section := range []string{"audit", "session"} {
		table, ok := asMap(logging[section])
		if !ok || len(table) == 0 {
			continue
		}
		path := "logging." + section
		writeTableHeader(b, path)
		for _, key := range orderedKeys(table, optionOrder(path)) {
			if key == "archive" {
				continue
			}
			writeOption(b, path, key, table[key])
		}
		if archive, ok := asMap(table["archive"]); ok && len(archive) > 0 {
			archivePath := path + ".archive"
			writeTableHeader(b, archivePath)
			for _, key := range orderedKeys(archive, optionOrder(archivePath)) {
				writeOption(b, archivePath, key, archive[key])
			}
		}
	}
}

func writeSSH(b *bytes.Buffer, root map[string]any) {
	ssh, ok := asMap(root["ssh"])
	if !ok {
		return
	}
	for _, section := range []string{"connection", "security"} {
		table, ok := asMap(ssh[section])
		if !ok || len(table) == 0 {
			continue
		}
		path := "ssh." + section
		writeTableHeader(b, path)
		for _, key := range orderedKeys(table, optionOrder(path)) {
			writeOption(b, path, key, table[key])
		}
	}
}

func writeTableHeader(b *bytes.Buffer, path string) {
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "[%s]\n", path)
}

func writeArrayTableHeader(b *bytes.Buffer, path string) {
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "[[%s]]\n", path)
}

func writeOptionIfPresent(b *bytes.Buffer, path string, table map[string]any, key string) {
	if value, ok := table[key]; ok {
		writeOption(b, path, key, value)
	}
}

func writeInlineMapIfPresent(b *bytes.Buffer, path string, table map[string]any, key string) {
	value, ok := asMap(table[key])
	if !ok || len(value) == 0 {
		return
	}
	writeOption(b, path, key, value)
}

func writeRawOptionIfPresent(b *bytes.Buffer, table map[string]any, key string) {
	if value, ok := table[key]; ok {
		writeRawOption(b, key, value)
	}
}

func writeRawOption(b *bytes.Buffer, key string, value any) {
	fmt.Fprintf(b, "%s = %s\n", key, formatTOMLValue(value))
}

func writeOption(b *bytes.Buffer, path, key string, value any) {
	for _, line := range optionComment(path, key) {
		fmt.Fprintf(b, "# %s\n", line)
	}
	fmt.Fprintf(b, "%s = %s\n\n", key, formatTOMLValue(value))
}

func optionComment(path, key string) []string {
	comments := map[string][]string{
		"include":                                {"Import shared or modular config before applying this file's local overrides.", `Common value: ["conf.d/base.yaml"] or ["inventory/*.yaml"].`},
		"agent.auto_start":                       {"Start the nssh runtime agent on first provider-session request.", "Common value: true."},
		"agent.idle_timeout":                     {"How long the nssh runtime agent can sit idle before exiting.", `Common value: "1h" for default use, "4h" for a longer work session.`},
		"agent.activity_increment":               {"How much activity extends the idle deadline, capped by idle_timeout.", `Common value: "15m" or "30m".`},
		"agent.max_lifetime":                     {"Hard maximum runtime for the agent even if it remains active.", `Common value: "8h" for a workday, "24h" for default behavior.`},
		"credential.include":                     {"Import credential provider definitions under [credential].", `Common value: ["credentials/*.yaml"].`},
		"credential.provider.type":               {"Credential backend type.", `Acceptable values: "pass", "1password", "bitwarden".`},
		"credential.provider.config.account":     {"1Password account shorthand passed to op when needed.", `Common value: "" or your 1Password account name.`},
		"credential.provider.config.vault":       {"1Password vault containing SSH credential items.", `Common value: "Network" or "TeamVault".`},
		"credential.provider.config.command":     {"Credential CLI command for pass-compatible providers.", `Common value: "pass".`},
		"credential.provider.config.prefix":      {"Password-store path prefix for nssh-managed entries.", `Common value: "nssh".`},
		"credential.provider.config.session":     {"Provider session handling mode.", `Acceptable values: "external", "agent", "none".`},
		"inventory.include":                      {"Import inventory providers and provider-owned groups under [inventory].", `Common value: ["inventory/*.yaml"].`},
		"inventory.provider.group.domain_suffix": {"Legacy group domain suffix metadata.", `Prefer inventory.provider.<provider>.group.<group>.match.domain_suffix for group selection.`},
		"inventory.auth":                         {"Default nssh-owned SSH identity and auth routing.", `Common keys: username, username_ref, credential_provider, password_ref, auth_mode.`, "Use username_ref when treating the SSH username as sensitive; it costs an extra provider call, so time to first prompt is slower."},
		"inventory.provider.group.auth":          {"Provider group nssh-owned SSH identity and auth routing.", `Common keys: username, username_ref, credential_provider, password_ref, auth_mode.`, "Use username_ref when treating the SSH username as sensitive; it costs an extra provider call, so time to first prompt is slower."},
		"inventory.provider.group.match":         {"Select provider objects or local hosts into this inventory group.", "Common values: domain_suffix, manufacturer, tenant."},
		"inventory.host.auth_disabled":           {"Disable inherited stored credentials for this host.", "Common value: true for public-key-only or manually prompted hosts."},
		"inventory.host.auth":                    {"Host nssh-owned SSH identity and auth routing override.", "Common value: provider plus a stable provider item reference."},
		"inventory.provider.type":                {"Inventory provider type.", `Acceptable values: "local", "netbox", "containerlab".`},
		"inventory.provider.config":              {"Inventory provider connection settings.", "Common value: environment-backed URL/token so secrets are not stored in config."},
		"logging.audit.enabled":                  {"Enable security event logging.", "Common value: true."},
		"logging.audit.max_size":                 {"Maximum audit log size before rotation.", `Common value: "10MB".`},
		"logging.session.enabled":                {"Record SSH sessions automatically.", "Common value: true for audit-heavy workflows, false to disable."},
		"logging.session.append_mode":            {"Append sessions to a daily cast file instead of separate files.", "Common value: true."},
		"logging.session.asciinema_server_url":   {"Custom asciinema upload server URL.", `Common value: "https://asciinema.org".`},
		"logging.session.dir":                    {"Directory for session recording files.", `Common value: "~/.local/state/nssh/casts".`},
		"logging.session.exclude_hosts":          {"Host patterns that should never be recorded.", `Common value: ["lab-*", "regex:.*-mgmt$"].`},
		"logging.session.idle_time_limit":        {"Cap long pauses in recordings, in seconds.", "Common value: 0 to disable."},
		"logging.session.idle_time_limit_mode":   {"When to apply idle time limiting.", `Acceptable values: "play", "record", "both".`},
		"logging.session.include_hosts":          {"Host patterns that should be recorded, taking precedence over excludes.", `Common value: ["prod-*"].`},
		"logging.session.title_format":           {"Recording title template.", `Common value: "nssh:{host}".`},
		"logging.session.window_size":            {"Fixed terminal size used for recordings.", `Common value: "145x30" or "100x30".`},
		"logging.session.auto_export_txt":        {"Export a plain-text copy of each recording after the session ends.", "Common value: true if recordings are searched often."},
		"logging.session.archive.dir":            {"Directory for monthly recording archives.", `Default value: "~/.local/state/nssh/archives".`},
		"logging.session.archive.enabled":        {"Enable automatic archival of old session recordings.", "Common value: false."},
		"logging.session.archive.jitter":         {"Randomize archive schedule timing.", `Common value: "30m".`},
		"logging.session.archive.max_bundles":    {"Maximum monthly archive bundles to retain.", "Common value: 12."},
		"logging.session.archive.max_run_bytes":  {"Maximum bytes to process per archive maintenance run.", "Common value: 0 for unlimited."},
		"logging.session.archive.min_age":        {"Minimum recording age before archival.", `Common value: "720h" for about 30 days.`},
		"ssh.connection.idle_timeout":            {"Disconnect SSH after inactivity.", `Common value: "0s" to disable.`},
		"ssh.connection.password_timeout":        {"How long to wait for an SSH password prompt.", `Common value: "10s".`},
		"ssh.connection.timeout":                 {"Overall SSH connection timeout.", `Common value: "30s".`},
		"ssh.security.accept_once_mode":          {"Accept-once host key behavior.", `Acceptable values: "pin", "accept-new".`},
		"ssh.security.compat_persist_probes":     {"Allow compatibility probes to write to real known_hosts.", "Common value: false unless using TOFU mode."},
		"ssh.security.host_key_policy":           {"Host key behavior preset. \"pin\" is stricter; \"tofu\" accepts first use.", `Common value: "pin" for strict mode, "tofu" for lab/internal gear.`},
	}
	keyPath := commentPath(path, key)
	if comment, ok := comments[keyPath]; ok {
		return comment
	}
	if strings.Contains(keyPath, ".provider.") && strings.Contains(keyPath, ".match.") {
		return comments["inventory.provider.group.match"]
	}
	return []string{"nssh configuration option.", "Use the documented type for this field."}
}

func commentPath(path, key string) string {
	full := key
	if path != "" {
		full = path + "." + key
	}
	parts := strings.Split(full, ".")
	if len(parts) >= 4 && parts[0] == "credential" && parts[1] == "provider" {
		parts = append([]string{"credential", "provider"}, parts[3:]...)
	}
	if len(parts) >= 4 && parts[0] == "inventory" && parts[1] == "host" {
		parts = append([]string{"inventory", "host"}, parts[3:]...)
	}
	if len(parts) >= 4 && parts[0] == "inventory" && parts[1] == "provider" {
		if len(parts) >= 6 && parts[3] == "group" {
			parts = append([]string{"inventory", "provider", "group"}, parts[5:]...)
		} else {
			parts = append([]string{"inventory", "provider"}, parts[3:]...)
		}
	}
	return strings.Join(parts, ".")
}

func formatTOMLValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case Duration:
		return strconv.Quote(time.Duration(typed).String())
	case []string:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, strconv.Quote(item))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, formatTOMLValue(item))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		keys := sortedMapKeys(typed)
		items := make([]string, 0, len(keys))
		for _, key := range keys {
			items = append(items, key+" = "+formatTOMLValue(typed[key]))
		}
		return "{ " + strings.Join(items, ", ") + " }"
	default:
		return strconv.Quote(fmt.Sprint(value))
	}
}

func orderedKeys(table map[string]any, preferred []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(table))
	for _, key := range preferred {
		if _, ok := table[key]; ok {
			out = append(out, key)
			seen[key] = true
		}
	}
	rest := make([]string, 0, len(table))
	for key := range table {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func optionOrder(path string) []string {
	switch path {
	case "agent":
		return []string{"auto_start", "idle_timeout", "activity_increment", "max_lifetime"}
	case "logging.audit":
		return []string{"enabled", "max_size"}
	case "logging.session":
		return []string{"enabled", "append_mode", "dir", "asciinema_server_url", "exclude_hosts", "include_hosts", "idle_time_limit", "idle_time_limit_mode", "title_format", "window_size", "auto_export_txt", "archive"}
	case "logging.session.archive":
		return []string{"enabled", "dir", "jitter", "max_bundles", "max_run_bytes", "min_age"}
	case "ssh.connection":
		return []string{"idle_timeout", "password_timeout", "timeout"}
	case "ssh.security":
		return []string{"accept_once_mode", "compat_persist_probes", "host_key_policy"}
	default:
		if strings.HasSuffix(path, ".config") {
			return []string{"account", "vault", "command", "prefix", "session", "base_url", "url_env", "token_env", "env_file", "jump_host", "sudo", "strict_host_key_checking"}
		}
		return nil
	}
}

func isNestedValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return false
	}
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
