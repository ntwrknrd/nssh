package software

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"filippo.io/age"
)

// ErrNeedsUnlock is returned when an operation requires an unlocked session.
// Callers should check for this error and prompt for unlock or return an
// appropriate message to the user.
var ErrNeedsUnlock = errors.New("credentials locked")

// Kind identifies the type of host-backed key storage.
type Kind string

const (
	// Passphrase indicates passphrase-protected key storage.
	Passphrase Kind = "passphrase"
	// Future: Keychain, TPM, etc.
)

// Store is the interface for host-backed key storage.
// The key material lives on the local host.
// Implementations include passphrase-protected files, OS keychains, etc.
//
// All methods are non-interactive - callers handle passphrase prompting.
// Session management (lock/unlock tracking) is handled by the agent, not the store.
type Store interface {
	// Kind returns the type of software store for logging/metrics.
	Kind() Kind

	// Identity returns the age identity for decryption.
	// Only available after a successful UnlockWithPassphrase() call.
	Identity() (age.Identity, error)

	// Recipient returns the age recipient for encryption.
	// Reads from age.pub file - does NOT require unlock.
	// This allows adding credentials while session is locked.
	Recipient() (age.Recipient, error)

	// InitializeWithPassphrase sets up key storage non-interactively.
	// If force is true, reinitializes even if already configured (creates backup first).
	// Returns error if already initialized and force is false.
	InitializeWithPassphrase(passphrase []byte, force bool) error

	// InitializeStagedWithPassphrase creates new keys in temp files without overwriting existing keys.
	// Returns the new recipient for vault re-encryption. Call CommitStaged() after
	// vault is re-encrypted to atomically swap the new keys into place.
	// This is used by rekey to ensure old keys are preserved until vault is updated.
	InitializeStagedWithPassphrase(passphrase []byte) (age.Recipient, error)

	// CommitStaged atomically moves staged keys into place, replacing old keys.
	// Must be called after InitializeStagedWithPassphrase() and successful vault re-encryption.
	CommitStaged() error

	// UnlockWithPassphrase decrypts the identity using the provided passphrase.
	// The identity is cached in memory until the process exits.
	UnlockWithPassphrase(passphrase []byte) error
}

// Config holds store configuration.
type Config struct {
	ConfigDir           string        // ~/.config/nssh
	DataDir             string        // ~/.local/share/nssh (XDG data dir - vault, backups)
	StateDir            string        // ~/.local/state/nssh (XDG state dir - lockout, audit)
	Logger              *slog.Logger  // Logger for audit events (nil = no logging)
	ScryptWorkFactor    int           // scrypt work factor (default 18 = 2^18 iterations)
	LockoutThreshold    int           // failed attempts before lockout (default 10)
	LockoutDuration     time.Duration // initial lockout duration (default 5m)
	MaxLockoutDuration  time.Duration // maximum lockout duration (default 1h)
	PassphraseMinLength int           // minimum passphrase length (default 12)
}

// New creates a new PassphraseStore.
func New(cfg Config) (Store, error) {
	return NewPassphraseStore(cfg)
}

// nilLogger returns a logger that discards all output.
func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
