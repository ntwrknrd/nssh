// Package vault provides age-encrypted credential management.
//
// The vault stores credentials (username/password pairs) encrypted with age,
// organized into contexts that map to SSH config include files.
//
// # Storage Structure
//
// Credentials are stored in an age-encrypted JSON file:
//
//	$XDG_CONFIG_HOME/nssh/credentials.age
//
// The encryption key is a passphrase-protected age identity (age.key.enc).
//
// # Credential Resolution
//
// The [Manager.ResolveCredential] method finds credentials for a host using
// a priority-based lookup:
//
//  1. Host-specific credential (exact hostname match)
//  2. Context default credential (from the SSH include file)
//  3. Domain-based credential (matching domain suffix)
//
// # Contexts
//
// Contexts group related hosts and credentials, typically corresponding to
// SSH config include files (e.g., "work.conf", "home.conf"). Each context
// can have a default credential and host-specific overrides.
//
// # Session Integration
//
// The vault requires an active agent session for decryption. Use [Manager.NeedsUnlock]
// to check if unlock is required, and coordinate with the session package for
// agent communication.
package vault
