package vault

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/ntwrknrd/nssh/internal/secret"
)

// ListContexts returns all contexts sorted by name.
func (m *Manager) ListContexts() ([]ContextEntry, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}

	var entries []ContextEntry
	for name, ctx := range data.Contexts {
		entries = append(entries, m.buildContextEntry(name, ctx))
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// GetContext returns a context by name.
func (m *Manager) GetContext(name string) (*ContextEntry, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}

	ctx, ok := data.Contexts[name]
	if !ok {
		return nil, nil
	}

	entry := m.buildContextEntry(name, ctx)
	return &entry, nil
}

// GetContextByIncludeFile returns a context matching the SSH include file.
// Accepts either a filename or full path - extracts basename for matching.
func (m *Manager) GetContextByIncludeFile(includeFile string) (*ContextEntry, error) {
	if includeFile == "" {
		return nil, nil
	}

	data, err := m.load()
	if err != nil {
		return nil, err
	}

	// Extract basename to match stored filenames
	requestedBase := filepath.Base(includeFile)

	for name, ctx := range data.Contexts {
		storedBase := filepath.Base(ctx.GitIncludeFile)
		if storedBase == requestedBase {
			entry := m.buildContextEntry(name, ctx)
			return &entry, nil
		}
	}

	return nil, nil
}

// buildContextEntry creates a ContextEntry from a context.
func (m *Manager) buildContextEntry(name string, ctx *Context) ContextEntry {
	count := 0
	if ctx.Credential != nil {
		count = 1
	}
	return ContextEntry{
		Name:            name,
		GitIncludeFile:  ctx.GitIncludeFile,
		Domain:          ctx.Domain,
		Credential:      ctx.Credential,
		CredentialCount: count,
	}
}

// CreateContext creates a new context.
func (m *Manager) CreateContext(name, gitIncludeFile, domain string, cred *Credential) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	if _, exists := data.Contexts[name]; exists {
		return fmt.Errorf("context %q already exists", name)
	}

	data.Contexts[name] = &Context{
		GitIncludeFile: gitIncludeFile,
		Domain:         domain,
		Credential:     cred,
	}

	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_context_create", "context", name, "domain", domain, "include", gitIncludeFile)
	return nil
}

// AddContextCredential adds or updates a context's credential.
func (m *Manager) AddContextCredential(name, username string, password *secret.Secret, overwrite bool) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	ctx, exists := data.Contexts[name]
	if !exists {
		return fmt.Errorf("context %q does not exist", name)
	}

	if ctx.Credential != nil && !overwrite {
		return fmt.Errorf("context %q already has a credential", name)
	}

	pw, err := extractPassword(password)
	if err != nil {
		return err
	}

	ctx.Credential = &Credential{
		Username: username,
		Password: pw,
	}

	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_context_set_credential", "context", name, "username", username, "overwrite", overwrite)
	return nil
}

// DeleteContext removes a context.
func (m *Manager) DeleteContext(name string) (bool, error) {
	unlock, err := m.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return false, err
	}

	if _, exists := data.Contexts[name]; !exists {
		return false, nil
	}

	delete(data.Contexts, name)
	if err := m.save(data); err != nil {
		return false, err
	}
	m.auditInfo("vault_context_delete", "context", name)
	return true, nil
}

// UpdateContextDomain updates a context's domain.
func (m *Manager) UpdateContextDomain(name, domain string) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	ctx, exists := data.Contexts[name]
	if !exists {
		return fmt.Errorf("context %q does not exist", name)
	}

	ctx.Domain = domain
	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_context_update_domain", "context", name, "domain", domain)
	return nil
}

// UpdateContextIncludeFile updates a context's SSH config include file.
func (m *Manager) UpdateContextIncludeFile(name, gitIncludeFile string) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	ctx, exists := data.Contexts[name]
	if !exists {
		return fmt.Errorf("context %q does not exist", name)
	}

	ctx.GitIncludeFile = gitIncludeFile
	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_context_update_include", "context", name, "include", gitIncludeFile)
	return nil
}

// RemoveContextCredential removes a context's credential.
func (m *Manager) RemoveContextCredential(name string) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	ctx, exists := data.Contexts[name]
	if !exists {
		return fmt.Errorf("context %q does not exist", name)
	}

	ctx.Credential = nil
	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_context_remove_credential", "context", name)
	return nil
}
