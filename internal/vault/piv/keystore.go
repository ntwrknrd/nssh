//go:build hardware

// Package piv provides PIV keystore persistence and ECIES crypto helpers.
// This package handles storage formats and cryptographic operations for
// YubiKey PIV-based credential management.
//
// Device access (piv-go) is NOT in this package - it stays in internal/agent
// behind build tags. This package is focused on persistence and crypto only.
package piv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

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

// LoadPIVKeystore loads the PIV keystore, auto-migrating from v1 if needed.
func LoadPIVKeystore(configDir string) (*PIVKeystore, error) {
	metaPath := filepath.Join(configDir, "piv.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read PIV config: %w (run 'nssh self init' with hardware mode first)", err)
	}

	// Try v2 format first
	var ks PIVKeystore
	if err := json.Unmarshal(data, &ks); err == nil && ks.Version == 2 {
		return &ks, nil
	}

	// Try v1 format (single key with separate encrypted identity file)
	var v1 PIVMeta
	if err := json.Unmarshal(data, &v1); err != nil {
		return nil, fmt.Errorf("parse PIV config: %w", err)
	}

	// Read v1 encrypted identity from separate file
	encPath := filepath.Join(configDir, "age.key.piv")
	encData, err := os.ReadFile(encPath)
	if err != nil {
		return nil, fmt.Errorf("read encrypted identity (v1 format): %w", err)
	}

	// Convert to v2 format
	ks = PIVKeystore{
		Version: 2,
		Keys: []PIVKey{{
			Serial:    v1.Serial,
			SlotKey:   v1.SlotKey,
			PublicKey: v1.PublicKey,
			Label:     "Primary",
			Identity:  encData,
		}},
	}

	// Save migrated format
	if err := SavePIVKeystore(configDir, &ks); err != nil {
		// Non-fatal - we can still use the in-memory v2 format
		fmt.Fprintf(os.Stderr, "warning: could not save migrated PIV config: %v\n", err)
	} else {
		// Remove old v1 file
		_ = os.Remove(encPath)
	}

	return &ks, nil
}

// SavePIVKeystore writes the keystore to piv.json.
func SavePIVKeystore(configDir string, ks *PIVKeystore) error {
	metaPath := filepath.Join(configDir, "piv.json")
	return SavePIVKeystoreToPath(metaPath, ks)
}

// SavePIVKeystoreToPath writes the keystore to the specified path.
// Uses atomic write (temp file + rename) to avoid partial writes on crash.
func SavePIVKeystoreToPath(path string, ks *PIVKeystore) error {
	ks.Version = 2
	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal PIV config: %w", err)
	}

	// Atomic write: write to temp file in same directory, then rename
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".piv.json.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Ensure temp file is cleaned up on error
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	success = true
	return nil
}

// AddPIVKey adds a new YubiKey to the keystore.
func AddPIVKey(configDir string, key PIVKey) error {
	ks, err := LoadPIVKeystore(configDir)
	if err != nil {
		// If no keystore exists, create a new one
		ks = &PIVKeystore{Version: 2}
	}

	// Check for duplicate serial
	for _, k := range ks.Keys {
		if k.Serial == key.Serial {
			return fmt.Errorf("YubiKey %d is already enrolled", key.Serial)
		}
	}

	// Set enrollment timestamp if not set
	if key.Enrolled == "" {
		key.Enrolled = time.Now().UTC().Format(time.RFC3339)
	}

	// Auto-label if not set
	if key.Label == "" {
		if len(ks.Keys) == 0 {
			key.Label = "Primary"
		} else {
			key.Label = fmt.Sprintf("Backup %d", len(ks.Keys))
		}
	}

	ks.Keys = append(ks.Keys, key)
	return SavePIVKeystore(configDir, ks)
}

// RemovePIVKey removes a YubiKey from the keystore by serial.
func RemovePIVKey(configDir string, serial uint32) error {
	ks, err := LoadPIVKeystore(configDir)
	if err != nil {
		return err
	}

	if len(ks.Keys) <= 1 {
		return errors.New("cannot remove the last enrolled YubiKey")
	}

	found := false
	newKeys := make([]PIVKey, 0, len(ks.Keys)-1)
	for _, k := range ks.Keys {
		if k.Serial == serial {
			found = true
			continue
		}
		newKeys = append(newKeys, k)
	}

	if !found {
		return fmt.Errorf("YubiKey %d not found in keystore", serial)
	}

	ks.Keys = newKeys
	return SavePIVKeystore(configDir, ks)
}
