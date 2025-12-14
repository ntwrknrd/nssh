//go:build !hardware

// Package piv provides PIV keystore persistence and ECIES crypto helpers.
// This is the stub implementation for non-hardware builds.
package piv

import "errors"

// ErrPIVNotAvailable is returned when PIV operations are attempted on non-hardware builds.
var ErrPIVNotAvailable = errors.New("PIV support not available (compile with -tags hardware)")

// PIVKeystore is the v2 multi-key format for piv.json.
type PIVKeystore struct {
	Version int      `json:"version"` // Always 2
	Keys    []PIVKey `json:"keys"`
}

// PIVKey represents a single enrolled YubiKey.
type PIVKey struct {
	Serial    uint32 `json:"serial"`
	SlotKey   uint32 `json:"slot_key"`
	PublicKey []byte `json:"public_key"`
	Label     string `json:"label,omitempty"`
	Enrolled  string `json:"enrolled,omitempty"` // RFC3339 timestamp
	Identity  []byte `json:"identity"`           // ECIES-encrypted age identity
}

// PIVMeta is the v1 single-key format (for migration).
type PIVMeta struct {
	Serial    uint32 `json:"serial"`
	SlotKey   uint32 `json:"slot_key"`
	PublicKey []byte `json:"public_key"`
}

// LoadPIVKeystore is a stub that returns an error on non-hardware builds.
func LoadPIVKeystore(_ string) (*PIVKeystore, error) {
	return nil, ErrPIVNotAvailable
}

// SavePIVKeystore is a stub that returns an error on non-hardware builds.
func SavePIVKeystore(_ string, _ *PIVKeystore) error {
	return ErrPIVNotAvailable
}

// SavePIVKeystoreToPath is a stub that returns an error on non-hardware builds.
func SavePIVKeystoreToPath(_ string, _ *PIVKeystore) error {
	return ErrPIVNotAvailable
}

// AddPIVKey is a stub that returns an error on non-hardware builds.
func AddPIVKey(_ string, _ PIVKey) error {
	return ErrPIVNotAvailable
}

// RemovePIVKey is a stub that returns an error on non-hardware builds.
func RemovePIVKey(_ string, _ uint32) error {
	return ErrPIVNotAvailable
}
