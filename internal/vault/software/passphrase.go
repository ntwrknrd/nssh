//go:build linux || darwin

package software

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"
)

// nowFunc is the function used to get current time.
// Tests can override this via setNowFunc in time_test.go.
var nowFunc = time.Now

// PassphraseStore implements Store using passphrase-protected age key.
type PassphraseStore struct {
	encKeyPath         string        // ~/.config/nssh/age.key.enc
	pubKeyPath         string        // ~/.config/nssh/age.pub
	stateDir           string        // ~/.local/state/nssh (XDG state dir)
	scryptWorkFactor   int           // scrypt work factor (2^N iterations)
	lockoutThreshold   int           // failed attempts before lockout
	lockoutDuration    time.Duration // initial lockout duration
	maxLockoutDuration time.Duration // maximum lockout duration
	logger             *slog.Logger  // For audit logging (nil-safe)
	identity           age.Identity  // cached after unlock
	passMinLen         int           // minimum passphrase length
}

// Kind returns the type of software store.
func (p *PassphraseStore) Kind() Kind {
	return Passphrase
}

// NewPassphraseStore creates a new PassphraseStore.
func NewPassphraseStore(cfg Config) (*PassphraseStore, error) {
	// IMPORTANT: cfg.StateDir is guaranteed to be populated by config.Load() before
	// this function is called. See "Config loading order and XDG path population"
	// section for the initialization sequence. The store never computes XDG paths.

	// Ensure state directory exists for lockout.json
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	// Ensure config directory exists for age.key.enc and age.pub
	if err := os.MkdirAll(cfg.ConfigDir, 0700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	// Create nil-safe logger wrapper
	logger := cfg.Logger
	if logger == nil {
		logger = nilLogger()
	}

	// Default scrypt work factor (2^18 = ~262k iterations, ~1s on modern hardware)
	workFactor := cfg.ScryptWorkFactor
	if workFactor < 14 {
		workFactor = 18 // default: secure for modern hardware
	} else if workFactor > 22 {
		workFactor = 22 // cap to prevent excessive delays (2^22 = ~4M iterations)
	}

	// Lockout defaults
	lockoutThreshold := cfg.LockoutThreshold
	if lockoutThreshold == 0 {
		lockoutThreshold = 10
	}
	lockoutDuration := cfg.LockoutDuration
	if lockoutDuration == 0 {
		lockoutDuration = 5 * time.Minute
	}
	maxLockoutDuration := cfg.MaxLockoutDuration
	if maxLockoutDuration == 0 {
		maxLockoutDuration = 1 * time.Hour
	}

	passMinLen := cfg.PassphraseMinLength
	if passMinLen == 0 {
		passMinLen = 12
	}
	if passMinLen < 8 {
		passMinLen = 8
	}
	if passMinLen > 128 {
		passMinLen = 128
	}

	return &PassphraseStore{
		encKeyPath:         filepath.Join(cfg.ConfigDir, "age.key.enc"),
		pubKeyPath:         filepath.Join(cfg.ConfigDir, "age.pub"),
		stateDir:           cfg.StateDir, // XDG state dir passed from config
		scryptWorkFactor:   workFactor,
		lockoutThreshold:   lockoutThreshold,
		lockoutDuration:    lockoutDuration,
		maxLockoutDuration: maxLockoutDuration,
		logger:             logger,
		passMinLen:         passMinLen,
	}, nil
}

// validatePassphraseBytes enforces passphrase requirements on raw bytes.
func validatePassphraseBytes(passphrase []byte, minLen int) error {
	if len(bytes.TrimSpace(passphrase)) == 0 {
		return errors.New("passphrase cannot be empty or whitespace-only")
	}
	if len(passphrase) < minLen {
		return fmt.Errorf("passphrase must be at least %d characters (got %d)", minLen, len(passphrase))
	}
	return nil
}

// InitializeWithPassphrase sets up passphrase-protected key storage non-interactively.
// Used for CI/testing environments where TTY is not available.
// The passphrase is validated against the same requirements as interactive Initialize.
func (p *PassphraseStore) InitializeWithPassphrase(passphrase []byte, force bool) error {
	// 0. Check if already initialized - prevent accidental reinitialization
	alreadyInitialized := false
	if _, err := os.Stat(p.encKeyPath); err == nil {
		if !force {
			return errors.New("credentials already initialized; use --force to reinitialize")
		}
		alreadyInitialized = true
	}

	// 1. Validate passphrase requirements
	if err := validatePassphraseBytes(passphrase, p.passMinLen); err != nil {
		return err
	}

	// 2. Generate X25519 keypair
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return err
	}

	// 3. Write keys atomically using temp files
	tmpPubPath := p.pubKeyPath + ".tmp"
	tmpEncPath := p.encKeyPath + ".tmp"

	defer func() {
		_ = os.Remove(tmpPubPath)
		_ = os.Remove(tmpEncPath)
	}()

	if err := os.WriteFile(tmpPubPath, []byte(identity.Recipient().String()+"\n"), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	if err := p.writeEncryptedKeyTo(tmpEncPath, identity, passphrase); err != nil {
		return fmt.Errorf("write encrypted key: %w", err)
	}

	// 4. Backup old keys if reinitializing
	var bakPubPath, bakEncPath string
	if alreadyInitialized {
		bakPubPath = p.pubKeyPath + ".bak"
		bakEncPath = p.encKeyPath + ".bak"
		_ = os.Rename(p.pubKeyPath, bakPubPath)
		_ = os.Rename(p.encKeyPath, bakEncPath)
	}

	// 5. Atomic rename: public key first
	if err := os.Rename(tmpPubPath, p.pubKeyPath); err != nil {
		if bakPubPath != "" {
			_ = os.Rename(bakPubPath, p.pubKeyPath)
			_ = os.Rename(bakEncPath, p.encKeyPath)
		}
		return fmt.Errorf("finalize public key: %w", err)
	}

	// 6. Atomic rename: encrypted key (commit point)
	if err := os.Rename(tmpEncPath, p.encKeyPath); err != nil {
		_ = os.Remove(p.pubKeyPath)
		if bakPubPath != "" {
			_ = os.Rename(bakPubPath, p.pubKeyPath)
			_ = os.Rename(bakEncPath, p.encKeyPath)
		}
		return fmt.Errorf("finalize encrypted key: %w", err)
	}

	// 7. Delete backups after success
	if bakPubPath != "" {
		_ = secureDelete(bakEncPath)
		_ = os.Remove(bakPubPath)
	}

	// 8. Cache identity for immediate use
	p.identity = identity
	p.logger.Info("store_initialized", "force", force, "reinit", alreadyInitialized, "method", "automation")

	return nil
}

// InitializeStagedWithPassphrase creates new keys in temp files without overwriting existing keys.
// Returns the new recipient for vault re-encryption. Call CommitStaged() after
// vault is re-encrypted to atomically swap the new keys into place.
func (p *PassphraseStore) InitializeStagedWithPassphrase(passphrase []byte) (age.Recipient, error) {
	// 1. Validate passphrase requirements
	if err := validatePassphraseBytes(passphrase, p.passMinLen); err != nil {
		return nil, err
	}

	// 2. Generate X25519 keypair
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}

	// 3. Write keys to STAGED temp files (not the .tmp used by Initialize)
	stagedPubPath := p.pubKeyPath + ".staged"
	stagedEncPath := p.encKeyPath + ".staged"

	// Write public key to staged file
	if err := os.WriteFile(stagedPubPath, []byte(identity.Recipient().String()+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("write staged public key: %w", err)
	}

	// Write encrypted key to staged file
	if err := p.writeEncryptedKeyTo(stagedEncPath, identity, passphrase); err != nil {
		_ = os.Remove(stagedPubPath)
		return nil, fmt.Errorf("write staged encrypted key: %w", err)
	}

	// Cache the new identity for use after CommitStaged
	p.identity = identity
	p.logger.Info("store_initialize_staged")

	return identity.Recipient(), nil
}

// CommitStaged atomically moves staged keys into place, replacing old keys.
// Must be called after InitializeStagedWithPassphrase() and successful vault re-encryption.
func (p *PassphraseStore) CommitStaged() error {
	stagedPubPath := p.pubKeyPath + ".staged"
	stagedEncPath := p.encKeyPath + ".staged"

	// Verify staged files exist
	if _, err := os.Stat(stagedEncPath); os.IsNotExist(err) {
		return errors.New("no staged keys to commit; call InitializeStaged first")
	}

	// Securely delete old keys before overwriting
	if _, err := os.Stat(p.encKeyPath); err == nil {
		_ = secureDelete(p.encKeyPath) // Best effort
	}
	_ = os.Remove(p.pubKeyPath) // Public key doesn't need secure delete

	// Atomic rename: public key first
	if err := os.Rename(stagedPubPath, p.pubKeyPath); err != nil {
		return fmt.Errorf("commit public key: %w", err)
	}

	// Atomic rename: encrypted key (commit point)
	if err := os.Rename(stagedEncPath, p.encKeyPath); err != nil {
		_ = os.Remove(p.pubKeyPath) // Clean up on failure
		return fmt.Errorf("commit encrypted key: %w", err)
	}

	p.logger.Info("store_commit_staged")
	return nil
}

// writeEncryptedKeyTo encrypts the X25519 identity and writes to the specified path.
//
// SECURITY NOTE: age.NewScryptRecipient requires a string argument, forcing a
// conversion from []byte. This briefly places the passphrase in unguarded memory
// (Go strings are immutable and cannot be zeroed). This is an accepted limitation
// of the age library API. The exposure window is minimal (microseconds) and the
// LockedBuffer is destroyed immediately after conversion.
func (p *PassphraseStore) writeEncryptedKeyTo(path string, identity *age.X25519Identity, passphrase []byte) error {
	scryptRecipient, err := age.NewScryptRecipient(string(passphrase))
	if err != nil {
		return err
	}
	scryptRecipient.SetWorkFactor(p.scryptWorkFactor)

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, scryptRecipient)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, identity.String()); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0600)
}

// decryptKey decrypts the X25519 identity from age.key.enc using the passphrase.
// See writeEncryptedKey for security note about string conversion.
func (p *PassphraseStore) decryptKey(passphrase []byte) (*age.X25519Identity, error) {
	encKey, err := os.ReadFile(p.encKeyPath)
	if err != nil {
		return nil, err
	}

	scryptIdentity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return nil, err
	}

	r, err := age.Decrypt(bytes.NewReader(encKey), scryptIdentity)
	if err != nil {
		return nil, fmt.Errorf("wrong passphrase or corrupted key file")
	}

	identityStr, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return age.ParseX25519Identity(strings.TrimSpace(string(identityStr)))
}

// lockoutState tracks failed unlock attempts across process invocations.
// Stored in ~/.local/state/nssh/lockout.json with HMAC signature.
type lockoutState struct {
	FailedAttempts int       `json:"failed_attempts"`
	LastAttempt    time.Time `json:"last_attempt"`
	LockedUntil    time.Time `json:"locked_until,omitempty"`
	Signature      string    `json:"sig,omitempty"` // HMAC-SHA256 hex-encoded
}

const (
	maxAttemptsPerSession = 3   // Attempts before process exits (internal behavior)
	lockoutJitter         = 250 // Max jitter in milliseconds (internal behavior)
)

// lockoutPath returns the path to the lockout state file.
// p.stateDir is guaranteed to be populated via NewPassphraseStore() which
// receives it from config.Load(). See "Config loading order" section.
func (p *PassphraseStore) lockoutPath() string {
	return filepath.Join(p.stateDir, "lockout.json")
}

// hmacKey derives an HMAC key from the public key file.
// This provides tamper detection without requiring unlock.
func (p *PassphraseStore) hmacKey() []byte {
	pubKey, err := os.ReadFile(p.pubKeyPath)
	if err != nil {
		// If no public key, use a constant (still provides some protection)
		return []byte("nssh-lockout-v1")
	}
	// Hash the public key to get a fixed-size key
	h := sha256.Sum256(pubKey)
	return h[:]
}

// signLockout computes HMAC signature for lockout state.
func (p *PassphraseStore) signLockout(state *lockoutState) string {
	key := p.hmacKey()

	// Create message from state fields (excluding signature)
	msg := fmt.Sprintf("%d|%d|%d",
		state.FailedAttempts,
		state.LastAttempt.UnixNano(),
		state.LockedUntil.UnixNano())

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyLockout checks HMAC signature of lockout state.
func (p *PassphraseStore) verifyLockout(state *lockoutState) bool {
	if state.Signature == "" {
		// No signature = old format or cleared state, accept it
		return state.FailedAttempts == 0 && state.LockedUntil.IsZero()
	}

	expected := p.signLockout(state)

	// Constant-time comparison
	return hmac.Equal([]byte(state.Signature), []byte(expected))
}

// loadLockout reads lockout state with file locking and HMAC verification.
func (p *PassphraseStore) loadLockout() (*lockoutState, error) {
	path := p.lockoutPath()

	f, err := os.OpenFile(path, os.O_RDONLY, 0600)
	if os.IsNotExist(err) {
		return &lockoutState{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Shared lock for reading
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return nil, fmt.Errorf("lock file: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var state lockoutState
	if err := json.Unmarshal(data, &state); err != nil {
		return &lockoutState{}, nil // Corrupted, reset
	}

	// Verify HMAC signature
	if !p.verifyLockout(&state) {
		// Tampering detected - assume worst case
		p.logger.Warn("lockout state tampering detected, assuming max lockout")
		return &lockoutState{
			FailedAttempts: p.lockoutThreshold,
			LastAttempt:    time.Now(),
			LockedUntil:    time.Now().Add(p.maxLockoutDuration),
		}, nil
	}

	return &state, nil
}

// saveLockout writes lockout state with file locking and HMAC signature.
func (p *PassphraseStore) saveLockout(state *lockoutState) error {
	path := p.lockoutPath()

	// Add HMAC signature
	state.Signature = p.signLockout(state)

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	// Open with exclusive lock
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Exclusive lock for writing
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock file: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	if _, err := f.Write(data); err != nil {
		return err
	}

	return f.Sync()
}

// jitteredDuration adds random jitter to prevent timing attacks.
func jitteredDuration(d time.Duration) time.Duration {
	// Add 0-250ms jitter
	jitterBytes := make([]byte, 1)
	if _, err := rand.Read(jitterBytes); err != nil {
		return d // Fall back to no jitter on error
	}
	jitter := time.Duration(int(jitterBytes[0]) * lockoutJitter / 256)
	return d + jitter*time.Millisecond
}

// checkLockout loads lockout state and checks if currently locked.
// Returns the state for use by recordFailure/clearLockout.
func (p *PassphraseStore) checkLockout() (*lockoutState, error) {
	state, err := p.loadLockout()
	if err != nil {
		return nil, fmt.Errorf("check lockout: %w", err)
	}

	now := nowFunc()
	if now.Before(state.LockedUntil) {
		remaining := state.LockedUntil.Sub(now).Round(time.Second)
		return nil, fmt.Errorf("too many failed attempts. Try again in %v", remaining)
	}

	// Decay: reset counter if last attempt was > 1 hour ago
	if now.Sub(state.LastAttempt) > time.Hour {
		state.FailedAttempts = 0
	}

	return state, nil
}

// recordFailure increments the failure counter and applies lockout if threshold reached.
// Returns error if lockout was triggered.
func (p *PassphraseStore) recordFailure(state *lockoutState) error {
	now := nowFunc()
	state.FailedAttempts++
	state.LastAttempt = now

	p.logger.Warn("unlock failed", "attempts", state.FailedAttempts)

	if state.FailedAttempts >= p.lockoutThreshold {
		// Calculate lockout duration with exponential backoff
		lockoutCount := (state.FailedAttempts - p.lockoutThreshold) / maxAttemptsPerSession
		duration := p.lockoutDuration * time.Duration(1<<lockoutCount)
		if duration > p.maxLockoutDuration {
			duration = p.maxLockoutDuration
		}
		// Add jitter to prevent timing attacks
		duration = jitteredDuration(duration)
		state.LockedUntil = now.Add(duration)
		_ = p.saveLockout(state)
		p.logger.Warn("lockout triggered", "duration", duration)
		return fmt.Errorf("too many failed attempts. Locked for %v", duration.Round(time.Second))
	}

	_ = p.saveLockout(state)
	return nil
}

// clearLockout resets failure count on successful unlock.
func (p *PassphraseStore) clearLockout(state *lockoutState) {
	state.FailedAttempts = 0
	state.LockedUntil = time.Time{}
	_ = p.saveLockout(state)
}

// Identity returns the age identity for decryption.
// Only available after UnlockWithPassphrase() has been called.
func (p *PassphraseStore) Identity() (age.Identity, error) {
	if p.identity != nil {
		return p.identity, nil
	}
	return nil, ErrNeedsUnlock
}

// Recipient returns the public key for encryption WITHOUT requiring unlock.
// This allows `nssh cred set` to encrypt new credentials
// even when the session is locked.
func (p *PassphraseStore) Recipient() (age.Recipient, error) {
	pubKey, err := os.ReadFile(p.pubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("public key not found (run 'nssh self init'): %w", err)
	}
	return age.ParseX25519Recipient(strings.TrimSpace(string(pubKey)))
}

// UnlockWithPassphrase unlocks using the provided passphrase (for automation).
// This bypasses the interactive prompt but still enforces lockout protection
// to prevent brute-force attacks via automation.
func (p *PassphraseStore) UnlockWithPassphrase(passphrase []byte) error {
	// Check for active lockout - CRITICAL: prevents brute-force via automation
	state, err := p.checkLockout()
	if err != nil {
		return err
	}

	// Validate passphrase format
	if err := validatePassphraseBytes(passphrase, p.passMinLen); err != nil {
		return err
	}

	// Decrypt age.key.enc using provided passphrase
	identity, err := p.decryptKey(passphrase)
	if err != nil {
		// Record failure - this is critical for lockout protection
		if lockoutErr := p.recordFailure(state); lockoutErr != nil {
			return lockoutErr
		}
		return fmt.Errorf("wrong passphrase or corrupted key file")
	}

	// Success - clear lockout state
	p.clearLockout(state)

	// Log successful unlock
	p.logger.Info("session unlocked", "mode", "passphrase", "method", "automation")

	// Cache identity in memory for this process
	// Session persistence is now handled by the agent
	p.identity = identity
	return nil
}

// secureDelete overwrites a file with random data before unlinking.
// Used by InitializeWithPassphrase(force=true) and migration to remove old key material.
// Note: This provides best-effort security. On SSDs with wear leveling,
// APFS copy-on-write, or other advanced filesystems, the original data
// may still exist in other sectors. For maximum security, use encrypted
// volumes where the encryption key can be destroyed.
func secureDelete(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already gone
		}
		return err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}

	// Overwrite with random data
	randomData := make([]byte, info.Size())
	if _, err := rand.Read(randomData); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.WriteAt(randomData, 0); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()

	return os.Remove(path)
}
