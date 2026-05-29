// Package software provides host-backed age identity storage.
//
// "Software" means the key material is stored on the local host, encrypted on disk.
// The agent handles session management after unlock.
//
// # Storage Format
//
// The passphrase-protected store uses:
//   - age.key.enc: scrypt-encrypted age X25519 identity
//   - age.pub: plaintext age recipient (public key)
//
// The public key file allows encryption (adding credentials) without unlocking,
// while decryption requires the passphrase to access the private key.
//
// # Store Interface
//
// The [Store] interface abstracts different software storage backends:
//
//	store, err := software.NewPassphraseStore(configDir)
//	if err := store.UnlockWithPassphrase(passphrase); err != nil {
//	    return err
//	}
//	identity, err := store.Identity()
//
// # Passphrase Security
//
// The passphrase store uses scrypt for key derivation with parameters tuned
// for security (N=2^20, r=8, p=1). The derived key encrypts the age identity
// using age's scrypt recipient.
//
// # Future Extensions
//
// The [Kind] type and [Store] interface are designed to support additional
// software backends such as OS keychains (macOS Keychain, Windows Credential
// Manager) or other local secret storage.
package software
