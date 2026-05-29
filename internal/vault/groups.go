package vault

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/secret"
)

// GetGroupCredential returns the SSH target credential for a group.
func (m *Manager) GetGroupCredential(group string) (*Credential, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}
	if data.Groups != nil {
		if cred := data.Groups[group]; cred != nil {
			return cred, nil
		}
	}

	// Transitional read support for released context credentials until upgrade
	// migrates them into group credential records.
	if ctx := data.Contexts[group]; ctx != nil && ctx.Credential != nil {
		return ctx.Credential, nil
	}
	return nil, nil
}

// SetGroupCredential creates or replaces a group SSH target credential.
func (m *Manager) SetGroupCredential(group, username string, password *secret.Secret) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}
	if data.Groups == nil {
		data.Groups = make(map[string]*Credential)
	}
	pw, err := extractPassword(password)
	if err != nil {
		return err
	}
	data.Groups[group] = &Credential{Username: username, Password: pw}
	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_group_set_credential", "group", group, "username", username)
	return nil
}

// RemoveGroupCredential removes a group SSH target credential.
func (m *Manager) RemoveGroupCredential(group string) (bool, error) {
	unlock, err := m.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return false, err
	}
	if data.Groups == nil {
		return false, nil
	}
	if _, ok := data.Groups[group]; !ok {
		return false, nil
	}
	delete(data.Groups, group)
	if err := m.save(data); err != nil {
		return false, fmt.Errorf("save group credential: %w", err)
	}
	m.auditInfo("vault_group_remove_credential", "group", group)
	return true, nil
}
