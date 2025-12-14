package vault

import (
	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/vault/hardware"
	"github.com/ntwrknrd/nssh/internal/vault/software"
)

// Mode is a sealed interface for manager initialization modes.
// The unexported mode() method ensures only types in this package can implement Mode.
type Mode interface{ mode() }

// Unexported structs - callers must use constructor functions
type auto struct{}
type softwareMode struct{ store software.Store }
type hardwareMode struct{ kind hardware.Kind }
type provided struct{ identity *age.X25519Identity }

// Marker method implementations (unexported = sealed)
func (auto) mode()         {}
func (softwareMode) mode() {}
func (hardwareMode) mode() {}
func (provided) mode()     {}

// Auto returns a mode that detects configuration from existing files.
// This is the only mode with a valid zero-value (no parameters needed).
func Auto() Mode {
	return auto{}
}

// Software returns a mode using a passphrase-protected key stored on disk.
// Panics if store is nil (programming error).
func Software(store software.Store) Mode {
	if store == nil {
		panic("vault.Software: store must not be nil")
	}
	return softwareMode{store: store}
}

// Hardware returns a mode using a key stored on a hardware device.
// The agent mediates access to the hardware token.
// Panics if kind is empty (programming error). Unknown kinds return
// errors from NewManager, not panics, since they may come from config.
func Hardware(kind hardware.Kind) Mode {
	if kind == "" {
		panic("vault.Hardware: kind must not be empty")
	}
	return hardwareMode{kind: kind}
}

// Provided returns a mode using an already-decrypted identity.
// Used for rekey/enrollment operations where the identity is obtained externally.
// Panics if identity is nil (programming error).
func Provided(identity *age.X25519Identity) Mode {
	if identity == nil {
		panic("vault.Provided: identity must not be nil")
	}
	return provided{identity: identity}
}
