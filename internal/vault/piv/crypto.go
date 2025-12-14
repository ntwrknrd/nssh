//go:build hardware

package piv

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const eciesInfo = "nssh-piv-wrap-v1"

// ECDHSharedKey is the interface for ECDH operations.
// piv-go's PrivateKey implements this for hardware-backed ECDH.
type ECDHSharedKey interface {
	SharedKey(*ecdsa.PublicKey) ([]byte, error)
}

// EncryptWithPIV encrypts plaintext using ECIES with a P-256 public key.
// Output format: ephemeral_public_key (65 bytes) || nonce (12 bytes) || ciphertext || tag (16 bytes)
func EncryptWithPIV(pubKey *ecdsa.PublicKey, plaintext []byte) ([]byte, error) {
	// Generate ephemeral key pair
	ephPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// ECDH: shared = ephPriv * pubKey
	sharedX, _ := pubKey.Curve.ScalarMult(pubKey.X, pubKey.Y, ephPriv.D.Bytes())

	// KDF: derive AES-256 key from shared secret
	kdf := hkdf.New(sha256.New, sharedX.Bytes(), nil, []byte(eciesInfo))
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(kdf, aesKey); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	// Encrypt with AES-256-GCM
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Output: ephemeral public key || nonce || ciphertext
	ephPubBytes := elliptic.Marshal(elliptic.P256(), ephPriv.X, ephPriv.Y)
	result := make([]byte, 0, len(ephPubBytes)+len(nonce)+len(ciphertext))
	result = append(result, ephPubBytes...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// DecryptWithPIV decrypts data encrypted with EncryptWithPIV.
// The ECDH operation happens on the YubiKey - the private key never leaves hardware.
func DecryptWithPIV(priv ECDHSharedKey, data []byte) ([]byte, error) {
	// Parse ephemeral public key (65 bytes for uncompressed P-256)
	const ephPubSize = 65
	const nonceSize = 12
	const minCiphertextSize = 16 // GCM tag only

	if len(data) < ephPubSize+nonceSize+minCiphertextSize {
		return nil, fmt.Errorf("ciphertext too short: need at least %d bytes, got %d",
			ephPubSize+nonceSize+minCiphertextSize, len(data))
	}

	ephPubX, ephPubY := elliptic.Unmarshal(elliptic.P256(), data[:ephPubSize])
	if ephPubX == nil {
		return nil, fmt.Errorf("invalid ephemeral public key")
	}
	ephPub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: ephPubX, Y: ephPubY}

	// ECDH: shared = pivPriv * ephPub
	sharedSecret, err := priv.SharedKey(ephPub)
	if err != nil {
		return nil, fmt.Errorf("ECDH: %w", err)
	}

	// KDF: derive AES key (same as encryption)
	kdf := hkdf.New(sha256.New, sharedSecret, nil, []byte(eciesInfo))
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(kdf, aesKey); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	// Decrypt with AES-256-GCM
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := data[ephPubSize : ephPubSize+nonceSize]
	ciphertext := data[ephPubSize+nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// MarshalP256PublicKey serializes a P-256 public key to uncompressed format.
func MarshalP256PublicKey(pub *ecdsa.PublicKey) []byte {
	return elliptic.Marshal(elliptic.P256(), pub.X, pub.Y)
}

// UnmarshalP256PublicKey deserializes a P-256 public key from uncompressed format.
func UnmarshalP256PublicKey(data []byte) (*ecdsa.PublicKey, error) {
	x, y := elliptic.Unmarshal(elliptic.P256(), data)
	if x == nil {
		return nil, fmt.Errorf("invalid P-256 public key")
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

// Zeroize overwrites a byte slice with zeros.
func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
