// Package resolve extracts shared host lookup, credential resolution, and
// vault unlock logic so that both interactive connect and remote command
// execution can share the same resolution path.
package resolve

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
)

// ResolvedHost holds the result of host resolution: everything needed to
// connect or run a remote command.
type ResolvedHost struct {
	Query       string
	Hostname    string
	Username    string
	IncludeFile string
	HostEntry   *sshconfig.HostEntry
	Credential  *vault.ResolvedCredential
	Manager     *vault.Manager
	Config      *config.Config
}

// ResolveHostForConnect performs host lookup, include-file discovery, vault
// unlock, and credential resolution. This is the shared path used by both
// interactive connect and remote command execution.
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

	// Resolve username: explicit > SSH config > nssh default
	username := explicitUser
	if username == "" && hostEntry != nil {
		username = hostEntry.User()
	}
	if username == "" {
		username = c.Host.Defaults.DefaultUser
	}

	group := resolveGroup(hostEntry, includeFile, c)

	// Resolve credentials
	var cred *vault.ResolvedCredential
	var mgr *vault.Manager
	provider, err := credentialProviderForConnect(c, &mgr)
	if err != nil {
		slog.Debug("credential provider not available", "err", err)
	} else {
		cred, err = resolveTargetCredential(provider, hostname, group, username)
		if err != nil {
			slog.Warn("credential resolution failed", "err", err)
		}
	}

	// Update username from resolved credential
	if cred != nil {
		username = cred.Username
	}

	return &ResolvedHost{
		Query:       query,
		Hostname:    hostname,
		Username:    username,
		IncludeFile: includeFile,
		HostEntry:   hostEntry,
		Credential:  cred,
		Manager:     mgr,
		Config:      c,
	}, nil
}

func credentialProviderForConnect(cfg *config.Config, mgrOut **vault.Manager) (credential.Provider, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	credCfg := cfg.Credential
	if err := credCfg.Validate(); err != nil {
		return nil, err
	}
	if credCfg.Type != config.CredentialProviderAge {
		return credential.NewProvider(cfg)
	}

	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return nil, err
	}
	if err := clisession.TryUnlockIfTTY(mgr); err != nil {
		if err == ui.ErrInterrupted {
			os.Exit(130)
		}
		slog.Warn("unlock failed", "err", err)
	}
	if mgrOut != nil {
		*mgrOut = mgr
	}
	return credential.NewAgeProvider(mgr), nil
}

func resolveTargetCredential(provider credential.Provider, hostname, group, username string) (*vault.ResolvedCredential, error) {
	if provider == nil {
		return nil, nil
	}
	if shouldQueryHostCredential(provider, hostname) {
		hostCred, err := provider.GetHost(hostname)
		if err != nil {
			return nil, err
		}
		if credentialMatchesUser(hostCred, username) {
			return &vault.ResolvedCredential{
				Username: hostCred.Username,
				Password: hostCred.Secret,
				Source:   vault.CredSourceHost,
			}, nil
		}
	}
	if group == "" {
		return nil, nil
	}
	groupCred, err := provider.GetGroup(group)
	if err != nil || groupCred == nil {
		return nil, err
	}
	if !credentialMatchesUser(groupCred, username) {
		return nil, nil
	}
	return &vault.ResolvedCredential{
		Username: groupCred.Username,
		Password: groupCred.Secret,
		Source:   vault.CredSourceGroup,
	}, nil
}

func shouldQueryHostCredential(provider credential.Provider, hostname string) bool {
	indexed, ok := provider.(credential.HostCredentialIndex)
	return !ok || indexed.HasHostCredential(hostname)
}

func credentialMatchesUser(record *credential.Record, username string) bool {
	if record == nil {
		return false
	}
	return username == "" || record.Username == username
}

func resolveGroup(hostEntry *sshconfig.HostEntry, includeFile string, cfg *config.Config) string {
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
