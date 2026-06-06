// Package connect extracts shared host lookup and credential resolution so
// interactive SSH and remote command execution can share the same path.
package connect

import (
	"context"
	"fmt"
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
	Hostname    string
	Username    string
	AuthMode    string
	IncludeFile string
	HostEntry   *sshconfig.HostEntry
	Credential  *ResolvedCredential
	Config      *config.Config
}

type CredentialTarget struct {
	Host            string
	Provider        string
	Source          string
	Ref             string
	RefKind         string
	UsernamePresent bool
	Password        *secret.Secret
	Resolver        func(context.Context) (*secret.Secret, error)
}

func CredentialTargetFromResolvedHost(resolved *ResolvedHost) (*CredentialTarget, error) {
	if resolved == nil {
		return nil, nil
	}
	if resolved.Credential == nil {
		return nil, fmt.Errorf("host %s has no configured credential", resolved.Hostname)
	}
	cred := resolved.Credential
	if cred.Password == nil && cred.PasswordResolver == nil {
		return nil, fmt.Errorf("host %s has no configured credential", resolved.Hostname)
	}
	return &CredentialTarget{
		Host:            resolved.Hostname,
		Provider:        cred.Provider,
		Source:          cred.Source,
		Ref:             cred.Ref,
		RefKind:         credentialRefKind(cred.Ref),
		UsernamePresent: strings.TrimSpace(resolved.Username) != "" || strings.TrimSpace(cred.Username) != "",
		Password:        cred.Password,
		Resolver:        cred.PasswordResolver,
	}, nil
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
	authCtx, managed := resolveInventoryAuthContext(hostEntry, hostname, group, c)
	auth := c.ResolveInventoryAuth(authCtx)

	hostUser := ""
	if hostEntry != nil && !managed {
		hostUser = hostEntry.User()
	}

	// Resolve credentials
	var cred *ResolvedCredential
	credentialUser := auth.Username
	registry, err := credential.NewRegistry(c)
	if err != nil {
		slog.Debug("credential registry not available", "err", err)
	} else {
		cred, err = resolveInventoryCredential(registry, auth, explicitUser)
		if err != nil {
			slog.Warn("credential resolution failed", "err", err)
		} else if cred != nil && credentialUser == "" {
			credentialUser = cred.Username
		}
	}

	username := selectConnectionUsername(managed, explicitUser, hostUser, "", credentialUser, "")

	return &ResolvedHost{
		Query:       query,
		Hostname:    hostname,
		Username:    username,
		AuthMode:    auth.AuthMode,
		IncludeFile: includeFile,
		HostEntry:   hostEntry,
		Credential:  cred,
		Config:      c,
	}, nil
}

func resolveInventoryAuthContext(hostEntry *sshconfig.HostEntry, hostname, group string, cfg *config.Config) (config.InventoryAuthContext, bool) {
	ctx := config.InventoryAuthContext{Host: hostname, Group: group}
	if hostEntry == nil {
		return ctx, false
	}
	if idx, err := inventory.BuildProviderIndex(); err == nil {
		if info := idx[hostEntry.Host]; info != nil {
			ctx.Provider = info.Provider
			ctx.Group = info.Group
			return ctx, true
		}
		for _, pattern := range hostEntry.Patterns {
			if info := idx[pattern]; info != nil {
				ctx.Provider = info.Provider
				ctx.Group = info.Group
				return ctx, true
			}
		}
	}
	if group != "" && inventory.LocalHostGroup(hostEntry, "") != "" {
		return ctx, true
	}
	return ctx, false
}

type providerRegistry interface {
	Provider(name string) credential.Provider
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

func credentialRefKind(ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return ""
	case strings.HasPrefix(ref, "op://") && strings.HasSuffix(ref, "/"):
		return "1password_item"
	case strings.HasPrefix(ref, "op://"):
		return "1password_secret"
	default:
		return "provider_ref"
	}
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
	return inventory.LocalHostGroup(hostEntry, "")
}
