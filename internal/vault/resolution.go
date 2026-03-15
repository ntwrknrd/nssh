package vault

import (
	"github.com/ntwrknrd/nssh/internal/secret"
)

// Credential source constants.
const (
	CredSourceHost        = "host"
	CredSourceContext     = "context"
	CredSourceSyncClass   = "sync-class"
	CredSourceSyncDefault = "sync-default"
)

// ResolvedCredential represents a resolved username/password pair.
type ResolvedCredential struct {
	Username string
	Password *secret.Secret
	Source   string // CredSource* constant
}

// ResolveCredential resolves credentials for a host using the resolution algorithm.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
//   - gitIncludeFile: The SSH config include file basename (for context lookup)
//   - username: Optional username filter (empty = use default)
//
// Algorithm:
// If username specified:
//  1. Search hosts[host].credentials for username match
//  2. Search context.credential for username match (if context exists)
//  3. Return nil if not found
//
// If username not specified:
//  1. If a host credential has default=true, use that credential
//  2. Otherwise fall back to context.credential
//  3. Return nil
func (m *Manager) ResolveCredential(host, gitIncludeFile, username string) (*ResolvedCredential, error) {
	// Get host credentials
	hostCreds, err := m.GetHostCredentials(host)
	if err != nil {
		return nil, err
	}

	// Get context credential if include file provided
	var contextCred *Credential
	if gitIncludeFile != "" {
		ctx, err := m.GetContextByIncludeFile(gitIncludeFile)
		if err != nil {
			return nil, err
		}
		if ctx != nil {
			contextCred = ctx.Credential
		}
	}

	// If username specified, find exact match
	if username != "" {
		// Check host-specific credentials first
		for _, cred := range hostCreds {
			if cred.Username == username {
				m.auditInfo("vault_resolve", "host", host, "username", username, "source", CredSourceHost)
				return &ResolvedCredential{
					Username: cred.Username,
					Password: secret.NewFromString(cred.Password),
					Source:   CredSourceHost,
				}, nil
			}
		}

		// Check context credential
		if contextCred != nil && contextCred.Username == username {
			m.auditInfo("vault_resolve", "host", host, "username", username, "source", CredSourceContext)
			return &ResolvedCredential{
				Username: contextCred.Username,
				Password: secret.NewFromString(contextCred.Password),
				Source:   CredSourceContext,
			}, nil
		}

		return nil, nil
	}

	// No username specified: find credential with default=true
	for _, cred := range hostCreds {
		if cred.Default {
			m.auditInfo("vault_resolve", "host", host, "username", cred.Username, "source", CredSourceHost)
			return &ResolvedCredential{
				Username: cred.Username,
				Password: secret.NewFromString(cred.Password),
				Source:   CredSourceHost,
			}, nil
		}
	}

	// Fall back to context credential
	if contextCred != nil {
		m.auditInfo("vault_resolve", "host", host, "username", contextCred.Username, "source", CredSourceContext)
		return &ResolvedCredential{
			Username: contextCred.Username,
			Password: secret.NewFromString(contextCred.Password),
			Source:   CredSourceContext,
		}, nil
	}

	m.auditInfo("vault_resolve_miss", "host", host, "username", username, "source", "none")
	return nil, nil
}

// ResolveCredentialWithDomain resolves credentials using domain-based context matching.
// This is used when the host identifier matches a context's domain pattern.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
//   - username: Optional username filter (empty = use default)
func (m *Manager) ResolveCredentialWithDomain(host, username string) (*ResolvedCredential, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}

	// Get host credentials
	hostCreds, err := m.GetHostCredentials(host)
	if err != nil {
		return nil, err
	}

	// If username specified, check host credentials first
	if username != "" {
		for _, cred := range hostCreds {
			if cred.Username == username {
				m.auditInfo("vault_resolve", "host", host, "username", username, "source", CredSourceHost)
				return &ResolvedCredential{
					Username: cred.Username,
					Password: secret.NewFromString(cred.Password),
					Source:   CredSourceHost,
				}, nil
			}
		}
	}

	// No username specified: find credential with default=true
	if username == "" {
		for _, cred := range hostCreds {
			if cred.Default {
				m.auditInfo("vault_resolve", "host", host, "username", cred.Username, "source", CredSourceHost)
				return &ResolvedCredential{
					Username: cred.Username,
					Password: secret.NewFromString(cred.Password),
					Source:   CredSourceHost,
				}, nil
			}
		}
	}

	// Try domain-based context matching
	for _, ctx := range data.Contexts {
		if ctx.Domain == "" || ctx.Credential == nil {
			continue
		}

		// Check if host identifier ends with domain
		if matchesDomain(host, ctx.Domain) {
			if username != "" {
				// Check if context credential matches requested username
				if ctx.Credential.Username == username {
					m.auditInfo("vault_resolve", "host", host, "username", username, "source", CredSourceContext)
					return &ResolvedCredential{
						Username: ctx.Credential.Username,
						Password: secret.NewFromString(ctx.Credential.Password),
						Source:   CredSourceContext,
					}, nil
				}
			} else {
				// No username specified, use context credential
				m.auditInfo("vault_resolve", "host", host, "username", ctx.Credential.Username, "source", CredSourceContext)
				return &ResolvedCredential{
					Username: ctx.Credential.Username,
					Password: secret.NewFromString(ctx.Credential.Password),
					Source:   CredSourceContext,
				}, nil
			}
		}
	}

	m.auditInfo("vault_resolve_miss", "host", host, "username", username, "source", "none")
	return nil, nil
}

// matchesDomain checks if hostname matches a domain pattern.
// Pattern "example.com" matches "server.example.com" but not "example.com.other.org"
func matchesDomain(hostname, domain string) bool {
	if domain == "" {
		return false
	}

	// Exact match
	if hostname == domain {
		return true
	}

	// Subdomain match: hostname ends with ".domain"
	if len(hostname) > len(domain)+1 {
		suffix := "." + domain
		return hostname[len(hostname)-len(suffix):] == suffix
	}

	return false
}
