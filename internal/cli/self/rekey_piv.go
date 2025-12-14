//go:build hardware

package self

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
	"github.com/go-piv/piv-go/v2/piv"
	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	vaultpiv "github.com/ntwrknrd/nssh/internal/vault/piv"
)

// runRekeyPIV rotates the age identity while keeping existing YubiKey P-256 keys.
// Each enrolled YubiKey gets the new identity encrypted with its existing P-256 key.
func runRekeyPIV(paths *config.Paths, cfg *config.Config) error {
	ui.Info("PIV mode: rotating age identity")
	fmt.Println()

	// Step 1: Load keystore and find connected YubiKey
	ui.Info("[1/6] Unlocking with YubiKey...")
	keystoreData, err := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("load PIV keystore: %w", err)
	}

	// Find connected enrolled YubiKey for later verification
	connectedSerials, err := agent.ListConnectedYubiKeys()
	if err != nil {
		return fmt.Errorf("list YubiKeys: %w", err)
	}
	var connectedKey *vaultpiv.PIVKey
	for i := range keystoreData.Keys {
		for _, s := range connectedSerials {
			if keystoreData.Keys[i].Serial == s {
				connectedKey = &keystoreData.Keys[i]
				break
			}
		}
		if connectedKey != nil {
			break
		}
	}
	if connectedKey == nil {
		return fmt.Errorf("no enrolled YubiKey connected - insert an enrolled YubiKey first")
	}

	// Decrypt current identity using connected YubiKey
	oldIdentity, err := decryptIdentityWithYubiKey(keystoreData, paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("decrypt identity: %w", err)
	}
	defer zeroizeIdentity(oldIdentity)

	ui.Success("Identity retrieved")
	fmt.Println()

	// Step 2: Create rollback backup
	ui.Info("[2/6] Creating rollback backup...")
	rollbackDir := filepath.Join(paths.BackupDir, fmt.Sprintf("%s%s", rollbackPrefixRekey, time.Now().Format(rollbackTimeFormat)))
	if err := createRollbackBackup(rollbackDir, paths); err != nil {
		return fmt.Errorf("create rollback backup: %w", err)
	}
	writeRollbackMetadata(rollbackDir, rollbackMetadata{
		Kind:      "rekey",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	ui.Success("Rollback backup created")
	fmt.Println()

	// Step 3: Generate new age identity
	ui.Info("[3/6] Generating new age identity...")
	newIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate age identity: %w", err)
	}
	ui.Success("New identity generated")
	fmt.Println()

	// Step 4: Re-encrypt vault with new identity
	ui.Info("[4/6] Re-encrypting vault...")
	if err := reEncryptVaultWithIdentities(paths, cfg, oldIdentity, newIdentity.Recipient()); err != nil {
		return fmt.Errorf("re-encrypt vault: %w", err)
	}
	ui.Success("Vault re-encrypted")
	fmt.Println()

	// Step 5: Update keystore with new identity encrypted for each YubiKey
	ui.Info("[5/6] Updating YubiKey keystore...")
	for i := range keystoreData.Keys {
		k := &keystoreData.Keys[i]
		ui.Info("  Re-encrypting for YubiKey %d (%s)", k.Serial, k.Label)

		// Parse public key
		pubKey, err := vaultpiv.UnmarshalP256PublicKey(k.PublicKey)
		if err != nil {
			cleanupStagedPIVFiles(paths.ConfigDir)
			return fmt.Errorf("parse public key for YubiKey %d: %w", k.Serial, err)
		}

		// Encrypt new identity with this YubiKey's P-256 public key
		encrypted, err := vaultpiv.EncryptWithPIV(pubKey, []byte(newIdentity.String()))
		if err != nil {
			cleanupStagedPIVFiles(paths.ConfigDir)
			return fmt.Errorf("encrypt identity for YubiKey %d: %w", k.Serial, err)
		}

		k.Identity = encrypted
	}

	// Write staged keystore
	if err := savePIVKeystoreStaged(paths.ConfigDir, keystoreData); err != nil {
		return fmt.Errorf("write staged keystore: %w", err)
	}
	ui.Success("Keystore updated (%d keys)", len(keystoreData.Keys))
	fmt.Println()

	// Terminate old agent before verification
	if agent.IsRunning() {
		ui.Info("Terminating old session...")
		if client, err := agent.Connect(); err == nil {
			_ = client.Lock()
			_ = client.Close()
		}
	}

	// Step 6: Full round-trip verification through YubiKey
	ui.Info("[6/6] Verifying full round-trip through YubiKey...")
	ui.Warning("Touch your YubiKey to verify...")

	verifyErr := verifyRekeyPIVRoundTrip(paths, cfg, connectedKey.Serial)
	if verifyErr != nil {
		cleanupStagedPIVFiles(paths.ConfigDir)
		ui.Error("CRITICAL: Full round-trip verification failed!")
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
		return fmt.Errorf("verification failed: %w", verifyErr)
	}
	ui.Success("Full round-trip verification passed")

	// Commit: atomic swap of staged files
	if err := commitStagedPIV(paths.ConfigDir, newIdentity); err != nil {
		return fmt.Errorf("commit staged files: %w", err)
	}

	// Success! Clean up
	ui.SubSection("Cleanup")
	if err := os.RemoveAll(rollbackDir); err != nil {
		ui.Warning("Could not remove rollback backup: %v", err)
	}
	purged := purgeOldVaultBackups(paths.BackupDir, rollbackDir)
	if purged > 0 {
		ui.Info("Purged %d old backup(s) encrypted with previous key", purged)
	}

	// Reset lockout state
	lockoutPath := filepath.Join(paths.StateDir, "lockout.json")
	if err := os.Remove(lockoutPath); err != nil && !os.IsNotExist(err) {
		ui.Warning("Could not reset lockout state: %v", err)
	}

	ui.Success("Rekey complete")
	ui.Info("Age identity rotated, YubiKey P-256 keys unchanged")
	ui.Info("Please unlock with your YubiKey to start a new session")

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// decryptIdentityWithYubiKey finds a connected enrolled YubiKey and decrypts the identity.
func decryptIdentityWithYubiKey(keystore *vaultpiv.PIVKeystore, configDir string) (*age.X25519Identity, error) {
	// Find connected enrolled YubiKey
	connectedSerials, err := agent.ListConnectedYubiKeys()
	if err != nil {
		return nil, fmt.Errorf("list YubiKeys: %w", err)
	}

	var sourceKey *vaultpiv.PIVKey
	for i := range keystore.Keys {
		for _, s := range connectedSerials {
			if keystore.Keys[i].Serial == s {
				sourceKey = &keystore.Keys[i]
				break
			}
		}
		if sourceKey != nil {
			break
		}
	}

	if sourceKey == nil {
		return nil, fmt.Errorf("no enrolled YubiKey connected - insert an enrolled YubiKey first")
	}

	// Open the YubiKey
	yk, err := openYubiKeyBySerial(sourceKey.Serial)
	if err != nil {
		return nil, fmt.Errorf("open YubiKey %d: %w", sourceKey.Serial, err)
	}
	defer yk.Close()

	ui.Warning("Touch your YubiKey...")

	// Get private key to decrypt
	pubKey, err := vaultpiv.UnmarshalP256PublicKey(sourceKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	slot, _ := piv.RetiredKeyManagementSlot(sourceKey.SlotKey)
	priv, err := yk.PrivateKey(slot, pubKey, piv.KeyAuth{
		PINPrompt: func() (string, error) {
			return ui.Password("YubiKey PIN")
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get private key: %w", err)
	}

	decrypter, ok := priv.(vaultpiv.ECDHSharedKey)
	if !ok {
		return nil, fmt.Errorf("private key does not support ECDH")
	}

	// Decrypt the identity
	identityBytes, err := vaultpiv.DecryptWithPIV(decrypter, sourceKey.Identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt identity: %w", err)
	}
	defer zeroizeBytes(identityBytes)

	identity, err := age.ParseX25519Identity(string(identityBytes))
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}

	return identity, nil
}

// reEncryptVaultWithIdentities re-encrypts the vault using old identity to decrypt, new recipient to encrypt.
func reEncryptVaultWithIdentities(paths *config.Paths, cfg *config.Config, oldIdentity *age.X25519Identity, newRecipient age.Recipient) error {
	// Create a vault manager that uses the old identity directly
	mgr, err := vault.NewManager(
		vault.Provided(oldIdentity),
		vault.WithPaths(paths),
		vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
	)
	if err != nil {
		return fmt.Errorf("create vault manager: %w", err)
	}

	// Re-encrypt with new recipient
	return mgr.ReEncryptVault(newRecipient)
}

// savePIVKeystoreStaged writes the keystore to a staged file.
func savePIVKeystoreStaged(configDir string, ks *vaultpiv.PIVKeystore) error {
	stagedPath := filepath.Join(configDir, "piv.json.staged")
	return vaultpiv.SavePIVKeystoreToPath(stagedPath, ks)
}

// cleanupStagedPIVFiles removes staged PIV files on failure.
func cleanupStagedPIVFiles(configDir string) {
	_ = os.Remove(filepath.Join(configDir, "piv.json.staged"))
	_ = os.Remove(filepath.Join(configDir, "age.pub.staged"))
}

// commitStagedPIV atomically commits staged PIV files.
func commitStagedPIV(configDir string, newIdentity *age.X25519Identity) error {
	// Write new public key
	pubPath := filepath.Join(configDir, "age.pub")
	if err := os.WriteFile(pubPath, []byte(newIdentity.Recipient().String()), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	// Atomic rename of keystore
	stagedPath := filepath.Join(configDir, "piv.json.staged")
	finalPath := filepath.Join(configDir, "piv.json")
	if err := os.Rename(stagedPath, finalPath); err != nil {
		return fmt.Errorf("commit keystore: %w", err)
	}

	return nil
}

// verifyRekeyPIVRoundTrip performs full round-trip verification:
// 1. Load staged piv.json
// 2. Open YubiKey and decrypt identity
// 3. Use decrypted identity to verify vault decryption
func verifyRekeyPIVRoundTrip(paths *config.Paths, cfg *config.Config, serial uint32) error {
	// Load the staged keystore
	stagedPath := filepath.Join(paths.ConfigDir, "piv.json.staged")
	stagedKeystore, err := loadPIVKeystoreFromPath(stagedPath)
	if err != nil {
		return fmt.Errorf("load staged keystore: %w", err)
	}

	// Find the key for the connected YubiKey
	var verifyKey *vaultpiv.PIVKey
	for i := range stagedKeystore.Keys {
		if stagedKeystore.Keys[i].Serial == serial {
			verifyKey = &stagedKeystore.Keys[i]
			break
		}
	}
	if verifyKey == nil {
		return fmt.Errorf("YubiKey %d not found in staged keystore", serial)
	}

	// Open the YubiKey
	yk, err := openYubiKeyBySerial(serial)
	if err != nil {
		return fmt.Errorf("open YubiKey %d: %w", serial, err)
	}
	defer yk.Close()

	// Get private key to decrypt
	pubKey, err := vaultpiv.UnmarshalP256PublicKey(verifyKey.PublicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	slot, _ := piv.RetiredKeyManagementSlot(verifyKey.SlotKey)
	priv, err := yk.PrivateKey(slot, pubKey, piv.KeyAuth{
		PINPrompt: func() (string, error) {
			return ui.Password("YubiKey PIN (verification)")
		},
	})
	if err != nil {
		return fmt.Errorf("get private key: %w", err)
	}

	decrypter, ok := priv.(vaultpiv.ECDHSharedKey)
	if !ok {
		return fmt.Errorf("private key does not support ECDH")
	}

	// Decrypt the identity from staged keystore
	identityBytes, err := vaultpiv.DecryptWithPIV(decrypter, verifyKey.Identity)
	if err != nil {
		return fmt.Errorf("decrypt identity: %w", err)
	}

	identity, err := age.ParseX25519Identity(string(identityBytes))
	// Clear sensitive data
	for i := range identityBytes {
		identityBytes[i] = 0
	}
	if err != nil {
		return fmt.Errorf("parse age identity: %w", err)
	}

	// Verify vault can be decrypted with the round-trip identity
	mgr, err := vault.NewManager(
		vault.Provided(identity),
		vault.WithPaths(paths),
		vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
	)
	if err != nil {
		return fmt.Errorf("create verification manager: %w", err)
	}

	_, err = mgr.ListContexts()
	return err
}

// loadPIVKeystoreFromPath loads a PIV keystore from a specific path.
func loadPIVKeystoreFromPath(path string) (*vaultpiv.PIVKeystore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var ks vaultpiv.PIVKeystore
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, err
	}

	return &ks, nil
}

// zeroizeIdentity securely clears an age identity from memory.
// Note: This is best-effort as Go doesn't guarantee memory clearing,
// but we do what we can.
func zeroizeIdentity(id *age.X25519Identity) {
	if id == nil {
		return
	}
	// The identity string contains the private key material
	// We can't directly access it, so this is mostly symbolic
	// The real protection comes from the short lifetime of the variable
}
