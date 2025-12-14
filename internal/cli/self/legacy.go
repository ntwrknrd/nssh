package self

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/ntwrknrd/nssh/internal/vault/software"
)

// Legacy migration support for users upgrading from Python-era nssh or
// earlier Go versions with unprotected age keys.
//
// This file can be removed once legacy migration is no longer needed.

// legacyKeyPaths returns all known legacy plaintext key locations.
// Checks nssh-specific locations, standard age location, and Python-era locations.
func legacyKeyPaths() []string {
	home := homeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	return []string{
		// Go legacy location
		filepath.Join(configHome, "nssh", "age.key"),
		// Standard age location
		filepath.Join(configHome, "age", "keys.txt"),
		// Python-era locations (age.txt variants)
		filepath.Join(home, ".ssh", "age.txt"),
		filepath.Join(configHome, "age", "age.txt"),
		filepath.Join(configHome, "nssh", "age.txt"),
	}
}

// legacyCredentialPaths returns all known legacy credential file locations.
func legacyCredentialPaths() []string {
	home := homeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}

	return []string{
		// Current Go location
		filepath.Join(dataHome, "nssh", "credentials.age"),
		// Python-era location
		filepath.Join(home, ".ssh", "nssh_credentials.age"),
	}
}

// findExistingCredentials checks if any credentials file exists.
// Returns the path if found, empty string otherwise.
func findExistingCredentials() string {
	for _, path := range legacyCredentialPaths() {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// findExistingLegacyKey checks if any legacy key file exists.
// Returns the path if found, empty string otherwise.
func findExistingLegacyKey() string {
	for _, path := range legacyKeyPaths() {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// checkOrphanedCredentials detects credentials that would become inaccessible.
// Returns an error if credentials exist but no matching key can be found.
func checkOrphanedCredentials() error {
	credsPath := findExistingCredentials()
	if credsPath == "" {
		return nil // No credentials, safe to proceed
	}

	// Credentials exist - this is a problem if we're about to create new keys
	ui.Error("Existing credentials found: %s", AbbreviatePath(credsPath))
	ui.Error("No encryption key found to decrypt these credentials!")
	fmt.Println()
	ui.Warning("Creating new keys would make your credentials permanently inaccessible.")
	fmt.Println()
	fmt.Println("To fix this, locate your age key file and copy it to one of these locations:")
	for _, keyPath := range legacyKeyPaths() {
		fmt.Printf("  - %s\n", AbbreviatePath(keyPath))
	}
	fmt.Println()
	fmt.Println("Common locations where your old key might be:")
	fmt.Println("  - A backup archive")
	fmt.Println("  - ~/.ssh/age.txt (Python-era nssh)")
	fmt.Println("  - ~/.config/age/age.txt")
	fmt.Println()
	ui.Info("Once the key is in place, run 'nssh self init' again.")
	ui.Info("Use 'nssh self reset' to start fresh (WARNING: credentials will be lost).")

	return fmt.Errorf("cannot initialize: credentials exist without matching key")
}

// migrateLegacyToSoftware migrates legacy unprotected keys to software mode.
func migrateLegacyToSoftware(paths *config.Paths, cfg *config.Config) error {
	ksCfg := software.Config{
		ConfigDir:           paths.ConfigDir,
		DataDir:             paths.DataDir,
		StateDir:            paths.StateDir,
		ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
		PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
	}

	ks, err := software.New(ksCfg)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	return migrateLegacyKey(paths, ks)
}

// loadLegacyIdentity loads an age identity from a plaintext key file.
// This is used during migration from legacy unprotected storage.
func loadLegacyIdentity(keyPath string) (*age.X25519Identity, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	identities, err := age.ParseIdentities(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("parse identities: %w", err)
	}

	if len(identities) == 0 {
		return nil, errors.New("no identities found in key file")
	}

	x, ok := identities[0].(*age.X25519Identity)
	if !ok {
		return nil, errors.New("not an X25519 identity")
	}

	return x, nil
}

// migrateLegacyKey migrates from legacy plaintext age key to protected storage.
// This includes re-encrypting the vault with the new identity.
//
// CRITICAL: The legacy key is ONLY deleted after successful verification that
// credentials can be decrypted with the new store. This prevents data loss.
func migrateLegacyKey(paths *config.Paths, ks software.Store) error {
	// Find legacy key from any known location
	legacyKeyPath := findExistingLegacyKey()
	if legacyKeyPath == "" {
		return fmt.Errorf("legacy key not found in any known location")
	}
	ui.Info("Found legacy key: %s", AbbreviatePath(legacyKeyPath))

	// Check if vault exists at current or legacy locations
	vaultPath := findExistingCredentials()
	vaultExists := vaultPath != ""

	// If vault is at legacy location, move it to current location first
	if vaultExists && vaultPath != paths.CredentialsFile {
		ui.Info("Moving credentials from legacy location...")
		if err := os.MkdirAll(filepath.Dir(paths.CredentialsFile), 0700); err != nil {
			return fmt.Errorf("create credentials dir: %w", err)
		}
		if err := os.Rename(vaultPath, paths.CredentialsFile); err != nil {
			// Try copy if rename fails (cross-device)
			data, readErr := os.ReadFile(vaultPath)
			if readErr != nil {
				return fmt.Errorf("read legacy credentials: %w", readErr)
			}
			if writeErr := os.WriteFile(paths.CredentialsFile, data, 0600); writeErr != nil {
				return fmt.Errorf("write credentials: %w", writeErr)
			}
			// Keep old file until migration is verified
		}
		ui.Success("Moved credentials to: %s", AbbreviatePath(paths.CredentialsFile))
	}

	if !vaultExists {
		// No vault to migrate - just initialize new store
		ui.Info("No existing vault to migrate")
		if initErr := promptAndInitialize(ks); initErr != nil {
			return initErr
		}
		// Safe to delete legacy key since there's no vault depending on it
		deleteLegacyKey(legacyKeyPath, paths.ConfigDir)
		return nil
	}

	// Load legacy identity from plaintext key file
	legacyIdentity, err := loadLegacyIdentity(legacyKeyPath)
	if err != nil {
		return fmt.Errorf("load legacy key: %w", err)
	}

	// Vault exists - need to re-encrypt it
	legacyMgr, err := vault.NewManager(
		vault.Provided(legacyIdentity),
		vault.WithPaths(paths),
		vault.WithMaxBackups(5),
	)
	if err != nil {
		return fmt.Errorf("create manager with legacy key: %w", err)
	}

	// Validate legacy key can decrypt credentials BEFORE asking for new passphrase
	ui.Info("Validating legacy key can decrypt credentials...")
	ui.Info("  Key file: %s", legacyKeyPath)
	ui.Info("  Credentials: %s", paths.CredentialsFile)
	ui.Info("  Public key: %s", legacyIdentity.Recipient().String())

	if err := verifyVaultDecryption(paths, legacyIdentity); err != nil {
		ui.Error("Legacy key cannot decrypt credentials")
		fmt.Println()
		showKeyMismatchDiagnostics(legacyKeyPath, paths.CredentialsFile)
		return fmt.Errorf("key mismatch: credentials were encrypted with a different key")
	}
	ui.Success("Legacy key validated")

	// Initialize new store (prompt for passphrase)
	if initErr := promptAndInitialize(ks); initErr != nil {
		return initErr
	}

	// Get new recipient from newly initialized store
	newRecipient, recipErr := ks.Recipient()
	if recipErr != nil {
		return fmt.Errorf("get new recipient: %w", recipErr)
	}

	// Re-encrypt vault with new recipient, verifying before committing
	// This ensures the original file is never modified unless we can decrypt the new version
	ui.Info("Re-encrypting vault with new identity...")

	// Get identity for verification
	identity, identErr := ks.Identity()
	if identErr != nil {
		return fmt.Errorf("get identity for verification: %w", identErr)
	}

	if reErr := reEncryptVaultSafely(legacyMgr, newRecipient, identity); reErr != nil {
		ui.Error("Failed to re-encrypt vault: %v", reErr)
		ui.Warning("Original credentials unchanged - legacy key preserved at: %s", legacyKeyPath)
		return fmt.Errorf("re-encrypt vault: %w", reErr)
	}
	ui.Success("Vault re-encrypted and verified")

	// ONLY NOW is it safe to delete the legacy key
	deleteLegacyKey(legacyKeyPath, paths.ConfigDir)

	return nil
}

// deleteLegacyKey securely deletes the legacy key file if it's in the nssh config dir.
func deleteLegacyKey(legacyKeyPath, configDir string) {
	if strings.HasPrefix(legacyKeyPath, configDir) {
		if delErr := secureDeleteLegacyKey(legacyKeyPath); delErr != nil {
			ui.Warning("Could not securely delete legacy key: %v", delErr)
			ui.Warning("Please manually delete: %s", legacyKeyPath)
		}
	} else {
		ui.Warning("Legacy key at %s was not deleted (not in nssh config dir)", legacyKeyPath)
	}
}

// showKeyMismatchDiagnostics displays helpful information when a legacy key
// cannot decrypt the credentials file.
func showKeyMismatchDiagnostics(keyPath, credentialsPath string) {
	ui.SubSection("Diagnostics")

	// Try to show the public key for the legacy key file
	keyData, err := os.ReadFile(keyPath)
	if err == nil {
		identities, parseErr := age.ParseIdentities(strings.NewReader(string(keyData)))
		if parseErr == nil && len(identities) > 0 {
			if x, ok := identities[0].(*age.X25519Identity); ok {
				fmt.Printf("  Legacy key public key:\n")
				fmt.Printf("    %s\n\n", x.Recipient().String())
			}
		}
	}

	// Show the credentials file header (contains recipient info)
	credData, err := os.ReadFile(credentialsPath)
	if err == nil && len(credData) > 0 {
		lines := strings.SplitN(string(credData), "\n", 5)
		fmt.Printf("  Credentials file header:\n")
		for i, line := range lines {
			if i >= 3 {
				break
			}
			fmt.Printf("    %s\n", line)
		}
		fmt.Println()
	}

	// Show other locations to check for keys
	fmt.Printf("  The credentials were encrypted with a different key.\n")
	fmt.Printf("  Check these locations for other age key files:\n\n")
	for _, path := range legacyKeyPaths() {
		if _, statErr := os.Stat(path); statErr == nil {
			fmt.Printf("    [exists] %s\n", AbbreviatePath(path))
		} else {
			fmt.Printf("    [      ] %s\n", AbbreviatePath(path))
		}
	}
	fmt.Println()

	fmt.Printf("  To find all age key files on your system:\n")
	fmt.Printf("    find ~ -name '*.txt' -path '*age*' 2>/dev/null\n")
	fmt.Printf("    find ~ -name 'age*' -type f 2>/dev/null\n")
	fmt.Println()

	fmt.Printf("  Once you find the correct key, copy it to:\n")
	fmt.Printf("    ~/.config/nssh/age.key\n")
	fmt.Printf("  Then run 'nssh self init' again.\n")
}

// secureDeleteLegacyKey overwrites the legacy key file with random data before deletion.
func secureDeleteLegacyKey(path string) error {
	// Read file size
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Open for writing
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}

	// Overwrite with random data
	randomData := make([]byte, info.Size())
	if _, err := rand.Read(randomData); err != nil {
		// Fall back to zeros if random fails
		for i := range randomData {
			randomData[i] = 0
		}
	}

	if _, err := f.WriteAt(randomData, 0); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()

	return os.Remove(path)
}
