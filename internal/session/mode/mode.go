// Package mode provides canonical security mode identifiers.
// This is a leaf package with no imports from vault, agent, or CLI packages.
package mode

// Mode identifies the security mode for credential protection.
type Mode string

const (
	Software      Mode = "software"
	PIV           Mode = "piv"
	FIDO2         Mode = "fido2"
	SecureEnclave Mode = "secureenclave"
)

// Valid returns true if the mode is a known type.
func (m Mode) Valid() bool {
	switch m {
	case Software, PIV, FIDO2, SecureEnclave:
		return true
	}
	return false
}

// String returns the mode as a string.
func (m Mode) String() string {
	return string(m)
}
