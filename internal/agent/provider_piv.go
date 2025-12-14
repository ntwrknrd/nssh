//go:build hardware

package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
	"github.com/go-piv/piv-go/v2/piv"

	vaultpiv "github.com/ntwrknrd/nssh/internal/vault/piv"
)

// PIVProvider implements Provider using a YubiKey PIV slot.
// The age X25519 identity is encrypted with the YubiKey's P-256 key.
type PIVProvider struct {
	identity  *age.X25519Identity
	configDir string
	activeKey *vaultpiv.PIVKey // The key used for this session
}

// PIVInitConfig holds initialization options.
type PIVInitConfig struct {
	ConfigDir   string
	Slot        piv.Slot
	PINPolicy   piv.PINPolicy
	TouchPolicy piv.TouchPolicy
}

// NewPIVProvider creates a PIV provider, decrypting the age identity from YubiKey.
// This prompts for PIN (if configured) and touch.
// Supports both v1 (single key) and v2 (multi-key) formats.
func NewPIVProvider(configDir string, pinPrompt func() (string, error)) (*PIVProvider, error) {
	keystore, err := vaultpiv.LoadPIVKeystore(configDir)
	if err != nil {
		return nil, err
	}

	if len(keystore.Keys) == 0 {
		return nil, errors.New("no YubiKeys enrolled - run 'nssh self init' with hardware mode first")
	}

	// Find connected YubiKeys
	connectedSerials, err := ListConnectedYubiKeys()
	if err != nil {
		return nil, err
	}
	if len(connectedSerials) == 0 {
		return nil, errors.New("no YubiKey detected - insert your YubiKey and try again")
	}

	// Find first enrolled key that's connected
	var matchedKey *vaultpiv.PIVKey
	for i := range keystore.Keys {
		for _, serial := range connectedSerials {
			if keystore.Keys[i].Serial == serial {
				matchedKey = &keystore.Keys[i]
				break
			}
		}
		if matchedKey != nil {
			break
		}
	}

	if matchedKey == nil {
		// Build helpful error message
		var enrolledSerials []uint32
		for _, k := range keystore.Keys {
			enrolledSerials = append(enrolledSerials, k.Serial)
		}
		return nil, fmt.Errorf("no enrolled YubiKey connected (enrolled: %v, connected: %v)",
			enrolledSerials, connectedSerials)
	}

	// Open the matched YubiKey
	yk, err := openYubiKey(matchedKey.Serial)
	if err != nil {
		return nil, err
	}
	defer yk.Close()

	// Parse stored public key
	pubKey, err := vaultpiv.UnmarshalP256PublicKey(matchedKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse stored public key: %w", err)
	}

	// Get private key handle (prompts for PIN if required)
	slot, ok := piv.RetiredKeyManagementSlot(matchedKey.SlotKey)
	if !ok {
		return nil, fmt.Errorf("invalid PIV slot key: 0x%02x", matchedKey.SlotKey)
	}
	priv, err := yk.PrivateKey(slot, pubKey, piv.KeyAuth{
		PINPrompt: pinPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("get private key from YubiKey: %w", err)
	}

	// Cast to ECDHSharedKey interface for decryption
	decrypter, ok := priv.(vaultpiv.ECDHSharedKey)
	if !ok {
		return nil, fmt.Errorf("private key does not support ECDH")
	}

	// Decrypt identity from the matched key's embedded blob
	identityStr, err := vaultpiv.DecryptWithPIV(decrypter, matchedKey.Identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt identity with YubiKey: %w", err)
	}
	defer vaultpiv.Zeroize(identityStr)

	identity, err := age.ParseX25519Identity(string(identityStr))
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}

	return &PIVProvider{
		identity:  identity,
		configDir: configDir,
		activeKey: matchedKey,
	}, nil
}

// Mode returns "piv".
func (p *PIVProvider) Mode() string {
	return ModePIV
}

// Decrypt decrypts age-encrypted ciphertext using the cached X25519 identity.
func (p *PIVProvider) Decrypt(ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), p.identity)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// Recipient returns the age public key string.
func (p *PIVProvider) Recipient() string {
	return p.identity.Recipient().String()
}

// Close zeroizes the identity.
func (p *PIVProvider) Close() error {
	p.identity = nil
	return nil
}

var _ Provider = (*PIVProvider)(nil)

// ListConnectedYubiKeys returns serial numbers of all connected YubiKeys.
func ListConnectedYubiKeys() ([]uint32, error) {
	cards, err := piv.Cards()
	if err != nil {
		return nil, fmt.Errorf("list smart cards: %w", err)
	}

	var serials []uint32
	for _, card := range cards {
		yk, err := piv.Open(card)
		if err != nil {
			continue
		}
		serial, err := yk.Serial()
		yk.Close()
		if err != nil {
			continue
		}
		serials = append(serials, serial)
	}
	return serials, nil
}

// openYubiKey finds and opens a YubiKey with the given serial number.
// If serial is 0, opens the first available YubiKey.
func openYubiKey(serial uint32) (*piv.YubiKey, error) {
	cards, err := piv.Cards()
	if err != nil {
		return nil, fmt.Errorf("list smart cards: %w", err)
	}
	if len(cards) == 0 {
		return nil, errors.New("no YubiKey detected - insert your YubiKey and try again")
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
		if serial == 0 || s == serial {
			return yk, nil
		}
		yk.Close()
	}

	return nil, fmt.Errorf("YubiKey with serial %d not found", serial)
}
