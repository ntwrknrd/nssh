//go:build hardware

package self

import (
	"crypto/ecdsa"
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
	"github.com/ntwrknrd/nssh/internal/vault/software"
)

// deleteSoftwareKeystore removes the software keystore files.
func deleteSoftwareKeystore(configDir string) {
	_ = os.Remove(filepath.Join(configDir, "age.key.enc"))
}

// deleteHardwareKeystore removes the hardware keystore files.
func deleteHardwareKeystore(configDir string) {
	_ = os.Remove(filepath.Join(configDir, "piv.json"))
}

// runSoftwareToHardware switches from software (passphrase) to hardware (YubiKey PIV) mode.
// Preserves all credentials by re-encrypting the vault with the new hardware-backed key.
func runSoftwareToHardware(paths *config.Paths, cfg *config.Config) error {
	ui.Info("Switching from software to hardware mode")
	fmt.Println()

	// Step 1: Stop agent if running (holds old identity)
	if agent.IsRunning() {
		ui.Info("Stopping agent...")
		if client, err := agent.Connect(); err == nil {
			_ = client.Lock()
			_ = client.Close()
		}
	}

	// Step 2: Unlock software store and get identity
	ui.Info("[1/7] Unlocking current credentials...")
	ksCfg := software.Config{
		ConfigDir:           paths.ConfigDir,
		DataDir:             paths.DataDir,
		StateDir:            paths.StateDir,
		ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
		PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
	}
	softwareKs, err := software.New(ksCfg)
	if err != nil {
		return fmt.Errorf("create keystore: %w", err)
	}

	// Prompt for passphrase and unlock
	unlockPassBuf, passErr := ui.PasswordSecure("Enter passphrase to unlock credentials")
	if passErr != nil {
		return passErr
	}
	unlockErr := softwareKs.UnlockWithPassphrase(unlockPassBuf.Bytes())
	unlockPassBuf.Destroy()
	if unlockErr != nil {
		return fmt.Errorf("unlock current credentials: %w", unlockErr)
	}
	currentIdentity, err := softwareKs.Identity()
	if err != nil {
		return fmt.Errorf("get current identity: %w", err)
	}
	ui.Success("Current credentials unlocked")
	fmt.Println()

	// Step 3: Early validation - verify current identity can decrypt vault
	ui.Info("[2/7] Verifying vault access...")
	if err := verifyVaultDecryption(paths, currentIdentity); err != nil {
		return fmt.Errorf("current identity cannot decrypt vault: %w", err)
	}
	ui.Success("Vault access verified")
	fmt.Println()

	// Step 4: Create mode switch backup
	ui.Info("[3/7] Creating rollback backup...")
	rollbackDir, err := createModeSwitchBackup(paths, agent.ModeSoftware, agent.ModePIV)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	ui.Success("Rollback backup created")
	fmt.Println()

	// Step 5: Initialize PIV mode (generate key on YubiKey, create staged piv.json)
	ui.Info("[4/7] Setting up YubiKey...")
	pivKeystore, newIdentity, err := initPIVModeStaged(paths)
	if err != nil {
		return err
	}
	ui.Success("YubiKey configured")
	fmt.Println()

	// Step 6: Re-encrypt vault with new identity
	ui.Info("[5/7] Re-encrypting vault...")
	vaultMgr, err := vault.NewManager(
		vault.Software(softwareKs),
		vault.WithPaths(paths),
		vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
	)
	if err != nil {
		cleanupStagedPIVFiles(paths.ConfigDir)
		return fmt.Errorf("create vault manager: %w", err)
	}
	if err := vaultMgr.ReEncryptVault(newIdentity.Recipient()); err != nil {
		cleanupStagedPIVFiles(paths.ConfigDir)
		return fmt.Errorf("re-encrypt vault: %w", err)
	}
	ui.Success("Vault re-encrypted")
	fmt.Println()

	// Step 7: Full round-trip verification through YubiKey
	ui.Info("[6/7] Verifying full round-trip through YubiKey...")
	ui.Warning("Touch your YubiKey to verify...")

	// Find a connected enrolled key for verification
	connectedSerials, err := agent.ListConnectedYubiKeys()
	if err != nil {
		cleanupStagedPIVFiles(paths.ConfigDir)
		return fmt.Errorf("list YubiKeys: %w", err)
	}
	var verifySerial uint32
	for _, ks := range pivKeystore.Keys {
		for _, s := range connectedSerials {
			if ks.Serial == s {
				verifySerial = s
				break
			}
		}
		if verifySerial != 0 {
			break
		}
	}
	if verifySerial == 0 {
		cleanupStagedPIVFiles(paths.ConfigDir)
		return fmt.Errorf("no enrolled YubiKey connected for verification")
	}

	if err := verifyModeSwitchPIVRoundTrip(paths, cfg, verifySerial); err != nil {
		cleanupStagedPIVFiles(paths.ConfigDir)
		ui.Error("CRITICAL: Full round-trip verification failed!")
		ui.Info("Rollback backup at: %s", AbbreviatePath(rollbackDir))
		fmt.Println()

		doRollback, _ := ui.Confirm("Restore from rollback backup now?", true)
		if doRollback {
			if rbErr := restoreFromRollback(rollbackDir, paths); rbErr != nil {
				ui.Error("Rollback failed: %v", rbErr)
				ui.Info("Manually restore with: nssh self rekey --rollback")
				return fmt.Errorf("verification failed, rollback failed: %w", rbErr)
			}
			ui.Success("Restored from rollback backup")
			return fmt.Errorf("verification failed, rolled back successfully")
		}
		ui.Info("To restore later: nssh self rekey --rollback")
		return fmt.Errorf("verification failed: %w", err)
	}
	ui.Success("Full round-trip verification passed")
	fmt.Println()

	// Step 8: Commit and cleanup
	ui.Info("[7/7] Finalizing mode switch...")

	// Commit staged PIV files
	if err := commitStagedPIV(paths.ConfigDir, newIdentity); err != nil {
		return fmt.Errorf("commit PIV files: %w", err)
	}

	// Delete software keystore
	deleteSoftwareKeystore(paths.ConfigDir)

	// Cleanup rollback and old backups
	_ = os.RemoveAll(rollbackDir)
	purged := purgeOldVaultBackups(paths.BackupDir, rollbackDir)
	if purged > 0 {
		ui.Info("Purged %d old backup(s)", purged)
	}

	// Reset lockout state
	lockoutPath := filepath.Join(paths.StateDir, "lockout.json")
	_ = os.Remove(lockoutPath)

	ui.Success("Mode switch complete: software -> hardware")
	ui.Info("Please unlock with your YubiKey to start a new session")

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// runHardwareToSoftware switches from hardware (YubiKey PIV) to software (passphrase) mode.
// Preserves all credentials by re-encrypting the vault with a new passphrase-protected key.
func runHardwareToSoftware(paths *config.Paths, cfg *config.Config) error {
	ui.Info("Switching from hardware to software mode")
	fmt.Println()

	// Step 1: Stop agent if running (holds old identity)
	if agent.IsRunning() {
		ui.Info("Stopping agent...")
		if client, err := agent.Connect(); err == nil {
			_ = client.Lock()
			_ = client.Close()
		}
	}

	// Step 2: Unlock with YubiKey and get identity
	ui.Info("[1/6] Unlocking with YubiKey...")
	keystoreData, err := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("load PIV keystore: %w", err)
	}
	currentIdentity, err := decryptIdentityWithYubiKey(keystoreData, paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("decrypt identity: %w", err)
	}
	defer zeroizeIdentity(currentIdentity)
	ui.Success("Identity retrieved")
	fmt.Println()

	// Step 3: Early validation - verify current identity can decrypt vault
	ui.Info("[2/6] Verifying vault access...")
	if err := verifyVaultDecryption(paths, currentIdentity); err != nil {
		return fmt.Errorf("current identity cannot decrypt vault: %w", err)
	}
	ui.Success("Vault access verified")
	fmt.Println()

	// Step 4: Create mode switch backup
	ui.Info("[3/6] Creating rollback backup...")
	rollbackDir, err := createModeSwitchBackup(paths, agent.ModePIV, agent.ModeSoftware)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	ui.Success("Rollback backup created")
	fmt.Println()

	// Step 5: Initialize software store with staged keys
	ui.Info("[4/6] Creating new passphrase-protected key...")
	ui.Info("You'll be prompted to create a new passphrase")
	fmt.Println()

	ksCfg := software.Config{
		ConfigDir:           paths.ConfigDir,
		DataDir:             paths.DataDir,
		StateDir:            paths.StateDir,
		ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
		PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
	}
	targetKs, err := software.New(ksCfg)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	passphraseBuf, err := ui.PasswordSecureWithConfirm("passphrase")
	if err != nil {
		return err
	}

	newRecipient, initErr := targetKs.InitializeStagedWithPassphrase(passphraseBuf.Bytes())
	passphraseBuf.Destroy()
	if initErr != nil {
		return fmt.Errorf("create new keys: %w", initErr)
	}
	newIdentity, err := targetKs.Identity()
	if err != nil {
		cleanupStagedFiles(paths.ConfigDir)
		return fmt.Errorf("get new identity: %w", err)
	}
	ui.Success("New key created")
	fmt.Println()

	// Step 6: Re-encrypt vault with new identity
	ui.Info("[5/6] Re-encrypting vault...")
	vaultMgr, err := vault.NewManager(
		vault.Provided(currentIdentity),
		vault.WithPaths(paths),
		vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
	)
	if err != nil {
		cleanupStagedFiles(paths.ConfigDir)
		return fmt.Errorf("create vault manager: %w", err)
	}
	if err := vaultMgr.ReEncryptVault(newRecipient); err != nil {
		cleanupStagedFiles(paths.ConfigDir)
		return fmt.Errorf("re-encrypt vault: %w", err)
	}
	ui.Success("Vault re-encrypted")
	fmt.Println()

	// Step 7: Verify with new software key
	ui.Info("Verifying new key can decrypt vault...")
	if err := verifyVaultDecryption(paths, newIdentity); err != nil {
		cleanupStagedFiles(paths.ConfigDir)
		ui.Error("CRITICAL: Verification failed!")
		ui.Info("Rollback backup at: %s", AbbreviatePath(rollbackDir))
		fmt.Println()

		doRollback, _ := ui.Confirm("Restore from rollback backup now?", true)
		if doRollback {
			if rbErr := restoreFromRollback(rollbackDir, paths); rbErr != nil {
				ui.Error("Rollback failed: %v", rbErr)
				ui.Info("Manually restore with: nssh self rekey --rollback")
				return fmt.Errorf("verification failed, rollback failed: %w", rbErr)
			}
			ui.Success("Restored from rollback backup")
			return fmt.Errorf("verification failed, rolled back successfully")
		}
		ui.Info("To restore later: nssh self rekey --rollback")
		return fmt.Errorf("verification failed: %w", err)
	}
	ui.Success("Verification passed")
	fmt.Println()

	// Step 8: Commit and cleanup
	ui.Info("[6/6] Finalizing mode switch...")

	// Commit staged software keys
	if err := targetKs.CommitStaged(); err != nil {
		return fmt.Errorf("commit new keys: %w", err)
	}

	// Delete hardware keystore (YubiKey keys remain but are orphaned - harmless)
	deleteHardwareKeystore(paths.ConfigDir)

	// Cleanup rollback and old backups
	_ = os.RemoveAll(rollbackDir)
	purged := purgeOldVaultBackups(paths.BackupDir, rollbackDir)
	if purged > 0 {
		ui.Info("Purged %d old backup(s)", purged)
	}

	// Reset lockout state
	lockoutPath := filepath.Join(paths.StateDir, "lockout.json")
	_ = os.Remove(lockoutPath)

	ui.Success("Mode switch complete: hardware -> software")
	ui.Info("Please unlock with your new passphrase to start a new session")

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// initPIVModeStaged initializes PIV mode with staged files.
// Returns the keystore and new identity on success.
func initPIVModeStaged(paths *config.Paths) (*vaultpiv.PIVKeystore, *age.X25519Identity, error) {
	// Detect YubiKey
	cards, err := piv.Cards()
	if err != nil {
		return nil, nil, fmt.Errorf("list smart cards: %w", err)
	}
	if len(cards) == 0 {
		return nil, nil, fmt.Errorf("no YubiKey detected - insert your YubiKey and try again")
	}

	// Open first available YubiKey
	yk, err := piv.Open(cards[0])
	if err != nil {
		return nil, nil, fmt.Errorf("open YubiKey: %w", err)
	}
	defer yk.Close()

	serial, _ := yk.Serial()
	ui.Success("Found: YubiKey (serial: %d)", serial)

	// Generate key on YubiKey
	ui.Warning("Touch your YubiKey when it blinks...")

	slotKey := uint32(0x82) // Retired Key Management 1
	slot, ok := piv.RetiredKeyManagementSlot(slotKey)
	if !ok {
		return nil, nil, fmt.Errorf("invalid PIV slot: 0x%02x", slotKey)
	}

	pinPolicy := piv.PINPolicyOnce
	touchPolicy := piv.TouchPolicyAlways

	pivPub, err := generateKeyWithFallback(yk, slot, piv.Key{
		Algorithm:   piv.AlgorithmEC256,
		PINPolicy:   pinPolicy,
		TouchPolicy: touchPolicy,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("generate key on YubiKey: %w", err)
	}
	ecdsaPub, ok := pivPub.(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected key type from YubiKey: %T", pivPub)
	}

	// Generate age identity
	ageIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, nil, fmt.Errorf("generate age identity: %w", err)
	}

	// Encrypt age identity with YubiKey's P-256 key
	encrypted, err := vaultpiv.EncryptWithPIV(ecdsaPub, []byte(ageIdentity.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt identity: %w", err)
	}

	// Create keystore
	pivKeystore := &vaultpiv.PIVKeystore{
		Version: 2,
		Keys: []vaultpiv.PIVKey{{
			Serial:    serial,
			SlotKey:   slotKey,
			PublicKey: vaultpiv.MarshalP256PublicKey(ecdsaPub),
			Label:     "Primary",
			Enrolled:  time.Now().UTC().Format(time.RFC3339),
			Identity:  encrypted,
		}},
	}

	// Save staged keystore
	if err := savePIVKeystoreStaged(paths.ConfigDir, pivKeystore); err != nil {
		return nil, nil, fmt.Errorf("write staged keystore: %w", err)
	}

	return pivKeystore, ageIdentity, nil
}

// verifyModeSwitchPIVRoundTrip performs full round-trip verification for mode switch.
func verifyModeSwitchPIVRoundTrip(paths *config.Paths, cfg *config.Config, serial uint32) error {
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
	return verifyVaultDecryption(paths, identity)
}
