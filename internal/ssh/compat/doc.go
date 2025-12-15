// Package compat provides SSH compatibility detection and fix definitions.
//
// This package is intentionally small and import-light so both the connector
// and sshconfig mutators can share the same compatibility concepts without
// creating unwanted dependencies.
//
// # Compatibility Types
//
// The package defines fix types for common SSH negotiation failures:
//
//   - [CompatKex]: Legacy key exchange algorithms (diffie-hellman-group1-sha1, etc.)
//   - [CompatMACs]: Legacy MAC algorithms (hmac-sha1, etc.)
//   - [CompatCiphers]: Legacy cipher algorithms (aes128-cbc, 3des-cbc, etc.)
//   - [CompatHostKey]: Legacy host key algorithms (ssh-rsa, ssh-dss)
//
// # Detection
//
// Use [ParseCompatibilityError] to analyze SSH stderr output and identify
// which compatibility fixes are needed:
//
//	compatTypes := compat.ParseCompatibilityError(sshStderr)
//	for _, ct := range compatTypes {
//	    fmt.Println("Needs fix:", compat.CompatConfigs[ct].Name)
//	}
//
// # Fix Definitions
//
// The [CompatConfigs] map provides SSH config directives for each fix type.
// These are applied via the sshconfig package's [ApplyCompatFixes] function.
//
// # Error Patterns
//
// Each fix type includes regex patterns that match OpenSSH error messages,
// enabling automatic detection of compatibility issues during connection
// failures.
package compat
