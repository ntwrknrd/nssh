//go:build hardware

package self

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Default management keys for different algorithms.
// YubiKey Manager often sets up PIN-protected management keys,
// but we also support the factory defaults.
var (
	// DefaultManagementKey3DES is the factory default 3DES management key (24 bytes).
	DefaultManagementKey3DES = []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	}
	// DefaultManagementKeyAES256 is a default AES-256 management key (32 bytes).
	// YubiKey 5.7+ defaults to AES. This matches the pattern of the 3DES default.
	DefaultManagementKeyAES256 = []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	}
)

// initPIV initializes credentials with YubiKey PIV protection.
// Note: setupCredentialProtection handles the "already initialized" check,
// so this function assumes it's being called for a new install or mode switch.
func initPIV(paths *config.Paths, cfg *config.Config) error {
	// Check existing initialization (v2 format)
	pivPath := filepath.Join(paths.ConfigDir, "piv.json")
	pivExists := false
	if _, err := os.Stat(pivPath); err == nil {
		pivExists = true
	}

	// Check if we're switching from software mode (vault re-encryption needed)
	softwareKeyPath := filepath.Join(paths.ConfigDir, "age.key.enc")
	softwareKeyExists := false
	if _, err := os.Stat(softwareKeyPath); err == nil {
		softwareKeyExists = true
	}

	// Detect partial PIV state from a previous failed attempt:
	// - piv.json exists (PIV keystore was saved)
	// - age.key.enc also exists (software key wasn't removed yet)
	//
	// If partial state is detected, try to complete the previous attempt.
	partialPIVState := pivExists && softwareKeyExists

	if partialPIVState {
		ui.Warning("Detected partial PIV setup from previous attempt")
		ui.Info("Attempting to complete previous setup...")
		fmt.Println()

		// Try to complete the previous PIV setup by verifying the existing keystore
		if err := tryCompletePIVSetup(paths, cfg, softwareKeyPath); err != nil {
			ui.Warning("Could not complete previous setup: %v", err)
			ui.Info("Starting fresh PIV initialization...")
			// Remove partial piv.json to start fresh
			_ = os.Remove(pivPath)
			pivExists = false
		} else {
			// Success! Previous setup completed
			return nil
		}
	}

	switchingFromSoftware := false
	var softwareKs software.Store

	if softwareKeyExists {
		switchingFromSoftware = true
	}

	if switchingFromSoftware {
		ui.Info("Switching from software mode to hardware mode")
		ui.Info("You'll need to enter your current passphrase to migrate credentials")
		fmt.Println()

		// Create keystore to unlock current credentials
		ksCfg := software.Config{
			ConfigDir:           paths.ConfigDir,
			DataDir:             paths.DataDir,
			StateDir:            paths.StateDir,
			ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
			PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
		}
		var ksErr error
		softwareKs, ksErr = software.New(ksCfg)
		if ksErr != nil {
			return fmt.Errorf("create keystore: %w", ksErr)
		}

		// Prompt for passphrase and unlock to verify
		passphraseBuf, passErr := ui.PasswordSecure("Enter passphrase to unlock credentials")
		if passErr != nil {
			return passErr
		}
		unlockErr := softwareKs.UnlockWithPassphrase(passphraseBuf.Bytes())
		passphraseBuf.Destroy()
		if unlockErr != nil {
			return fmt.Errorf("unlock current credentials: %w", unlockErr)
		}
		ui.Success("Current credentials unlocked")
		fmt.Println()
	}

	// Check if vault exists (need to re-encrypt if switching modes)
	vaultExists := false
	if _, err := os.Stat(paths.CredentialsFile); err == nil {
		vaultExists = true
	}

	ui.Info("Hardware security requires a YubiKey 4 or 5 series.")
	confirmed, err := ui.Confirm("Continue?", true)
	if err != nil || !confirmed {
		return fmt.Errorf("hardware setup cancelled")
	}

	fmt.Println()
	ui.SubSection("YubiKey PIV Setup")

	// Determine step count based on whether we need to migrate and verify
	totalSteps := 4 // detect, generate, backup, save
	if switchingFromSoftware && vaultExists {
		totalSteps = 6 // + re-encrypt, verify
	} else if vaultExists {
		totalSteps = 5 // + verify (no re-encrypt needed for fresh PIV init with existing vault)
	}
	step := 1

	// Step 1: Detect YubiKey
	ui.Info("[%d/%d] Detecting YubiKeys...", step, totalSteps)
	step++
	cards, err := piv.Cards()
	if err != nil {
		return fmt.Errorf("list smart cards: %w", err)
	}
	if len(cards) == 0 {
		return fmt.Errorf("no YubiKey detected - insert your YubiKey and try again")
	}

	// Open first available YubiKey
	yk, err := piv.Open(cards[0])
	if err != nil {
		return fmt.Errorf("open YubiKey: %w", err)
	}
	defer yk.Close()

	serial, _ := yk.Serial()
	ui.Success("Found: YubiKey (serial: %d)", serial)

	// Step 2: Generate key and encrypt identity
	ui.Info("[%d/%d] Generating key on YubiKey...", step, totalSteps)
	step++
	ui.Warning("Touch your YubiKey when it blinks...")

	slotKey := uint32(0x82) // Retired Key Management 1
	slot, ok := piv.RetiredKeyManagementSlot(slotKey)
	if !ok {
		return fmt.Errorf("invalid PIV slot: 0x%02x", slotKey)
	}

	pinPolicy := piv.PINPolicyOnce
	touchPolicy := piv.TouchPolicyAlways

	pivPub, err := generateKeyWithFallback(yk, slot, piv.Key{
		Algorithm:   piv.AlgorithmEC256,
		PINPolicy:   pinPolicy,
		TouchPolicy: touchPolicy,
	})
	if err != nil {
		return fmt.Errorf("generate key on YubiKey: %w", err)
	}
	ecdsaPub, ok := pivPub.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("unexpected key type from YubiKey: %T", pivPub)
	}

	// Generate age identity
	ageIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate age identity: %w", err)
	}

	// Encrypt age identity with YubiKey's P-256 key
	encrypted, err := vaultpiv.EncryptWithPIV(ecdsaPub, []byte(ageIdentity.String()))
	if err != nil {
		return fmt.Errorf("encrypt identity: %w", err)
	}

	ui.Success("Primary YubiKey configured (serial: %d)", serial)

	// Create keystore with primary key
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

	// Step 3: Offer backup enrollment
	ui.Info("[%d/%d] Backup enrollment", step, totalSteps)
	step++
	fmt.Println()
	ui.Warning("If you lose your only YubiKey, you will lose access to ALL stored credentials.")
	fmt.Println()

	enrollBackup, err := ui.Confirm("Enroll a backup YubiKey now?", true)
	if err != nil {
		enrollBackup = false
	}

	if enrollBackup {
		if err := enrollBackupKeys(pivKeystore, ageIdentity, serial); err != nil {
			ui.Warning("Backup enrollment failed: %v", err)
			ui.Info("You can enroll a backup later with: nssh self piv enroll")
		}
	} else {
		ui.Info("You can enroll a backup later with: nssh self piv enroll")
	}

	// IMPORTANT: Save piv.json BEFORE re-encrypting vault.
	// This ensures we never lose the PIV identity if re-encryption succeeds
	// but a later step fails.
	ui.Info("[%d/%d] Saving PIV keystore...", step, totalSteps)
	step++

	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Save piv.json first - this is the critical data
	if err := vaultpiv.SavePIVKeystore(paths.ConfigDir, pivKeystore); err != nil {
		return fmt.Errorf("write PIV keystore: %w", err)
	}

	// Save age.pub (needed for vault operations)
	pubPath := filepath.Join(paths.ConfigDir, "age.pub")
	if err := os.WriteFile(pubPath, []byte(ageIdentity.Recipient().String()), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}
	ui.Success("PIV keystore saved")

	// Step 5 (if migrating): Re-encrypt vault with new identity
	// Now safe to re-encrypt because piv.json exists for recovery
	if switchingFromSoftware && vaultExists {
		ui.Info("[%d/%d] Re-encrypting vault with new identity...", step, totalSteps)
		step++

		// Create vault manager with current (software) keystore
		vaultMgr, vaultErr := vault.NewManager(
			vault.Software(softwareKs),
			vault.WithPaths(paths),
			vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
		)
		if vaultErr != nil {
			return fmt.Errorf("create vault manager: %w", vaultErr)
		}

		// Re-encrypt with new PIV identity
		if reErr := vaultMgr.ReEncryptVault(ageIdentity.Recipient()); reErr != nil {
			return fmt.Errorf("re-encrypt vault: %w", reErr)
		}
		ui.Success("Vault re-encrypted with new identity")
	}

	// Full round-trip verification: decrypt identity via YubiKey, then decrypt vault
	if vaultExists {
		ui.Info("[%d/%d] Verifying full round-trip through YubiKey...", step, totalSteps)
		step++
		ui.Warning("Touch your YubiKey to verify...")

		// Load the keystore we just saved
		savedKeystore, loadErr := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
		if loadErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("load saved keystore: %w", loadErr)
		}

		// Find ANY connected enrolled YubiKey for verification
		// (user may have swapped YubiKeys during backup enrollment)
		connectedSerials, connErr := agent.ListConnectedYubiKeys()
		if connErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("list connected YubiKeys: %w", connErr)
		}

		var verifyKey *vaultpiv.PIVKey
		for i := range savedKeystore.Keys {
			for _, s := range connectedSerials {
				if savedKeystore.Keys[i].Serial == s {
					verifyKey = &savedKeystore.Keys[i]
					break
				}
			}
			if verifyKey != nil {
				break
			}
		}
		if verifyKey == nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("no enrolled YubiKey connected for verification")
		}

		// Open the connected YubiKey
		verifyYK, openErr := openYubiKeyBySerial(verifyKey.Serial)
		if openErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("open YubiKey for verification: %w", openErr)
		}
		defer verifyYK.Close()

		// Decrypt identity using the YubiKey (full round-trip)
		pubKey, parseErr := vaultpiv.UnmarshalP256PublicKey(verifyKey.PublicKey)
		if parseErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("parse public key: %w", parseErr)
		}

		verifySlot, _ := piv.RetiredKeyManagementSlot(verifyKey.SlotKey)
		priv, privErr := verifyYK.PrivateKey(verifySlot, pubKey, piv.KeyAuth{
			PINPrompt: func() (string, error) {
				return ui.Password("YubiKey PIN (verification)")
			},
		})
		if privErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("get private key for verification: %w", privErr)
		}

		decrypter, ok := priv.(vaultpiv.ECDHSharedKey)
		if !ok {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("private key does not support ECDH")
		}

		decryptedIdentityBytes, decErr := vaultpiv.DecryptWithPIV(decrypter, verifyKey.Identity)
		if decErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("decrypt identity from YubiKey: %w", decErr)
		}

		decryptedIdentity, parseIdErr := age.ParseX25519Identity(string(decryptedIdentityBytes))
		// Clear sensitive data
		for i := range decryptedIdentityBytes {
			decryptedIdentityBytes[i] = 0
		}
		if parseIdErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("parse decrypted identity: %w", parseIdErr)
		}

		// Verify vault can be decrypted with the round-trip identity
		verifyMgr, verifyErr := vault.NewManager(
			vault.Provided(decryptedIdentity),
			vault.WithPaths(paths),
			vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
		)
		if verifyErr != nil {
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("create verification manager: %w", verifyErr)
		}
		if _, listErr := verifyMgr.ListContexts(); listErr != nil {
			ui.Error("CRITICAL: Full round-trip verification failed!")
			if switchingFromSoftware {
				ui.Warning("Old software key preserved at: %s", softwareKeyPath)
			}
			return fmt.Errorf("vault decryption verification failed: %w", listErr)
		}
		ui.Success("Full round-trip verification passed")
	}

	// Remove old key files now that migration is verified
	if switchingFromSoftware {
		if err := os.Remove(softwareKeyPath); err != nil && !os.IsNotExist(err) {
			ui.Warning("Could not remove old software key: %v", err)
		} else {
			ui.Info("Old software key removed")
		}
	}

	// Remove any old v1 format file if it exists
	_ = os.Remove(filepath.Join(paths.ConfigDir, "age.key.piv"))

	fmt.Println()
	ui.Success("PIV setup complete (%d YubiKey(s) enrolled)", len(pivKeystore.Keys))
	for _, k := range pivKeystore.Keys {
		ui.Info("  %s: %d (slot 0x%02x)", k.Label, k.Serial, k.SlotKey)
	}

	return nil
}

// enrollBackupKeys prompts to enroll additional YubiKeys.
func enrollBackupKeys(keystore *vaultpiv.PIVKeystore, ageIdentity *age.X25519Identity, primarySerial uint32) error {
	backupNum := 1
	for {
		fmt.Println()
		ui.Info("Insert backup YubiKey and press Enter (or 'q' to finish)...")
		var input string
		fmt.Scanln(&input)
		if strings.ToLower(strings.TrimSpace(input)) == "q" {
			break
		}

		// Find new YubiKey
		cards, err := piv.Cards()
		if err != nil {
			ui.Warning("Could not list smart cards: %v", err)
			continue
		}

		var newYK *piv.YubiKey
		var newSerial uint32
		for _, card := range cards {
			yk, err := piv.Open(card)
			if err != nil {
				continue
			}
			s, err := yk.Serial()
			if err != nil {
				yk.Close()
				continue
			}
			// Skip already enrolled keys
			alreadyEnrolled := false
			for _, k := range keystore.Keys {
				if k.Serial == s {
					alreadyEnrolled = true
					break
				}
			}
			if alreadyEnrolled {
				yk.Close()
				continue
			}
			newYK = yk
			newSerial = s
			break
		}

		if newYK == nil {
			ui.Warning("No new YubiKey detected. Make sure to insert a different YubiKey.")
			continue
		}
		defer newYK.Close()

		ui.Success("Found new YubiKey (serial: %d)", newSerial)
		ui.Warning("Touch your YubiKey when it blinks...")

		// Generate key on backup YubiKey
		slotKey := uint32(0x82)
		slot, _ := piv.RetiredKeyManagementSlot(slotKey)

		pivPub, err := generateKeyWithFallback(newYK, slot, piv.Key{
			Algorithm:   piv.AlgorithmEC256,
			PINPolicy:   piv.PINPolicyOnce,
			TouchPolicy: piv.TouchPolicyAlways,
		})
		if err != nil {
			ui.Warning("Could not generate key on YubiKey: %v", err)
			newYK.Close()
			continue
		}

		ecdsaPub, ok := pivPub.(*ecdsa.PublicKey)
		if !ok {
			ui.Warning("Unexpected key type from YubiKey")
			newYK.Close()
			continue
		}

		// Encrypt identity for this YubiKey
		encrypted, err := vaultpiv.EncryptWithPIV(ecdsaPub, []byte(ageIdentity.String()))
		if err != nil {
			ui.Warning("Could not encrypt identity: %v", err)
			newYK.Close()
			continue
		}

		// Add to keystore
		keystore.Keys = append(keystore.Keys, vaultpiv.PIVKey{
			Serial:    newSerial,
			SlotKey:   slotKey,
			PublicKey: vaultpiv.MarshalP256PublicKey(ecdsaPub),
			Label:     fmt.Sprintf("Backup %d", backupNum),
			Enrolled:  time.Now().UTC().Format(time.RFC3339),
			Identity:  encrypted,
		})
		backupNum++

		ui.Success("Backup YubiKey enrolled (%d keys total)", len(keystore.Keys))
		newYK.Close()

		// Ask if they want to enroll more
		more, err := ui.Confirm("Enroll another backup?", false)
		if err != nil || !more {
			break
		}
	}

	return nil
}

// getManagementKey retrieves the management key for YubiKey operations.
// It first tries to get a PIN-protected management key (common with YubiKey Manager),
// then falls back to default keys.
func getManagementKey(yk *piv.YubiKey) ([]byte, error) {
	// First, try to get PIN-protected management key
	// This is the recommended setup from YubiKey Manager
	pin, err := ui.Password("YubiKey PIN (for management key)")
	if err != nil {
		return nil, fmt.Errorf("read PIN: %w", err)
	}

	meta, err := yk.Metadata(pin)
	if err == nil && meta.ManagementKey != nil {
		ui.Success("Using PIN-protected management key")
		return *meta.ManagementKey, nil
	}

	// PIN-protected key not available
	// Return nil to signal caller should try defaults
	ui.Info("No PIN-protected management key found, will try defaults...")
	return nil, nil
}

// generateKeyWithFallback tries to generate a key using different management keys.
// It first tries PIN-protected, then 3DES default, then AES-256 default.
func generateKeyWithFallback(yk *piv.YubiKey, slot piv.Slot, opts piv.Key) (interface{}, error) {
	// Try to get PIN-protected management key
	mgmtKey, err := getManagementKey(yk)
	if err != nil {
		return nil, err
	}

	// If we got a PIN-protected key, use it
	if mgmtKey != nil {
		return yk.GenerateKey(mgmtKey, slot, opts)
	}

	// Try default keys in order
	keysToTry := []struct {
		name string
		key  []byte
	}{
		{"3DES default", DefaultManagementKey3DES},
		{"AES-256 default", DefaultManagementKeyAES256},
	}

	var lastErr error
	for _, k := range keysToTry {
		pub, err := yk.GenerateKey(k.key, slot, opts)
		if err == nil {
			ui.Success("Using %s management key", k.name)
			return pub, nil
		}
		lastErr = err

		// Check if error indicates wrong key size - try next
		if strings.Contains(err.Error(), "invalid management key length") ||
			strings.Contains(err.Error(), "expected 32") ||
			strings.Contains(err.Error(), "expected 24") {
			continue
		}

		// Other error (wrong key, auth failed, etc.) - don't try more defaults
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("generate key failed (tried default management keys): %w\n"+
			"If you've changed your management key, set up PIN-protected management key in YubiKey Manager", lastErr)
	}

	return nil, fmt.Errorf("no valid management key found")
}

// tryCompletePIVSetup attempts to complete a partial PIV setup from a previous failed attempt.
// It verifies that the existing piv.json can decrypt the vault, then removes the software key.
func tryCompletePIVSetup(paths *config.Paths, cfg *config.Config, softwareKeyPath string) error {
	// Load existing keystore
	keystore, err := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("load keystore: %w", err)
	}

	// Find a connected enrolled YubiKey
	connectedSerials, err := agent.ListConnectedYubiKeys()
	if err != nil {
		return fmt.Errorf("list YubiKeys: %w", err)
	}

	var matchedKey *vaultpiv.PIVKey
	for i := range keystore.Keys {
		for _, s := range connectedSerials {
			if keystore.Keys[i].Serial == s {
				matchedKey = &keystore.Keys[i]
				break
			}
		}
		if matchedKey != nil {
			break
		}
	}

	if matchedKey == nil {
		return fmt.Errorf("no enrolled YubiKey connected")
	}

	ui.Info("Found enrolled YubiKey (serial: %d)", matchedKey.Serial)
	ui.Warning("Touch your YubiKey to verify previous setup...")

	// Open the YubiKey
	yk, err := openYubiKeyBySerial(matchedKey.Serial)
	if err != nil {
		return fmt.Errorf("open YubiKey: %w", err)
	}
	defer yk.Close()

	// Get private key handle
	pubKey, err := vaultpiv.UnmarshalP256PublicKey(matchedKey.PublicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	slot, _ := piv.RetiredKeyManagementSlot(matchedKey.SlotKey)
	priv, err := yk.PrivateKey(slot, pubKey, piv.KeyAuth{
		PINPrompt: func() (string, error) {
			return ui.Password("YubiKey PIN")
		},
	})
	if err != nil {
		return fmt.Errorf("get private key: %w", err)
	}

	decrypter, ok := priv.(vaultpiv.ECDHSharedKey)
	if !ok {
		return fmt.Errorf("private key does not support ECDH")
	}

	// Decrypt the identity
	identityBytes, err := vaultpiv.DecryptWithPIV(decrypter, matchedKey.Identity)
	if err != nil {
		return fmt.Errorf("decrypt identity: %w", err)
	}

	identity, err := age.ParseX25519Identity(string(identityBytes))
	// Clear sensitive data
	for i := range identityBytes {
		identityBytes[i] = 0
	}
	if err != nil {
		return fmt.Errorf("parse identity: %w", err)
	}

	// Try to decrypt the vault with this identity
	mgr, err := vault.NewManager(
		vault.Provided(identity),
		vault.WithPaths(paths),
		vault.WithMaxBackups(cfg.Logging.Audit.MaxBackupFiles),
	)
	if err != nil {
		return fmt.Errorf("create vault manager: %w", err)
	}

	if _, err := mgr.ListContexts(); err != nil {
		return fmt.Errorf("vault not accessible with PIV identity: %w", err)
	}

	ui.Success("Previous PIV setup verified successfully")

	// Complete the setup by removing the software key
	if err := os.Remove(softwareKeyPath); err != nil && !os.IsNotExist(err) {
		ui.Warning("Could not remove old software key: %v", err)
	} else {
		ui.Info("Old software key removed")
	}

	fmt.Println()
	ui.Success("PIV setup complete (recovered from previous attempt)")
	for _, k := range keystore.Keys {
		ui.Info("  %s: %d (slot 0x%02x)", k.Label, k.Serial, k.SlotKey)
	}

	return nil
}
