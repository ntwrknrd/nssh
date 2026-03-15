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
	"golang.org/x/term"
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
func ResolveHostForConnect(query string, explicitUser string) (*ResolvedHost, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, err
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
		username = cfg.Host.Defaults.DefaultUser
	}

	// Resolve credentials
	var cred *vault.ResolvedCredential
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		slog.Debug("vault not available", "err", err)
	} else {
		// Auto-prompt for unlock if needed and TTY is available
		if mgr.NeedsUnlock() {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				slog.Debug("vault locked, prompting for unlock")
				if err := clisession.Unlock(mgr, false); err != nil {
					if err == ui.ErrInterrupted {
						os.Exit(130)
					}
					slog.Warn("unlock failed", "err", err)
				}
			} else {
				slog.Debug("vault locked and no TTY available, skipping unlock")
			}
		}

		// Step 1: host-specific override (source "host" only)
		if includeFile != "" {
			cred, err = mgr.ResolveCredential(hostname, includeFile, username)
			if err != nil {
				slog.Warn("credential resolution failed", "err", err)
			}
			if cred != nil && cred.Source != "host" {
				cred = nil // only accept host-specific at this step
			}
		}

		// Steps 2-3: sync credential (class, then default)
		if cred == nil {
			cred = resolveSyncCredential(mgr, hostname, username)
		}

		// Steps 4-5: legacy include-file context + domain fallback
		if cred == nil {
			if includeFile != "" {
				cred, err = mgr.ResolveCredential(hostname, includeFile, username)
				if err != nil {
					slog.Warn("credential resolution failed", "err", err)
				}
			}
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
		Config:      cfg,
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

	// Try class credential first
	if info.CredentialClass != "" {
		cred, err := mgr.GetSyncSourceCredential(info.Source, info.CredentialClass)
		if err != nil {
			slog.Warn("sync class credential lookup failed", "err", err)
		} else if cred != nil {
			return toResolvedCredential(cred, username, "sync-class")
		}
	}

	// Try default credential
	cred, err := mgr.GetSyncSourceCredential(info.Source, "")
	if err != nil {
		slog.Warn("sync default credential lookup failed", "err", err)
	} else if cred != nil {
		return toResolvedCredential(cred, username, "sync-default")
	}

	return nil
}

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
