package vault

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CreateBackup creates a timestamped backup of the current vault.
// Returns the backup path on success, empty string if no vault exists.
func (m *Manager) CreateBackup() (string, error) {
	if _, err := os.Stat(m.credentialPath); os.IsNotExist(err) {
		return "", nil // Nothing to backup
	}

	if err := os.MkdirAll(m.backupDir, 0700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("credentials.age.%s", timestamp)
	backupPath := filepath.Join(m.backupDir, backupName)

	// Copy file
	src, err := os.ReadFile(m.credentialPath)
	if err != nil {
		return "", fmt.Errorf("read source for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, src, 0600); err != nil {
		return "", fmt.Errorf("write backup file: %w", err)
	}

	// Prune old backups
	m.pruneBackups()

	return backupPath, nil
}

// backup creates a backup of the current credential file.
func (m *Manager) backup() error {
	if _, err := os.Stat(m.credentialPath); os.IsNotExist(err) {
		return nil // Nothing to backup
	}

	if err := os.MkdirAll(m.backupDir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("credentials.age.bak.%s", timestamp)
	backupPath := filepath.Join(m.backupDir, backupName)

	// Copy file
	src, err := os.Open(m.credentialPath)
	if err != nil {
		return fmt.Errorf("open source for backup: %w", err)
	}
	defer func() {
		if err := src.Close(); err != nil {
			slog.Debug("failed to close backup source", "err", err)
		}
	}()

	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil // Backup already exists for this timestamp, skip
		}
		return fmt.Errorf("create backup file: %w", err)
	}
	defer func() {
		if err := dst.Close(); err != nil {
			slog.Debug("failed to close backup dest", "err", err)
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy to backup: %w", err)
	}

	m.pruneBackups()
	m.auditInfo("vault_backup_created", "path", backupPath)
	return nil
}

// pruneBackups removes old backups beyond maxBackups.
func (m *Manager) pruneBackups() {
	if m.maxBackups <= 0 {
		return
	}

	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		return // Ignore if dir doesn't exist
	}

	// Filter backup files
	var backups []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) != ".tmp" {
			backups = append(backups, e)
		}
	}

	if len(backups) <= m.maxBackups {
		return
	}

	// Sort by modification time (newest first)
	// Note: Info() errors are intentionally ignored - if stat fails,
	// we return false to preserve existing order for those entries
	sort.Slice(backups, func(i, j int) bool {
		fi, errI := backups[i].Info()
		fj, errJ := backups[j].Info()
		if errI != nil || errJ != nil || fi == nil || fj == nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})

	// Remove oldest
	for _, b := range backups[m.maxBackups:] {
		_ = os.Remove(filepath.Join(m.backupDir, b.Name()))
	}
	if len(backups) > m.maxBackups {
		m.auditInfo("vault_backup_pruned", "kept", m.maxBackups, "removed", len(backups)-m.maxBackups)
	}
}
