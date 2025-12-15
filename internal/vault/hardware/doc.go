// Package hardware provides types for hardware security device integration.
//
// Hardware modes store the private key on a separate security device (YubiKey PIV,
// FIDO2 token, Secure Enclave) rather than on the host filesystem. The agent still
// mediates access to the hardware token for session management.
//
// # Supported Hardware Types
//
// The [Kind] type identifies supported hardware backends:
//
//   - [PIV]: PIV-compatible smartcards (YubiKey 4/5, etc.)
//   - [FIDO2]: FIDO2/WebAuthn security keys (future)
//   - [SecureEnclave]: Apple Secure Enclave on macOS/iOS (future)
//
// # Architecture
//
// This package provides only type definitions and validation. Actual device
// communication is implemented in:
//   - internal/vault/piv: PIV keystore persistence and ECIES crypto
//   - internal/agent: PIV device access via piv-go (behind build tags)
//
// # Build Tags
//
// Hardware support requires CGO and the "hardware" build tag:
//
//	go build -tags hardware ./cmd/nssh
//
// Without the build tag, hardware types are still available but device
// operations will fail with stub implementations.
package hardware
