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
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	intsync "github.com/ntwrknrd/nssh/internal/sync"
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

	// Resolve credentials
	var cred *vault.ResolvedCredential
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		slog.Debug("vault not available", "err", err)
	} else {
		// Auto-prompt for unlock if needed and TTY is available
		if err := clisession.TryUnlockIfTTY(mgr); err != nil {
			if err == ui.ErrInterrupted {
				os.Exit(130)
			}
			slog.Warn("unlock failed", "err", err)
		}

		// Single vault resolution call -- split result by source
		var hostCred, contextCred *vault.ResolvedCredential
		if includeFile != "" {
			resolved, resolveErr := mgr.ResolveCredential(hostname, includeFile, username)
			if resolveErr != nil {
				slog.Warn("credential resolution failed", "err", resolveErr)
			} else if resolved != nil {
				if resolved.Source == vault.CredSourceHost {
					hostCred = resolved
				} else {
					contextCred = resolved
				}
			}
		}

		// Step 1: host-specific override
		cred = hostCred

		// Steps 2-4: sync credential (class, default, context)
		if cred == nil && len(c.Sync.Sources) > 0 {
			cred = resolveSyncCredential(mgr, hostname, username)
		}

		// Steps 5-6: legacy include-file context + domain fallback
		if cred == nil {
			cred = contextCred
			if cred == nil {
				cred, err = mgr.ResolveCredentialWithDomain(hostname, username)
				if err != nil {
					slog.Warn("domain credential resolution failed", "err", err)
				}
			}
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

// resolveSyncCredential checks the sync index for the hostname and tries
// class-specific then default sync credentials. Returns nil if the host
// is not sync-managed or no sync credential is configured.
func resolveSyncCredential(mgr *vault.Manager, hostname, username string) *vault.ResolvedCredential {
	syncIndex, err := intsync.BuildSyncIndex()
	if err != nil {
		slog.Debug("sync index unavailable", "err", err)
		return nil
	}

	info := syncIndex[hostname]
	if info == nil {
		return nil
	}

	sv, err := mgr.GetSyncSource(info.Source)
	if err != nil {
		slog.Warn("sync credential lookup failed", "err", err)
		return nil
	}
	if sv == nil {
		return nil
	}

	// Try class credential first, then default
	if info.CredentialClass != "" && sv.ClassCredentials != nil {
		if c := sv.ClassCredentials[info.CredentialClass]; c != nil {
			return toResolvedCredential(c, username, vault.CredSourceSyncClass)
		}
	}
	if sv.DefaultCredential != nil {
		return toResolvedCredential(sv.DefaultCredential, username, vault.CredSourceSyncDefault)
	}

	// Step 4: sync-managed context credential
	// Context credentials own both username and password (unlike class/default
	// which share a password across devices and take the username from the SSH
	// config chain). Build the result directly to match legacy context behavior
	// in vault.ResolveCredential.
	if info.Context != "" {
		ctx, err := mgr.GetContext(info.Context)
		if err != nil {
			slog.Warn("sync context lookup failed", "err", err)
		} else if ctx != nil && ctx.Credential != nil {
			return &vault.ResolvedCredential{
				Username: ctx.Credential.Username,
				Password: secret.NewFromString(ctx.Credential.Password),
				Source:   vault.CredSourceSyncContext,
			}
		}
	}

	return nil
}

// toResolvedCredential builds a ResolvedCredential from a vault entry.
// Sync credentials are class-based (shared password across a device class),
// so the username comes from the normal resolution chain (SSH config, nssh
// defaults) rather than the stored credential. The caller-provided username
// takes precedence when set; the credential's own username is only used as
// a fallback for non-sync credential sources.
func toResolvedCredential(cred *vault.Credential, username, source string) *vault.ResolvedCredential {
	u := cred.Username
	if username != "" {
		u = username
	}
	return &vault.ResolvedCredential{
		Username: u,
		Password: secret.NewFromString(cred.Password),
		Source:   source,
	}
}
