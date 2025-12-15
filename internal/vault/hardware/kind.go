package hardware

import "github.com/ntwrknrd/nssh/internal/session/mode"

// Kind identifies the type of hardware security device.
// Values are derived from session/mode to maintain a single source of truth.
type Kind string

const (
	// PIV indicates a PIV-compatible device (YubiKey).
	PIV Kind = Kind(mode.PIV)

	// FIDO2 indicates a FIDO2/WebAuthn security key.
	FIDO2 Kind = Kind(mode.FIDO2)

	// SecureEnclave indicates Apple Secure Enclave (macOS/iOS).
	SecureEnclave Kind = Kind(mode.SecureEnclave)
)

// Valid returns true if the kind is a known hardware type.
func (k Kind) Valid() bool {
	switch k {
	case PIV, FIDO2, SecureEnclave:
		return true
	default:
		return false
	}
}

// String returns the kind as a string for logging/errors.
func (k Kind) String() string {
	return string(k)
}
