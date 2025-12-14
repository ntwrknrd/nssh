package vault

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/secret"
)

// GetHostCredentials returns credentials for a specific host.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
func (m *Manager) GetHostCredentials(host string) ([]Credential, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}

	hostData, ok := data.Hosts[host]
	if !ok || hostData == nil {
		m.auditInfo("vault_read_host_credentials", "host", host, "count", 0)
		return nil, nil
	}

	m.auditInfo("vault_read_host_credentials", "host", host, "count", len(hostData.Credentials))
	return hostData.Credentials, nil
}

// GetHostDefaultCredential returns the default credential username for a host.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
func (m *Manager) GetHostDefaultCredential(host string) (string, error) {
	creds, err := m.GetHostCredentials(host)
	if err != nil {
		return "", err
	}
	for _, cred := range creds {
		if cred.Default {
			return cred.Username, nil
		}
	}
	return "", nil
}

// SetHostDefaultCredential sets the default credential username for a host.
// Pass empty username to clear the default.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
//   - username: The username to set as default (empty to clear)
func (m *Manager) SetHostDefaultCredential(host, username string) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	hostData, exists := data.Hosts[host]
	if !exists || hostData == nil {
		return fmt.Errorf("host %q does not have any credentials", host)
	}

	// Toggle default flags
	found := username == "" // If clearing, consider it found
	for i := range hostData.Credentials {
		if hostData.Credentials[i].Username == username {
			hostData.Credentials[i].Default = true
			found = true
		} else {
			hostData.Credentials[i].Default = false
		}
	}

	if !found {
		return fmt.Errorf("credential %q not found for host %q", username, host)
	}

	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_host_set_default", "host", host, "username", username)
	return nil
}

// ListHostsWithCredentials returns a set of Host identifiers that have custom credentials.
func (m *Manager) ListHostsWithCredentials() (map[string]bool, error) {
	data, err := m.load()
	if err != nil {
		return nil, err
	}

	result := make(map[string]bool)
	for host, hostData := range data.Hosts {
		if hostData != nil && len(hostData.Credentials) > 0 {
			result[host] = true
		}
	}

	return result, nil
}

// AddHostCredential adds a credential to a host.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
//   - username: The username for the credential
//   - password: The password for the credential
func (m *Manager) AddHostCredential(host, username string, password *secret.Secret) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return err
	}

	pw, err := extractPassword(password)
	if err != nil {
		return err
	}

	if data.Hosts[host] == nil {
		data.Hosts[host] = &HostCredentials{
			Credentials: []Credential{},
		}
	}

	data.Hosts[host].Credentials = append(data.Hosts[host].Credentials, Credential{
		Username: username,
		Password: pw,
	})

	if err := m.save(data); err != nil {
		return err
	}
	m.auditInfo("vault_host_add_credential", "host", host, "username", username)
	return nil
}

// DeleteHostCredentials removes all credentials for a host.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
func (m *Manager) DeleteHostCredentials(host string) (bool, error) {
	unlock, err := m.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return false, err
	}

	if _, exists := data.Hosts[host]; !exists {
		return false, nil
	}

	delete(data.Hosts, host)
	if err := m.save(data); err != nil {
		return false, err
	}
	m.auditInfo("vault_host_delete_credentials", "host", host)
	return true, nil
}

// UpdateHostCredential updates the password for an existing host credential.
// Returns true if updated, false if not found.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
//   - username: The username to update
//   - password: The new password
func (m *Manager) UpdateHostCredential(host, username string, password *secret.Secret) (bool, error) {
	unlock, err := m.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return false, err
	}

	hostData, exists := data.Hosts[host]
	if !exists || hostData == nil {
		return false, nil
	}

	pw, err := extractPassword(password)
	if err != nil {
		return false, err
	}

	// Find and update the credential
	for i, cred := range hostData.Credentials {
		if cred.Username == username {
			hostData.Credentials[i].Password = pw
			if err := m.save(data); err != nil {
				return false, err
			}
			m.auditInfo("vault_host_update_credential", "host", host, "username", username)
			return true, nil
		}
	}

	return false, nil
}

// RemoveHostCredential removes a specific credential by username from a host.
// Returns true if a credential was removed, false if not found.
//
// Parameters:
//   - host: The SSH Host identifier (unique within a context/include file)
//   - username: The username to remove
func (m *Manager) RemoveHostCredential(host, username string) (bool, error) {
	unlock, err := m.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	data, err := m.loadFresh()
	if err != nil {
		return false, err
	}

	hostData, exists := data.Hosts[host]
	if !exists || hostData == nil {
		return false, nil
	}

	// Find and remove the credential
	found := false
	newCreds := make([]Credential, 0, len(hostData.Credentials))
	for _, cred := range hostData.Credentials {
		if cred.Username == username {
			found = true
			continue // Skip this one (remove it)
		}
		newCreds = append(newCreds, cred)
	}

	if !found {
		return false, nil
	}

	// If no credentials left, remove the host entry entirely
	if len(newCreds) == 0 {
		delete(data.Hosts, host)
	} else {
		hostData.Credentials = newCreds
	}

	if err := m.save(data); err != nil {
		return false, err
	}
	m.auditInfo("vault_host_remove_credential", "host", host, "username", username)
	return true, nil
}
