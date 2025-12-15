// Package secret provides secure memory handling for sensitive data.
//
// This package wraps memguard to provide memory-safe storage for passwords,
// keys, and other sensitive values. Data is stored in locked memory pages
// that are protected from swapping and core dumps.
//
// # Security Properties
//
// Secrets created with this package benefit from:
//   - Memory locking (mlock) to prevent swapping to disk
//   - Guard pages to detect buffer overflows
//   - Automatic zeroing on destruction
//   - Protection against accidental logging (no String/Format methods)
//
// # Usage Pattern
//
// Access secret data through callbacks to prevent reference retention:
//
//	secret := secret.New(sensitiveBytes)
//	defer secret.Destroy()
//
//	err := secret.UseBytes(func(data []byte) error {
//	    // Use data here; don't retain references
//	    return doSomething(data)
//	})
//
// # Creating Secrets
//
// Use [New] for byte slices (source is wiped), [NewFromString] for strings
// (cannot wipe immutable strings), or [NewFromReader] to read from an io.Reader
// with size limits.
//
// # Important
//
// Always call [Secret.Destroy] when done to zero and release the secure memory.
// Secrets that are not destroyed will leak protected memory pages.
package secret
