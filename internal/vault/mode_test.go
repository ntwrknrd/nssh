package vault

import (
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/vault/hardware"
	"github.com/ntwrknrd/nssh/internal/vault/software"
	"github.com/stretchr/testify/assert"
)

// mockStore implements software.Store for testing
type mockStore struct{}

func (m *mockStore) Kind() software.Kind                                          { return software.Passphrase }
func (m *mockStore) Identity() (age.Identity, error)                              { return nil, nil }
func (m *mockStore) Recipient() (age.Recipient, error)                            { return nil, nil }
func (m *mockStore) InitializeWithPassphrase(passphrase []byte, force bool) error { return nil }
func (m *mockStore) InitializeStagedWithPassphrase(passphrase []byte) (age.Recipient, error) {
	return nil, nil
}
func (m *mockStore) CommitStaged() error                          { return nil }
func (m *mockStore) UnlockWithPassphrase(passphrase []byte) error { return nil }

func TestAuto_Valid(t *testing.T) {
	mode := Auto()
	assert.NotNil(t, mode)
	// Verify it's the right type
	_, ok := mode.(auto)
	assert.True(t, ok, "Auto() should return auto type")
}

func TestSoftware_Valid(t *testing.T) {
	store := &mockStore{}
	mode := Software(store)
	assert.NotNil(t, mode)
	// Verify it's the right type with the store
	sm, ok := mode.(softwareMode)
	assert.True(t, ok, "Software() should return softwareMode type")
	assert.Equal(t, store, sm.store)
}

func TestSoftware_NilStore_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "vault.Software: store must not be nil", func() {
		Software(nil)
	})
}

func TestHardware_ValidKinds(t *testing.T) {
	for _, kind := range []hardware.Kind{hardware.PIV, hardware.FIDO2, hardware.SecureEnclave} {
		mode := Hardware(kind)
		assert.NotNil(t, mode)
		// Verify it's the right type with the kind
		hm, ok := mode.(hardwareMode)
		assert.True(t, ok, "Hardware() should return hardwareMode type")
		assert.Equal(t, kind, hm.kind)
	}
}

func TestHardware_EmptyKind_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "vault.Hardware: kind must not be empty", func() {
		Hardware("")
	})
}

func TestProvided_Valid(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	assert.NoError(t, err)

	mode := Provided(identity)
	assert.NotNil(t, mode)
	// Verify it's the right type with the identity
	p, ok := mode.(provided)
	assert.True(t, ok, "Provided() should return provided type")
	assert.Equal(t, identity, p.identity)
}

func TestProvided_NilIdentity_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "vault.Provided: identity must not be nil", func() {
		Provided(nil)
	})
}

func TestNewManager_Hardware_InvalidKind_ReturnsError(t *testing.T) {
	// Create temp directory for test paths
	tmpDir := t.TempDir()

	paths := &config.Paths{
		CredentialsFile: filepath.Join(tmpDir, "credentials.age"),
		ConfigDir:       tmpDir,
		DataDir:         tmpDir,
		StateDir:        tmpDir,
		BackupDir:       filepath.Join(tmpDir, "backups"),
	}

	// Try to create a manager with an invalid hardware kind
	_, err := NewManager(
		Hardware("invalid"),
		WithPaths(paths),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown hardware kind")
}

// TestMode_Sealed verifies that the Mode interface is properly sealed
// by checking that all exported constructors produce types with the mode() method
func TestMode_Sealed(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()
	store := &mockStore{}

	modes := []Mode{
		Auto(),
		Software(store),
		Hardware(hardware.PIV),
		Provided(identity),
	}

	for _, m := range modes {
		// All modes should satisfy the Mode interface
		assert.NotNil(t, m)
		// The mode() method should be callable (but does nothing)
		m.mode()
	}
}
