//go:build hardware

// Package piv provides PIV keystore persistence and ECIES crypto helpers.
//
// This package handles storage formats and cryptographic operations for
// YubiKey PIV-based credential management. Device access (piv-go) is NOT
// in this package - it stays in internal/agent behind build tags. This
// package is focused on persistence and crypto only.
//
// # Keystore Format
//
// The PIV keystore (piv.json) uses a versioned JSON format:
//
//	{
//	    "version": 2,
//	    "keys": [
//	        {
//	            "serial": 12345678,
//	            "slot_key": 0x9a,
//	            "public_key": "<base64>",
//	            "identity": "<ECIES-encrypted age identity>",
//	            "label": "Work YubiKey",
//	            "enrolled": "2024-01-15T10:30:00Z"
//	        }
//	    ]
//	}
//
// # Multi-Key Support
//
// Version 2 supports multiple enrolled YubiKeys, allowing users to have
// backup keys or different keys for different contexts. Each key entry
// contains its own ECIES-encrypted copy of the age identity.
//
// # ECIES Encryption
//
// The age identity is encrypted using ECIES (Elliptic Curve Integrated
// Encryption Scheme) with the YubiKey's PIV key. This allows the identity
// to be decrypted only when the YubiKey is present and the PIN is provided.
//
// # Build Tags
//
// This package requires the "hardware" build tag. Without it, stub
// implementations are used that return appropriate errors.
package piv
