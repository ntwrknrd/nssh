//go:build hardware

package self

import (
	"crypto/ecdsa"
	"fmt"
	"strconv"
	"time"

	"filippo.io/age"
	"github.com/go-piv/piv-go/v2/piv"
	"github.com/ntwrknrd/nssh/internal/agent"
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	vaultpiv "github.com/ntwrknrd/nssh/internal/vault/piv"
	"github.com/spf13/cobra"
)

// NewPivCmd creates the piv command group for managing YubiKeys.
func NewPivCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "piv",
		Short: "Manage enrolled YubiKeys",
		Long: `Manage YubiKeys enrolled for PIV hardware security.

Commands:
  enroll    Enroll an additional YubiKey
  list      List enrolled YubiKeys
  remove    Remove an enrolled YubiKey`,
	}

	cmd.AddCommand(newPivEnrollCmd())
	cmd.AddCommand(newPivListCmd())
	cmd.AddCommand(newPivRemoveCmd())

	return cmd
}

func newPivEnrollCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enroll",
		Short: "Enroll an additional YubiKey",
		Long: `Enroll a new YubiKey for PIV hardware security.

This requires unlocking with an existing enrolled YubiKey first,
then generates a key on the new YubiKey and encrypts the identity for it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPivEnroll()
		},
	}
}

func newPivListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List enrolled YubiKeys",
		Long:  `Display all YubiKeys enrolled for PIV hardware security.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPivList()
		},
	}
}

func newPivRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <serial>",
		Short: "Remove an enrolled YubiKey",
		Long: `Remove a YubiKey from the enrolled keys list.

At least one YubiKey must remain enrolled. Use 'nssh self piv list' to see
enrolled serial numbers.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serial, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid serial number: %s", args[0])
			}
			return runPivRemove(uint32(serial))
		},
	}
}

func runPivEnroll() error {
	paths := config.DefaultPaths()

	// Verify PIV mode from filesystem
	detectedMode, err := vault.DetectSecurityMode(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("detect security mode: %w", err)
	}
	if detectedMode != agent.ModePIV {
		return fmt.Errorf("PIV mode not configured - run 'nssh self init' with hardware mode first")
	}

	ui.CommandStart("ENROLL YUBIKEY")

	// Load existing keystore
	keystore, err := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("load PIV keystore: %w", err)
	}

	// Need to unlock first to get the age identity
	ui.Info("Unlocking with existing YubiKey to retrieve identity...")
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fmt.Errorf("initialize vault: %w", err)
	}
	if err := clisession.Unlock(mgr, false); err != nil {
		return fmt.Errorf("unlock vault: %w", err)
	}

	// Get the age identity from the connected agent
	client, err := agent.Connect()
	if err != nil {
		return fmt.Errorf("connect to agent: %w", err)
	}
	defer client.Close()

	// Find a connected enrolled key to decrypt the identity
	connectedSerials, err := agent.ListConnectedYubiKeys()
	if err != nil {
		return fmt.Errorf("list YubiKeys: %w", err)
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
		return fmt.Errorf("no enrolled YubiKey connected - insert an enrolled YubiKey first")
	}

	// Open the source YubiKey
	sourceYK, err := openYubiKeyBySerial(sourceKey.Serial)
	if err != nil {
		return fmt.Errorf("open YubiKey %d: %w", sourceKey.Serial, err)
	}
	defer sourceYK.Close()

	// Get private key to decrypt
	sourcePubKey, err := vaultpiv.UnmarshalP256PublicKey(sourceKey.PublicKey)
	if err != nil {
		return fmt.Errorf("parse source public key: %w", err)
	}

	slot, _ := piv.RetiredKeyManagementSlot(sourceKey.SlotKey)
	sourcePriv, err := sourceYK.PrivateKey(slot, sourcePubKey, piv.KeyAuth{
		PINPrompt: func() (string, error) {
			return ui.Password("YubiKey PIN")
		},
	})
	if err != nil {
		return fmt.Errorf("get private key: %w", err)
	}

	decrypter, ok := sourcePriv.(vaultpiv.ECDHSharedKey)
	if !ok {
		return fmt.Errorf("private key does not support ECDH")
	}

	// Decrypt the identity
	identityBytes, err := vaultpiv.DecryptWithPIV(decrypter, sourceKey.Identity)
	if err != nil {
		return fmt.Errorf("decrypt identity: %w", err)
	}
	defer zeroizeBytes(identityBytes)

	ageIdentity, err := age.ParseX25519Identity(string(identityBytes))
	if err != nil {
		return fmt.Errorf("parse age identity: %w", err)
	}

	ui.Success("Identity retrieved")
	fmt.Println()

	// Now enroll new key
	ui.Info("Insert the new YubiKey and press Enter...")
	fmt.Scanln()

	// Find new YubiKey
	cards, err := piv.Cards()
	if err != nil {
		return fmt.Errorf("list smart cards: %w", err)
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
		return fmt.Errorf("no new YubiKey detected - make sure to insert a different YubiKey")
	}
	defer newYK.Close()

	ui.Success("Found new YubiKey (serial: %d)", newSerial)
	ui.Warning("Touch your new YubiKey when it blinks...")

	// Generate key on new YubiKey
	slotKey := uint32(0x82)
	newSlot, _ := piv.RetiredKeyManagementSlot(slotKey)

	pivPub, err := generateKeyWithFallback(newYK, newSlot, piv.Key{
		Algorithm:   piv.AlgorithmEC256,
		PINPolicy:   piv.PINPolicyOnce,
		TouchPolicy: piv.TouchPolicyAlways,
	})
	if err != nil {
		return fmt.Errorf("generate key on new YubiKey: %w", err)
	}

	ecdsaPub, ok := pivPub.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("unexpected key type from YubiKey")
	}

	// Encrypt identity for new YubiKey
	encrypted, err := vaultpiv.EncryptWithPIV(ecdsaPub, []byte(ageIdentity.String()))
	if err != nil {
		return fmt.Errorf("encrypt identity for new YubiKey: %w", err)
	}

	// Add to keystore
	newKey := vaultpiv.PIVKey{
		Serial:    newSerial,
		SlotKey:   slotKey,
		PublicKey: vaultpiv.MarshalP256PublicKey(ecdsaPub),
		Label:     fmt.Sprintf("Backup %d", len(keystore.Keys)),
		Enrolled:  time.Now().UTC().Format(time.RFC3339),
		Identity:  encrypted,
	}

	keystore.Keys = append(keystore.Keys, newKey)

	if err := vaultpiv.SavePIVKeystore(paths.ConfigDir, keystore); err != nil {
		return fmt.Errorf("save keystore: %w", err)
	}

	ui.Success("YubiKey enrolled successfully")
	ui.Info("Total enrolled keys: %d", len(keystore.Keys))

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func runPivList() error {
	paths := config.DefaultPaths()

	ui.CommandStart("ENROLLED YUBIKEYS")

	keystore, err := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("load PIV keystore: %w", err)
	}

	// Get connected YubiKeys
	connectedSerials, _ := agent.ListConnectedYubiKeys()
	connectedMap := make(map[uint32]bool)
	for _, s := range connectedSerials {
		connectedMap[s] = true
	}

	fmt.Println()
	for _, k := range keystore.Keys {
		status := ""
		if connectedMap[k.Serial] {
			status = " (connected)"
		}
		ui.Info("%s: %d (slot 0x%02x)%s", k.Label, k.Serial, k.SlotKey, status)
		if k.Enrolled != "" {
			t, err := time.Parse(time.RFC3339, k.Enrolled)
			if err == nil {
				ui.Info("  Enrolled: %s", t.Format("2006-01-02 15:04"))
			}
		}
	}
	fmt.Println()

	ui.Info("Total: %d key(s)", len(keystore.Keys))

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func runPivRemove(serial uint32) error {
	paths := config.DefaultPaths()

	ui.CommandStart("REMOVE YUBIKEY")

	keystore, err := vaultpiv.LoadPIVKeystore(paths.ConfigDir)
	if err != nil {
		return fmt.Errorf("load PIV keystore: %w", err)
	}

	// Find the key
	var keyToRemove *vaultpiv.PIVKey
	for i := range keystore.Keys {
		if keystore.Keys[i].Serial == serial {
			keyToRemove = &keystore.Keys[i]
			break
		}
	}

	if keyToRemove == nil {
		return fmt.Errorf("YubiKey %d not found in keystore", serial)
	}

	if len(keystore.Keys) <= 1 {
		return fmt.Errorf("cannot remove the last enrolled YubiKey - enroll another key first")
	}

	ui.Warning("About to remove YubiKey: %s (serial: %d)", keyToRemove.Label, keyToRemove.Serial)
	confirmed, err := ui.Confirm("Continue?", false)
	if err != nil || !confirmed {
		ui.Abort("Removal cancelled")
		return nil
	}

	if err := vaultpiv.RemovePIVKey(paths.ConfigDir, serial); err != nil {
		return fmt.Errorf("remove key: %w", err)
	}

	ui.Success("YubiKey %d removed", serial)
	ui.Info("Remaining keys: %d", len(keystore.Keys)-1)

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// openYubiKeyBySerial opens a specific YubiKey by serial number.
func openYubiKeyBySerial(serial uint32) (*piv.YubiKey, error) {
	cards, err := piv.Cards()
	if err != nil {
		return nil, fmt.Errorf("list smart cards: %w", err)
	}

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
		if s == serial {
			return yk, nil
		}
		yk.Close()
	}

	return nil, fmt.Errorf("YubiKey with serial %d not found", serial)
}

// zeroizeBytes overwrites a byte slice with zeros.
func zeroizeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
