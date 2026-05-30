// Package connect extracts shared host lookup and credential resolution so
// interactive SSH and remote command execution can share the same path.
package connect

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

const (
	CredSourceHost  = "host"
	CredSourceGroup = "group"
)

type ResolvedCredential struct {
	Username string
	Password *secret.Secret
	Source   string
}

// ResolvedHost holds the result of host resolution: everything needed to
// connect or run a remote command.
type ResolvedHost struct {
	Query       string
	Hostname    string
	Username    string
	IncludeFile string
	HostEntry   *sshconfig.HostEntry
	Credential  *ResolvedCredential
	Config      *config.Config
}

// ResolveHostForConnect performs host lookup, include-file discovery, and
// credential resolution. This is the shared path used by both interactive
// connect and remote command execution.
// If cfg is provided, it is used instead of loading the default config.
func ResolveHostForConnect(query, explicitUser string, cfg ...*config.Config) (*ResolvedHost, error) {
	var c *config.Config
	if len(cfg) > 0 && cfg[0] != nil {
		c = cfg[0]
	} else {
		var err error
		c, err = config.LoadDefault()
		if err != nil {
			return nil, err
		}
	}

	hostname := query
	// Strip user@ prefix if present
	if idx := strings.LastIndex(hostname, "@"); idx != -1 {
		if explicitUser == "" {
			explicitUser = hostname[:idx]
		}
		hostname = hostname[idx+1:]
	}

	// Look up host in SSH config
	var includeFile string
	var hostEntry *sshconfig.HostEntry
	parser := sshconfig.NewParser()
	if host, err := parser.FindHost(hostname); err == nil && host != nil {
		hostEntry = host
		includeFile = filepath.Base(host.SourceFile)
	}

	group := resolveGroup(hostEntry, c)

	// Resolve username: explicit > SSH config > inventory group default > nssh default
	username := explicitUser
	if username == "" && hostEntry != nil {
		username = hostEntry.User()
	}
	if username == "" && group != "" {
		username = strings.TrimSpace(c.Inventory.Group[group].DefaultUser)
	}
	if username == "" {
		username = c.Host.Defaults.DefaultUser
	}

	// Resolve credentials
	var cred *ResolvedCredential
	registry, err := credential.NewRegistry(c)
	if err != nil {
		slog.Debug("credential registry not available", "err", err)
	} else {
		cred, err = resolveBoundCredential(c, registry, hostname, group, username)
		if err != nil {
			slog.Warn("credential resolution failed", "err", err)
		}
	}

	// Update username from resolved credential
	if cred != nil && cred.Username != "" {
		username = cred.Username
	}

	return &ResolvedHost{
		Query:       query,
		Hostname:    hostname,
		Username:    username,
		IncludeFile: includeFile,
		HostEntry:   hostEntry,
		Credential:  cred,
		Config:      c,
	}, nil
}

type providerRegistry interface {
	Provider(name string) credential.Provider
	DefaultProviderName() string
}

func resolveBoundCredential(cfg *config.Config, registry providerRegistry, hostname, group, username string) (*ResolvedCredential, error) {
	if cfg == nil || registry == nil {
		return nil, nil
	}
	if ref, ok := cfg.Inventory.Host[hostname]; ok && ref.Auth.IsSet() {
		providerName := bindingProvider(ref.Auth.CredentialRef(), registry.DefaultProviderName())
		provider := registry.Provider(providerName)
		return resolveScopedCredential(provider, scopeHost, hostname, username)
	}
	if group == "" {
		return nil, nil
	}
	if ref, ok := cfg.Inventory.Group[group]; ok && ref.Auth.IsSet() {
		providerName := bindingProvider(ref.Auth.CredentialRef(), registry.DefaultProviderName())
		provider := registry.Provider(providerName)
		return resolveScopedCredential(provider, scopeGroup, group, username)
	}
	return nil, nil
}

type credentialScope string

const (
	scopeHost  credentialScope = "host"
	scopeGroup credentialScope = "group"
)

func bindingProvider(ref config.CredentialRefConfig, defaultProvider string) string {
	if strings.TrimSpace(ref.Provider) != "" {
		return ref.Provider
	}
	return defaultProvider
}

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
	}, nil
}

func credentialMatchesUser(record *credential.Record, username string) bool {
	if record == nil {
		return false
	}
	return username == "" || record.Username == "" || record.Username == username
}

func resolveGroup(hostEntry *sshconfig.HostEntry, cfg *config.Config) string {
	if hostEntry == nil {
		return ""
	}
	if idx, err := inventory.BuildProviderIndex(); err == nil {
		if info := idx[hostEntry.Host]; info != nil {
			return info.Group
		}
		for _, pattern := range hostEntry.Patterns {
			if info := idx[pattern]; info != nil {
				return info.Group
			}
		}
	}
	return inventory.LocalHostGroup(hostEntry, cfg.Inventory.DefaultGroup)
}
