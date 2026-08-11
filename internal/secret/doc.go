// Package secret provides transient memory handling for provider-resolved SSH
// passwords.
//
// This package wraps memguard for the short path between credential providers
// and PTY password injection. It is not credential storage, a vault, or a
// provider authentication layer.
//
// # Security Properties
//
// Passwords created with this package benefit from:
//   - Memory locking (mlock) to prevent swapping to disk
//   - Guard pages to detect buffer overflows
//   - Automatic zeroing on destruction
//   - Protection against accidental logging (no String/Format methods)
//
// # Usage Pattern
//
// Access secret data through callbacks to prevent reference retention:
//
//	password := secret.NewFromString(providerPassword)
//	defer password.Destroy()
//
//	err := password.Use(func(data []byte) error {
//	    // Write data here; don't retain references.
//	    return writePassword(data)
//	})
//
// # Creating Secrets
//
// Use [NewFromString] for password strings returned by external provider CLIs.
// The source string cannot be explicitly wiped because Go strings are immutable;
// the memguard copy is destroyed by [Secret.Destroy].
//
// # Important
//
// Always call [Secret.Destroy] when done to zero and release the secure memory.
// Secrets that are not destroyed will leak protected memory pages.
package secret
