//go:build !hardware

package piv

import (
	"crypto/ecdsa"
)

// ECDHSharedKey is the interface for ECDH operations.
// piv-go's PrivateKey implements this for hardware-backed ECDH.
type ECDHSharedKey interface {
	SharedKey(*ecdsa.PublicKey) ([]byte, error)
}

// EncryptWithPIV is a stub that returns an error on non-hardware builds.
func EncryptWithPIV(_ *ecdsa.PublicKey, _ []byte) ([]byte, error) {
	return nil, ErrPIVNotAvailable
}

// DecryptWithPIV is a stub that returns an error on non-hardware builds.
func DecryptWithPIV(_ ECDHSharedKey, _ []byte) ([]byte, error) {
	return nil, ErrPIVNotAvailable
}

// MarshalP256PublicKey is a stub that returns nil on non-hardware builds.
func MarshalP256PublicKey(_ *ecdsa.PublicKey) []byte {
	return nil
}

// UnmarshalP256PublicKey is a stub that returns an error on non-hardware builds.
func UnmarshalP256PublicKey(_ []byte) (*ecdsa.PublicKey, error) {
	return nil, ErrPIVNotAvailable
}

// Zeroize overwrites a byte slice with zeros.
// This is safe to use on non-hardware builds.
func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
