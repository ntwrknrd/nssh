package self

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/agent"
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/ntwrknrd/nssh/internal/vault/software"
	"github.com/spf13/cobra"
)

// NewRekeyCmd creates the rekey command.
func NewRekeyCmd() *cobra.Command {
	var (
		repairPubkey   bool
		rollback       bool
		switchHardware bool
		switchSoftware bool
	)

	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Rotate or switch security mode",
		Long: `Rotate the credential encryption keys or switch between security modes.

By default, rotates keys within the current mode:
  - Software mode: generates new passphrase-protected key
  - Hardware mode: generates new age identity (P-256 key on YubiKey preserved)

Use --software or --hardware to switch security modes while preserving
all stored credentials. The vault is re-encrypted with the new key.

Use --repair-pubkey to regenerate the public key file without rotating keys.
Use --rollback to restore from the most recent rollback backup (rekey or mode switch).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if rollback {
				return runRollback()
			}
			return runRekey(repairPubkey, switchHardware, switchSoftware)
		},
	}

	cmd.Flags().BoolVar(&repairPubkey, "repair-pubkey", false, "regenerate age.pub without rotation")
	cmd.Flags().BoolVar(&rollback, "rollback", false, "restore from rollback backup")
	cmd.Flags().BoolVar(&switchHardware, "hardware", false, "switch to YubiKey PIV mode")
	cmd.Flags().BoolVar(&switchSoftware, "software", false, "switch to passphrase mode")

	return cmd
}

func runRekey(repairPubkey, switchHardware, switchSoftware bool) error {
	paths := config.DefaultPaths()
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Validate flags
	if switchHardware && switchSoftware {
		return fmt.Errorf("cannot specify both --hardware and --software")
	}

	// Detect current mode from filesystem
	currentMode, err := vault.DetectSecurityMode(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("detect security mode: %w", err)
	}

	// Validate mode switch flags
	if switchHardware && currentMode == agent.ModePIV {
		return fmt.Errorf("already in hardware mode. Use 'rekey' without flags to rotate keys")
	}
	if switchSoftware && currentMode == agent.ModeSoftware {
		return fmt.Errorf("already in software mode. Use 'rekey' without flags to rotate keys")
	}

	ui.CommandStart("REKEY CREDENTIALS")

	// Create vault manager to get current identity
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fmt.Errorf("initialize vault: %w", err)
	}

	// Ensure vault is unlocked
	if mgr.NeedsUnlock() {
		ui.Info("Unlocking current credentials...")
		if err := clisession.Unlock(mgr, false); err != nil {
			return fmt.Errorf("unlock failed: %w", err)
		}
	}

	// Handle --repair-pubkey
	if repairPubkey {
		return repairPublicKey(mgr, paths)
	}

	// Dispatch based on flags and current mode
	switch {
	case switchHardware:
		// software -> hardware mode switch
		return runSoftwareToHardware(paths, cfg)
	case switchSoftware:
		// hardware -> software mode switch
		return runHardwareToSoftware(paths, cfg)
	case currentMode == agent.ModePIV:
		// Same-mode rekey for hardware
		return runRekeyPIV(paths, cfg)
	default:
		// Same-mode rekey for software (fall through to existing logic)
	}

	// Software mode: Continue with passphrase-based rekey

	// Create target store config
	ksCfg := software.Config{
		ConfigDir:           paths.ConfigDir,
		DataDir:             paths.DataDir,
		StateDir:            paths.StateDir,
		ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
		PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
	}

	// Create new store
	targetKs, err := software.New(ksCfg)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	// Create rollback backup of current state (keys + vault)
	// This allows manual recovery if something goes catastrophically wrong
	rollbackDir := filepath.Join(paths.BackupDir, fmt.Sprintf("%s%s", rollbackPrefixRekey, time.Now().Format(rollbackTimeFormat)))
	if err := createRollbackBackup(rollbackDir, paths); err != nil {
		return fmt.Errorf("create rollback backup: %w", err)
	}
	ui.Success("Rollback backup created")
	writeRollbackMetadata(rollbackDir, rollbackMetadata{
		Kind:      "rekey",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// Full vault re-encryption flow using staged keys:
	// 1. Get current identity (already done via mgr.Unlock())
	// 2. Create new keys in staged temp files (prompts for new passphrase)
	// 3. Re-encrypt vault with new recipient
	// 4. Verify decryption with new key
	// 5. Commit staged keys (atomic swap) - only after verification passes
	// This ensures old keys are preserved if vault re-encryption fails.

	ui.SubSection("Creating new keys")

	passphraseBuf, err := ui.PasswordSecureWithConfirm("new passphrase")
	if err != nil {
		return err
	}

	newRecipient, initErr := targetKs.InitializeStagedWithPassphrase(passphraseBuf.Bytes())
	passphraseBuf.Destroy()
	if initErr != nil {
		return fmt.Errorf("create new keys: %w", initErr)
	}

	// Re-encrypt vault with new recipient (old keys still in place)
	ui.SubSection("Re-encrypting credentials")
	if err := mgr.ReEncryptVault(newRecipient); err != nil {
		// Clean up staged files on failure
		cleanupStagedFiles(paths.ConfigDir)
		return fmt.Errorf("re-encrypt vault: %w", err)
	}

	// Terminate old agent BEFORE verification - it has the OLD identity
	// which can't decrypt the vault anymore. This allows verification
	// to use the store's NEW identity directly.
	if agent.IsRunning() {
		ui.Info("Terminating old session...")
		if client, err := agent.Connect(); err == nil {
			_ = client.Lock()
			_ = client.Close()
		}
	}

	// CRITICAL: Verify we can decrypt the vault with the NEW key before committing
	// This ensures we don't delete old keys if re-encryption produced garbage
	ui.Info("Verifying credentials can be decrypted with new key...")
	newIdentity, err := targetKs.Identity()
	if err != nil {
		cleanupStagedFiles(paths.ConfigDir)
		return fmt.Errorf("get new identity for verification: %w", err)
	}
	if err := verifyVaultDecryption(paths, newIdentity); err != nil {
		cleanupStagedFiles(paths.ConfigDir)
		ui.Error("CRITICAL: Cannot decrypt credentials with new key!")
		ui.Warning("Vault was re-encrypted but verification failed")
		ui.Info("Rollback backup at: %s", AbbreviatePath(rollbackDir))
		fmt.Println()

		// Prompt to rollback automatically
		doRollback, _ := ui.Confirm("Restore from rollback backup now?", true)
		if doRollback {
			if rbErr := restoreFromRollback(rollbackDir, paths); rbErr != nil {
				ui.Error("Rollback failed: %v", rbErr)
				ui.Info("Manually restore with: nssh self rekey --rollback")
				return fmt.Errorf("verification failed, rollback failed: %w", rbErr)
			}
			ui.Success("Restored from rollback backup")
			ui.Info("Your credentials are accessible again")
			return fmt.Errorf("verification failed, rolled back successfully")
		}

		ui.Info("To restore later: nssh self rekey --rollback")
		return fmt.Errorf("verification failed: %w", err)
	}
	ui.Success("Verification passed")

	// Commit staged keys - only now is it safe to replace old keys
	if err := targetKs.CommitStaged(); err != nil {
		return fmt.Errorf("commit new keys: %w", err)
	}

	// Success! Clean up rollback backup and purge old vault backups
	// (old backups are encrypted with the now-deleted old key)
	ui.SubSection("Cleanup")
	if err := os.RemoveAll(rollbackDir); err != nil {
		ui.Warning("Could not remove rollback backup: %v", err)
	}
	purged := purgeOldVaultBackups(paths.BackupDir, rollbackDir)
	if purged > 0 {
		ui.Info("Purged %d old backup(s) encrypted with previous key", purged)
	}

	// Reset lockout state - the old lockout file's HMAC was signed with the old key
	// and won't verify with the new key, causing false "tampered" detection
	lockoutPath := filepath.Join(paths.StateDir, "lockout.json")
	if err := os.Remove(lockoutPath); err != nil && !os.IsNotExist(err) {
		ui.Warning("Could not reset lockout state: %v", err)
	}

	ui.Success("Keys rotated and credentials re-encrypted")
	ui.Info("Please unlock with your new passphrase to start a new session")

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// cleanupStagedFiles removes staged key files on failure.
func cleanupStagedFiles(configDir string) {
	_ = os.Remove(filepath.Join(configDir, "age.pub.staged"))
	_ = os.Remove(filepath.Join(configDir, "age.key.enc.staged"))
}

// verifyVaultDecryption tests that an identity can decrypt the vault.
// Used for early validation (before prompting for new credentials) and
// post-write verification (after re-encryption).
// Returns nil if vault doesn't exist or decryption succeeds.
func verifyVaultDecryption(paths *config.Paths, identity age.Identity) error {
	// Check if vault exists
	if _, err := os.Stat(paths.CredentialsFile); os.IsNotExist(err) {
		return nil // No vault to verify
	}

	x25519Identity, ok := identity.(*age.X25519Identity)
	if !ok {
		return fmt.Errorf("unexpected identity type: %T", identity)
	}

	mgr, err := vault.NewManager(
		vault.Provided(x25519Identity),
		vault.WithPaths(paths),
	)
	if err != nil {
		return fmt.Errorf("create verification manager: %w", err)
	}

	if _, err := mgr.ListContexts(); err != nil {
		return fmt.Errorf("decrypt vault: %w", err)
	}

	return nil
}

// reEncryptVaultSafely re-encrypts vault with verification.
// Verifies the new identity can decrypt before committing.
// Returns nil if vault doesn't exist (nothing to migrate).
func reEncryptVaultSafely(mgr *vault.Manager, newRecipient age.Recipient, newIdentity age.Identity) error {
	// Check if vault exists
	if _, err := os.Stat(mgr.CredentialPath()); os.IsNotExist(err) {
		return nil // No vault to re-encrypt
	}

	// Verification function - tests that we can decrypt the temp file with new identity
	verifyFn := func(tempPath string) error {
		ciphertext, err := os.ReadFile(tempPath)
		if err != nil {
			return fmt.Errorf("read temp file: %w", err)
		}

		r, err := age.Decrypt(bytes.NewReader(ciphertext), newIdentity)
		if err != nil {
			return fmt.Errorf("decrypt with new identity: %w", err)
		}

		// Read and parse to ensure it's valid JSON
		plaintext, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("read decrypted data: %w", err)
		}

		var data vault.VaultData
		if err := json.Unmarshal(plaintext, &data); err != nil {
			return fmt.Errorf("parse vault data: %w", err)
		}

		return nil
	}

	_, err := mgr.ReEncryptVaultWithVerify(newRecipient, verifyFn)
	return err
}

const (
	rollbackPrefixRekey      = "rekey-rollback-"
	rollbackPrefixModeSwitch = "mode-switch-rollback-"
	rollbackTimeFormat       = "20060102-150405"
)

type rollbackMetadata struct {
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	FromMode  string `json:"from_mode,omitempty"`
	ToMode    string `json:"to_mode,omitempty"`
}

// createRollbackBackup creates a backup of current keys and vault for manual recovery.
func createRollbackBackup(rollbackDir string, paths *config.Paths) error {
	if err := os.MkdirAll(rollbackDir, 0700); err != nil {
		return err
	}

	// Copy current key files
	filesToBackup := []struct {
		src, dst string
	}{
		{filepath.Join(paths.ConfigDir, "age.key.enc"), filepath.Join(rollbackDir, "age.key.enc")},
		{filepath.Join(paths.ConfigDir, "age.key.piv"), filepath.Join(rollbackDir, "age.key.piv")},
		{filepath.Join(paths.ConfigDir, "age.key"), filepath.Join(rollbackDir, "age.key")},
		{filepath.Join(paths.ConfigDir, "age.pub"), filepath.Join(rollbackDir, "age.pub")},
		{filepath.Join(paths.ConfigDir, "piv.json"), filepath.Join(rollbackDir, "piv.json")},
		{paths.CredentialsFile, filepath.Join(rollbackDir, "credentials.age")},
		{paths.ConfigFile, filepath.Join(rollbackDir, "config.toml")},
	}

	for _, f := range filesToBackup {
		if err := copyFile(f.src, f.dst); err != nil {
			// Non-fatal if file doesn't exist
			if !os.IsNotExist(err) {
				return fmt.Errorf("backup %s: %w", filepath.Base(f.src), err)
			}
		}
	}

	return nil
}

// writeRollbackMetadata stores rollback metadata (best-effort; non-fatal).
func writeRollbackMetadata(rollbackDir string, meta rollbackMetadata) {
	metaPath := filepath.Join(rollbackDir, "metadata.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		ui.Warning("Could not encode rollback metadata: %v", err)
		return
	}
	if err := os.WriteFile(metaPath, data, 0600); err != nil {
		ui.Warning("Could not write rollback metadata: %v", err)
	}
}

// readRollbackMetadata loads rollback metadata if present (returns zero value on failure).
func readRollbackMetadata(rollbackDir string) rollbackMetadata {
	metaPath := filepath.Join(rollbackDir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return rollbackMetadata{}
	}

	var meta rollbackMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return rollbackMetadata{}
	}
	return meta
}

// createModeSwitchBackup creates a rollback backup when changing security modes.
// Used by mode_switch.go (hardware builds only).
//
//nolint:unused // used in hardware builds (mode_switch.go)
func createModeSwitchBackup(paths *config.Paths, fromMode, toMode string) (string, error) {
	rollbackDir := filepath.Join(paths.BackupDir, fmt.Sprintf("%s%s", rollbackPrefixModeSwitch, time.Now().Format(rollbackTimeFormat)))

	if err := createRollbackBackup(rollbackDir, paths); err != nil {
		return "", err
	}

	writeRollbackMetadata(rollbackDir, rollbackMetadata{
		Kind:      "mode-switch",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		FromMode:  fromMode,
		ToMode:    toMode,
	})

	return rollbackDir, nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}

// purgeOldVaultBackups removes vault backups and old rollback directories.
// These are useless after rekey or mode switch since the old key is deleted.
func purgeOldVaultBackups(backupDir, currentRollbackDir string) int {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return 0
	}

	purged := 0
	for _, entry := range entries {
		path := filepath.Join(backupDir, entry.Name())
		name := entry.Name()

		// Skip the current rollback directory (will be cleaned up separately)
		if path == currentRollbackDir {
			continue
		}

		// Remove old rollback directories from previous failed/successful operations
		if entry.IsDir() && (strings.HasPrefix(name, rollbackPrefixRekey) || strings.HasPrefix(name, rollbackPrefixModeSwitch)) {
			if err := os.RemoveAll(path); err == nil {
				purged++
			}
			continue
		}

		// Remove old credential backups (files starting with credentials.age.)
		if !entry.IsDir() && strings.HasPrefix(name, "credentials.age.") {
			if err := os.Remove(path); err == nil {
				purged++
			}
		}
	}
	return purged
}

// runRollback restores from the most recent rekey rollback backup.
func runRollback() error {
	paths := config.DefaultPaths()

	ui.CommandStart("REKEY ROLLBACK")

	// Find most recent rollback backup
	rollbackDir, err := findLatestRollback(paths.BackupDir)
	if err != nil {
		return fmt.Errorf("find rollback backup: %w", err)
	}
	if rollbackDir == "" {
		ui.Error("No rollback backup found")
		return fmt.Errorf("no rollback backup found in %s", paths.BackupDir)
	}

	ui.Info("Found rollback backup: %s", AbbreviatePath(rollbackDir))

	if meta := readRollbackMetadata(rollbackDir); meta.Kind != "" {
		created := meta.CreatedAt
		if created == "" {
			created = "unknown time"
		}
		switch meta.Kind {
		case "mode-switch":
			from := meta.FromMode
			if from == "" {
				from = "unknown"
			}
			to := meta.ToMode
			if to == "" {
				to = "unknown"
			}
			ui.Info("Type: mode switch (%s -> %s) at %s", from, to, created)
		case "rekey":
			ui.Info("Type: rekey (created %s)", created)
		default:
			ui.Info("Type: %s (created %s)", meta.Kind, created)
		}
	}

	// Confirm
	doRestore, _ := ui.Confirm("Restore from this backup?", true)
	if !doRestore {
		ui.Abort("Rollback canceled")
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	// Restore
	if err := restoreFromRollback(rollbackDir, paths); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	ui.Success("Restored from rollback backup")
	ui.Info("Your credentials should now be accessible")
	ui.Info("Run 'nssh unlock' to verify")

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// findLatestRollback finds the most recent rekey-rollback directory.
func findLatestRollback(backupDir string) (string, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return "", err
	}

	var (
		latestPath string
		latestTime time.Time
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()

		var ts string
		switch {
		case strings.HasPrefix(name, rollbackPrefixRekey):
			ts = strings.TrimPrefix(name, rollbackPrefixRekey)
		case strings.HasPrefix(name, rollbackPrefixModeSwitch):
			ts = strings.TrimPrefix(name, rollbackPrefixModeSwitch)
		default:
			continue
		}

		t, err := time.Parse(rollbackTimeFormat, ts)
		if err != nil {
			continue
		}

		if t.After(latestTime) {
			latestTime = t
			latestPath = filepath.Join(backupDir, name)
		}
	}

	return latestPath, nil
}

// restoreFromRollback copies files from rollback backup to their original locations.
func restoreFromRollback(rollbackDir string, paths *config.Paths) error {
	if err := paths.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure paths: %w", err)
	}

	filesToRestore := []struct {
		src, dst string
	}{
		{filepath.Join(rollbackDir, "age.key.enc"), filepath.Join(paths.ConfigDir, "age.key.enc")},
		{filepath.Join(rollbackDir, "age.key.piv"), filepath.Join(paths.ConfigDir, "age.key.piv")},
		{filepath.Join(rollbackDir, "age.key"), filepath.Join(paths.ConfigDir, "age.key")},
		{filepath.Join(rollbackDir, "age.pub"), filepath.Join(paths.ConfigDir, "age.pub")},
		{filepath.Join(rollbackDir, "credentials.age"), paths.CredentialsFile},
		{filepath.Join(rollbackDir, "piv.json"), filepath.Join(paths.ConfigDir, "piv.json")},
		{filepath.Join(rollbackDir, "config.toml"), paths.ConfigFile},
	}

	for _, f := range filesToRestore {
		if err := copyFile(f.src, f.dst); err != nil {
			if os.IsNotExist(err) {
				continue // Skip if source doesn't exist
			}
			return fmt.Errorf("restore %s: %w", filepath.Base(f.src), err)
		}
	}

	return nil
}

// repairPublicKey regenerates the age.pub file from the current identity.
func repairPublicKey(mgr *vault.Manager, paths *config.Paths) error {
	ui.Info("Regenerating public key...")

	// Get recipient from current identity
	recipient, err := mgr.GetRecipient()
	if err != nil {
		return fmt.Errorf("get recipient: %w", err)
	}

	// Convert recipient to string representation
	// age.X25519Recipient implements Stringer via its String() method
	recipientStr := fmt.Sprintf("%s", recipient)

	// Write public key file
	pubKeyPath := filepath.Join(paths.ConfigDir, "age.pub")
	if err := os.WriteFile(pubKeyPath, []byte(recipientStr), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	ui.Success("Public key regenerated: %s", AbbreviatePath(pubKeyPath))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
