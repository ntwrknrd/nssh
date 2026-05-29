package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// ReEncryptVault re-encrypts the vault with a new recipient.
// For safer migration with verification, use ReEncryptVaultWithVerify.
func (m *Manager) ReEncryptVault(newRecipient age.Recipient) error {
	_, err := m.ReEncryptVaultWithVerify(newRecipient, nil)
	return err
}

// ReEncryptVaultWithVerify re-encrypts the vault with a new recipient.
// If verifyFn is provided, it's called with the temp file path before committing.
// The original file is only replaced if verification succeeds.
// Returns the backup path (for recovery if needed) and any error.
func (m *Manager) ReEncryptVaultWithVerify(newRecipient age.Recipient, verifyFn func(tempPath string) error) (backupPath string, err error) {
	// Check if vault file exists
	if _, statErr := os.Stat(m.credentialPath); os.IsNotExist(statErr) {
		return "", nil // No vault to re-encrypt
	}

	// Create timestamped backup before any changes
	backupPath, err = m.CreateBackup()
	if err != nil {
		return "", fmt.Errorf("create backup: %w", err)
	}
	if backupPath != "" {
		slog.Info("backup created", "path", backupPath)
	}

	// Decrypt vault using current identity
	data, err := m.decrypt()
	if err != nil {
		return backupPath, fmt.Errorf("decrypt vault: %w", err)
	}

	// Marshal to JSON
	plaintext, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return backupPath, fmt.Errorf("marshal credentials: %w", err)
	}

	// Create temp file for atomic write
	dir := filepath.Dir(m.credentialPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return backupPath, fmt.Errorf("create credential dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".credentials-*.age.tmp")
	if err != nil {
		return backupPath, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on error
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	// Encrypt with new recipient
	w, err := age.Encrypt(tmpFile, newRecipient)
	if err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Debug("failed to close temp file after encrypt error", "err", closeErr)
		}
		return backupPath, fmt.Errorf("create age writer: %w", err)
	}

	if _, err := w.Write(plaintext); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Debug("failed to close temp file after write error", "err", closeErr)
		}
		return backupPath, fmt.Errorf("write encrypted data: %w", err)
	}

	if err := w.Close(); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Debug("failed to close temp file after age close error", "err", closeErr)
		}
		return backupPath, fmt.Errorf("close age writer: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return backupPath, fmt.Errorf("close temp file: %w", err)
	}

	// Set secure permissions before moving
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return backupPath, fmt.Errorf("set permissions: %w", err)
	}

	// If verification function provided, call it before committing
	if verifyFn != nil {
		if verifyErr := verifyFn(tmpPath); verifyErr != nil {
			return backupPath, fmt.Errorf("verification failed: %w", verifyErr)
		}
	}

	// Atomic rename - only happens after verification passes
	if err := os.Rename(tmpPath, m.credentialPath); err != nil {
		return backupPath, fmt.Errorf("atomic rename: %w", err)
	}

	tmpPath = "" // Prevent cleanup of renamed file

	// Invalidate cache
	m.cache = nil

	return backupPath, nil
}

// decrypt reads and decrypts the credential file.
func (m *Manager) decrypt() (*VaultData, error) {
	// Check if credentials file exists
	ciphertext, err := os.ReadFile(m.credentialPath)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyData(), nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var plaintext []byte

	switch m.state() {
	case stateUninitialized:
		return nil, errors.New("vault not initialized: run 'nssh self init'")

	case stateProvided:
		// Provided mode: use pre-loaded identities (rekey/migration operations)
		r, err := age.Decrypt(
			io.NewSectionReader(&bytesReaderAt{ciphertext}, 0, int64(len(ciphertext))),
			m.identities...,
		)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials: %w", err)
		}
		plaintext, err = io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read decrypted data: %w", err)
		}

	default: // stateLocked - use injected decrypt function
		if m.sessionDeps.Decrypt == nil {
			return nil, m.lockedError()
		}

		ctx := m.sessionDeps.BaseContext
		if ctx == nil {
			ctx = context.Background()
		}

		var decryptErr error
		plaintext, decryptErr = m.sessionDeps.Decrypt(ctx, ciphertext)
		if decryptErr != nil {
			if errors.Is(decryptErr, ErrSessionUnavailable) {
				return nil, m.lockedError()
			}
			return nil, fmt.Errorf("session decrypt: %w", decryptErr)
		}
	}

	var data VaultData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf("parse credential JSON: %w", err)
	}

	// Ensure maps are initialized
	if data.Contexts == nil {
		data.Contexts = make(map[string]*Context)
	}
	if data.Hosts == nil {
		data.Hosts = make(map[string]*HostCredentials)
	}
	if data.Groups == nil {
		data.Groups = make(map[string]*Credential)
	}
	if data.SyncSources == nil {
		data.SyncSources = make(map[string]*SyncSourceVault)
	}

	return &data, nil
}

// bytesReaderAt implements io.ReaderAt for []byte.
type bytesReaderAt struct {
	data []byte
}

func (b *bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n = copy(p, b.data[off:])
	if n < len(p) {
		err = io.EOF
	}
	return
}

// encrypt writes the vault data to the encrypted file.
func (m *Manager) encrypt(data *VaultData) error {
	// Get the recipient for encryption. This uses GetRecipient() which
	// reads from age.pub in protected mode, allowing encryption even when
	// the private key is held by an agent in another process.
	recipient, err := m.GetRecipient()
	if err != nil {
		return fmt.Errorf("get recipient for encryption: %w", err)
	}

	plaintext, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	// Create temp file for atomic write
	dir := filepath.Dir(m.credentialPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credential dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".credentials-*.age.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on error
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	w, err := age.Encrypt(tmpFile, recipient)
	if err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Debug("failed to close temp file after encrypt error", "err", closeErr)
		}
		return fmt.Errorf("create age writer: %w", err)
	}

	if _, err := w.Write(plaintext); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Debug("failed to close temp file after write error", "err", closeErr)
		}
		return fmt.Errorf("write encrypted data: %w", err)
	}

	if err := w.Close(); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Debug("failed to close temp file after age close error", "err", closeErr)
		}
		return fmt.Errorf("close age writer: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Set secure permissions before moving
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, m.credentialPath); err != nil {
		return fmt.Errorf("rename credential file: %w", err)
	}

	tmpPath = "" // Prevent cleanup of renamed file

	// Update cache
	m.cache = data
	if fi, err := os.Stat(m.credentialPath); err == nil {
		m.cacheMtime = fi.ModTime()
	}

	return nil
}

// emptyData returns an empty vault structure.
func emptyData() *VaultData {
	return &VaultData{
		Contexts:    make(map[string]*Context),
		Groups:      make(map[string]*Credential),
		Hosts:       make(map[string]*HostCredentials),
		SyncSources: make(map[string]*SyncSourceVault),
	}
}

// load returns cached data or decrypts fresh if stale.
func (m *Manager) load() (*VaultData, error) {
	// Check if cache is valid
	if m.cache != nil {
		fi, err := os.Stat(m.credentialPath)
		if err == nil && fi.ModTime().Equal(m.cacheMtime) {
			return m.cache, nil
		}
	}

	data, err := m.decrypt()
	if err != nil {
		return nil, err
	}

	m.cache = data
	if fi, err := os.Stat(m.credentialPath); err == nil {
		m.cacheMtime = fi.ModTime()
	}

	return data, nil
}

// loadFresh always decrypts from disk (for use within locks).
func (m *Manager) loadFresh() (*VaultData, error) {
	data, err := m.decrypt()
	if err != nil {
		return nil, err
	}

	m.cache = data
	if fi, err := os.Stat(m.credentialPath); err == nil {
		m.cacheMtime = fi.ModTime()
	}

	return data, nil
}

// save writes vault data with locking and backup.
func (m *Manager) save(data *VaultData) error {
	if err := m.backup(); err != nil {
		// Log but don't fail on backup errors
		fmt.Fprintf(os.Stderr, "warning: backup failed: %v\n", err)
	}

	return m.encrypt(data)
}
