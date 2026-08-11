// Package connect extracts shared host lookup and credential resolution so
// interactive SSH and remote command execution can share the same path.
package connect

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

const (
	CredSourceHost  = "host"
	CredSourceGroup = "group"
)

type ResolvedCredential struct {
	Username         string
	Password         *secret.Secret
	PasswordResolver func(context.Context) (*secret.Secret, error)
	Source           string
	Provider         string
	Ref              string
}

// ResolvedHost holds the result of host resolution: everything needed to
// connect or run a remote command.
type ResolvedHost struct {
	Query       string
	Canonical   string
	Hostname    string
	Port        int
	Username    string
	AuthMode    string
	Provider    string
	Group       string
	Aliases     []string
	SSH         config.SSHHostConfig
	Highlight   config.HighlightConfig
	IncludeFile string
	HostEntry   *sshconfig.HostEntry
	Credential  *ResolvedCredential
	Proxy       *ResolvedProxy
	Config      *config.Config
}

type ResolvedProxy struct {
	Canonical  string
	Hostname   string
	Port       int
	Username   string
	AuthMode   string
	Credential *ResolvedCredential
}

// ResolveHostForConnect performs host lookup, include-file discovery, and
// credential resolution. This is the shared path used by both interactive
// connect and remote command execution.
// If cfg is provided, it is used instead of loading the default config.
func ResolveHostForConnect(query, explicitUser string, cfg ...*config.Config) (*ResolvedHost, error) {
	c, err := loadConnectConfig(cfg...)
	if err != nil {
		return nil, err
	}

	hostname, explicitUser := splitConnectQuery(query, explicitUser)
	catalog, err := BuildHostCatalog(c)
	if err != nil {
		return nil, err
	}
	hostData, managed := catalog.Find(hostname)
	if !managed {
		return nil, &HostNotFoundError{Hostname: hostname}
	}
	return resolveCatalogHostForConnect(query, explicitUser, c, hostData)
}

// ResolveSmartHostForConnect performs smart lookup and full host resolution in
// one config/catalog pass. It preserves smart-connect matching while avoiding a
// separate ResolveHostname call before connection setup.
func ResolveSmartHostForConnect(query, explicitUser string, cfg ...*config.Config) (*ResolvedHost, error) {
	c, err := loadConnectConfig(cfg...)
	if err != nil {
		return nil, err
	}

	hostname, explicitUser := splitConnectQuery(query, explicitUser)
	catalog, err := BuildHostCatalog(c)
	if err != nil {
		return nil, err
	}
	canonical, err := resolveHostnameFromCatalog(hostname, catalog, selectHostFunc)
	if err != nil {
		return nil, err
	}
	hostData, managed := catalog.Find(canonical)
	if !managed {
		return nil, &HostNotFoundError{Hostname: canonical}
	}
	return resolveCatalogHostForConnect(query, explicitUser, c, hostData)
}

func loadConnectConfig(cfg ...*config.Config) (*config.Config, error) {
	if len(cfg) > 0 && cfg[0] != nil {
		return cfg[0], nil
	}
	configTimer := connector.StartTiming(connector.TimingConfigLoad)
	c, err := config.LoadDefault()
	configTimer.Emit()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func splitConnectQuery(query, explicitUser string) (string, string) {
	hostname := query
	if idx := strings.LastIndex(hostname, "@"); idx != -1 {
		if explicitUser == "" {
			explicitUser = hostname[:idx]
		}
		hostname = hostname[idx+1:]
	}
	return hostname, explicitUser
}

func resolveCatalogHostForConnect(query, explicitUser string, c *config.Config, hostData *ResolvedHostData) (*ResolvedHost, error) {
	registryTimer := connector.StartTiming(connector.TimingCredentialRegistry)
	registry, err := newConnectCredentialRegistry(c)
	registryTimer.Emit()
	if err != nil {
		slog.Debug("credential registry not available", "err", err)
		registry = nil
	}

	auth, cred, username := resolveCatalogAuthCredential(c, registry, hostData, explicitUser)

	resolved := &ResolvedHost{
		Query:      query,
		Canonical:  hostData.Canonical,
		Hostname:   hostData.Hostname,
		Port:       hostData.Port,
		Username:   username,
		AuthMode:   auth.AuthMode,
		Provider:   hostData.Provider,
		Group:      hostData.Group,
		Aliases:    hostData.Aliases,
		SSH:        config.MergeSSH(config.SSHHostConfig{}, hostData.SSH),
		Highlight:  hostData.Highlight,
		Credential: cred,
		Config:     c,
	}
	if hostData.ManagedProxy != nil {
		proxyAuth, proxyCredential, proxyUsername := resolveCatalogAuthCredential(c, registry, hostData.ManagedProxy, hostData.ManagedProxy.Username)
		resolved.Proxy = &ResolvedProxy{
			Canonical:  hostData.ManagedProxy.Canonical,
			Hostname:   hostData.ManagedProxy.Hostname,
			Port:       hostData.ManagedProxy.Port,
			Username:   proxyUsername,
			AuthMode:   proxyAuth.AuthMode,
			Credential: proxyCredential,
		}
		proxyData := *hostData.ManagedProxy
		proxyData.Username = proxyUsername
		if command := formatManagedProxyCommand(&proxyData, resolved.Hostname, resolved.Port); command != "" {
			deleteSSHOption(resolved.SSH.Options, "ProxyJump")
			if resolved.SSH.Options == nil {
				resolved.SSH.Options = make(config.SSHOptions)
			}
			resolved.SSH.Options["ProxyCommand"] = config.NewSSHOptionString(command)
		}
	}
	return resolved, nil
}

func deleteSSHOption(options config.SSHOptions, name string) {
	for key := range options {
		if strings.EqualFold(key, name) {
			delete(options, key)
		}
	}
}

func resolveCatalogAuthCredential(c *config.Config, registry providerRegistry, host *ResolvedHostData, explicitUser string) (config.InventoryAuthResolution, *ResolvedCredential, string) {
	if c == nil || host == nil {
		return config.InventoryAuthResolution{}, nil, strings.TrimSpace(explicitUser)
	}
	authTimer := connector.StartTiming(connector.TimingAuthResolve)
	auth := c.ResolveInventoryAuth(config.InventoryAuthContext{
		Host:     host.Canonical,
		Provider: host.Provider,
		Group:    catalogGroupID(host.Provider, host.Group),
	})
	authTimer.Emit()

	credentialUser := auth.Username
	var cred *ResolvedCredential
	if registry != nil {
		lookupTimer := connector.StartTiming(connector.TimingCredentialLookup)
		var err error
		cred, err = resolveInventoryCredential(registry, auth, explicitUser)
		lookupTimer.Emit()
		if err != nil {
			slog.Warn("credential resolution failed", "host", host.Hostname, "err", err)
		} else if cred != nil && credentialUser == "" {
			credentialUser = cred.Username
		}
	}
	username := selectConnectionUsername(true, explicitUser, "", "", credentialUser, "")
	return auth, cred, username
}

// ResolveLiteralHostForConnect resolves a literal destination without fuzzy
// matching or host-add fallback. Exact inventory matches still use managed
// metadata and credentials; unmanaged literals fall back to SSH defaults.
func ResolveLiteralHostForConnect(query, explicitUser string, cfg ...*config.Config) (*ResolvedHost, error) {
	var c *config.Config
	if len(cfg) > 0 && cfg[0] != nil {
		c = cfg[0]
	} else {
		var err error
		configTimer := connector.StartTiming(connector.TimingConfigLoad)
		c, err = config.LoadDefault()
		configTimer.Emit()
		if err != nil {
			return nil, err
		}
	}

	hostname := strings.TrimSpace(query)
	if idx := strings.LastIndex(hostname, "@"); idx != -1 {
		if explicitUser == "" {
			explicitUser = hostname[:idx]
		}
		hostname = hostname[idx+1:]
	}
	if hostname == "" {
		return nil, fmt.Errorf("literal target is required")
	}

	catalog, err := BuildHostCatalog(c)
	if err != nil {
		return nil, err
	}
	if _, managed := catalog.Find(hostname); managed {
		return ResolveHostForConnect(query, explicitUser, c)
	}

	return &ResolvedHost{
		Query:     query,
		Canonical: hostname,
		Hostname:  hostname,
		Port:      22,
		Username:  explicitUser,
		SSH:       config.MergeSSH(config.SSHHostConfig{}, c.SSH.Defaults),
		Highlight: config.MergeHighlight(config.HighlightConfig{}, c.Highlight),
		Config:    c,
	}, nil
}

type providerRegistry interface {
	Provider(name string) credential.Provider
}

var newConnectCredentialRegistry = func(cfg *config.Config) (providerRegistry, error) {
	return credential.NewRegistry(cfg)
}

func resolveBoundCredential(cfg *config.Config, registry providerRegistry, hostname, group, username string) (*ResolvedCredential, error) {
	if cfg == nil || registry == nil {
		return nil, nil
	}
	if ref, ok := cfg.Inventory.Host[hostname]; ok {
		if ref.AuthDisabled {
			return nil, nil
		}
		if ref.Auth.IsSet() {
			auth := ref.Auth
			auth.Normalize()
			provider := registry.Provider(auth.CredentialProvider)
			return resolveScopedCredential(provider, scopeHost, hostname, username)
		}
	}
	if group == "" {
		return nil, nil
	}
	providerName, _, err := config.ParseInventoryGroupID(group)
	if err != nil {
		return nil, nil
	}
	auth := cfg.ResolveInventoryAuth(config.InventoryAuthContext{Host: hostname, Provider: providerName, Group: group})
	return resolveInventoryCredential(registry, auth, username)
}

type credentialScope string

const (
	scopeHost  credentialScope = "host"
	scopeGroup credentialScope = "group"
)

func resolveScopedCredential(provider credential.Provider, scope credentialScope, name, username string) (*ResolvedCredential, error) {
	if provider == nil {
		return nil, nil
	}
	var (
		record *credential.Record
		err    error
		source string
	)
	switch scope {
	case scopeHost:
		record, err = provider.GetHost(name)
		source = CredSourceHost
	case scopeGroup:
		record, err = provider.GetGroup(name)
		source = CredSourceGroup
	}
	if err != nil || record == nil {
		return nil, err
	}
	if !credentialMatchesUser(record, username) {
		return nil, nil
	}
	recordUsername := record.Username
	if recordUsername == "" {
		recordUsername = username
	}
	return &ResolvedCredential{
		Username: recordUsername,
		Password: record.Secret,
		Source:   source,
		Ref:      record.Ref,
	}, nil
}

func credentialMatchesUser(record *credential.Record, username string) bool {
	if record == nil {
		return false
	}
	return username == "" || record.Username == "" || record.Username == username
}

func selectConnectionUsername(managed bool, explicitUser, hostUser, groupUser, credentialUser, defaultUser string) string {
	users := []string{explicitUser}
	if !managed {
		users = append(users, hostUser)
	}
	users = append(users, groupUser, credentialUser, defaultUser)
	for _, user := range users {
		if strings.TrimSpace(user) != "" {
			return strings.TrimSpace(user)
		}
	}
	return ""
}

func resolveInventoryCredential(registry providerRegistry, auth config.InventoryAuthResolution, explicitUser string) (*ResolvedCredential, error) {
	if registry == nil || auth.Disabled || auth.CredentialProvider == "" || auth.PasswordRef == "" {
		return nil, nil
	}
	if explicitUser != "" {
		authUser := strings.TrimSpace(auth.Username)
		if authUser != "" && explicitUser != authUser {
			return nil, nil
		}
	}
	provider := registry.Provider(auth.CredentialProvider)
	if provider == nil {
		return nil, nil
	}
	ref := config.CredentialRefConfig{
		Provider:    auth.CredentialProvider,
		Ref:         auth.PasswordRef,
		Username:    auth.Username,
		UsernameRef: auth.UsernameRef,
	}
	source := auth.Source
	if strings.HasPrefix(source, "group ") {
		source = CredSourceGroup
	} else if strings.HasPrefix(source, "host ") {
		source = CredSourceHost
	}

	if canDeferCredentialLookup(ref, explicitUser) {
		username := strings.TrimSpace(ref.Username)
		if username == "" {
			username = strings.TrimSpace(explicitUser)
			ref.Username = username
		}
		return &ResolvedCredential{
			Username: username,
			PasswordResolver: func(ctx context.Context) (*secret.Secret, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
				record, err := provider.GetRef(ref)
				if err != nil || record == nil {
					return nil, err
				}
				if !credentialMatchesUser(record, username) {
					return nil, nil
				}
				return record.Secret, nil
			},
			Source:   source,
			Provider: auth.CredentialProvider,
			Ref:      ref.Ref,
		}, nil
	}

	record, err := provider.GetRef(ref)
	if err != nil || record == nil {
		return nil, err
	}
	if explicitUser != "" && strings.TrimSpace(record.Username) != "" && explicitUser != strings.TrimSpace(record.Username) {
		return nil, nil
	}
	username := strings.TrimSpace(auth.Username)
	if username == "" {
		username = strings.TrimSpace(record.Username)
	}
	if username == "" {
		username = strings.TrimSpace(explicitUser)
	}
	return &ResolvedCredential{Username: username, Password: record.Secret, Source: source, Provider: auth.CredentialProvider, Ref: ref.Ref}, nil
}

func canDeferCredentialLookup(ref config.CredentialRefConfig, explicitUser string) bool {
	if strings.TrimSpace(ref.UsernameRef) != "" {
		return false
	}
	if strings.TrimSpace(ref.Username) != "" {
		return true
	}
	return strings.TrimSpace(explicitUser) != "" && isDirectSecretRef(ref.Ref)
}

func isDirectSecretRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	return strings.HasPrefix(ref, "op://") && !strings.HasSuffix(ref, "/")
}
