package vault

import "fmt"

// SyncSourceVault holds sync-owned encrypted credentials for a source.
type SyncSourceVault struct {
	DefaultCredential *Credential            `json:"default_credential,omitempty"`
	ClassCredentials  map[string]*Credential `json:"class_credentials,omitempty"`
}

// GetSyncSource returns the sync source vault entry for the named source.
func (m *Manager) GetSyncSource(source string) (*SyncSourceVault, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}
	if data.SyncSources == nil {
		return nil, nil
	}
	return data.SyncSources[source], nil
}

// ensureSyncSource loads the vault fresh and returns the data and the
// (possibly newly created) SyncSourceVault for the named source.
func (m *Manager) ensureSyncSource(source string) (*VaultData, *SyncSourceVault, error) {
	data, err := m.loadFresh()
	if err != nil {
		return nil, nil, fmt.Errorf("load vault: %w", err)
	}
	if data.SyncSources == nil {
		data.SyncSources = make(map[string]*SyncSourceVault)
	}
	sv := data.SyncSources[source]
	if sv == nil {
		sv = &SyncSourceVault{}
		data.SyncSources[source] = sv
	}
	return data, sv, nil
}

// SetSyncSourceDefaultCredential sets the default credential for a sync source.
func (m *Manager) SetSyncSourceDefaultCredential(source string, cred *Credential) error {
	data, sv, err := m.ensureSyncSource(source)
	if err != nil {
		return err
	}
	sv.DefaultCredential = cred
	m.auditInfo("sync_source_set_default", "source", source, "username", cred.Username)
	return m.save(data)
}

// SetSyncSourceClassCredential sets a class credential for a sync source.
func (m *Manager) SetSyncSourceClassCredential(source, class string, cred *Credential) error {
	data, sv, err := m.ensureSyncSource(source)
	if err != nil {
		return err
	}
	if sv.ClassCredentials == nil {
		sv.ClassCredentials = make(map[string]*Credential)
	}
	sv.ClassCredentials[class] = cred
	m.auditInfo("sync_source_set_class", "source", source, "class", class, "username", cred.Username)
	return m.save(data)
}

// GetSyncSourceCredential resolves a credential for a sync source.
// It checks class credentials first, then falls back to the default.
func (m *Manager) GetSyncSourceCredential(source, class string) (*Credential, error) {
	sv, err := m.GetSyncSource(source)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, nil
	}
	if class != "" && sv.ClassCredentials != nil {
		if c := sv.ClassCredentials[class]; c != nil {
			return c, nil
		}
	}
	return sv.DefaultCredential, nil
}

// ListSyncSources returns all sync source names in the vault.
func (m *Manager) ListSyncSources() ([]string, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}
	if data.SyncSources == nil {
		return nil, nil
	}
	names := make([]string, 0, len(data.SyncSources))
	for name := range data.SyncSources {
		names = append(names, name)
	}
	return names, nil
}

// DeleteSyncSource removes all credentials for a sync source from the vault.
func (m *Manager) DeleteSyncSource(source string) error {
	data, err := m.loadFresh()
	if err != nil {
		return fmt.Errorf("load vault: %w", err)
	}
	if data.SyncSources == nil {
		return nil
	}
	delete(data.SyncSources, source)
	m.auditInfo("sync_source_delete", "source", source)
	return m.save(data)
}
