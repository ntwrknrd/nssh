package vault

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/logging"
	"github.com/ntwrknrd/nssh/internal/session/mode"
	"github.com/ntwrknrd/nssh/internal/vault/hardware"
	"github.com/ntwrknrd/nssh/internal/vault/software"
)

// ErrSessionUnavailable indicates no decrypt session is available.
var ErrSessionUnavailable = errors.New("session unavailable")

// DecryptFunc decrypts vault ciphertext via an external session (typically agent).
// Implementations should respect ctx (deadlines/cancel) where possible.
type DecryptFunc func(ctx context.Context, ciphertext []byte) ([]byte, error)

// SessionAvailableFunc reports whether a decrypt session is available.
type SessionAvailableFunc func(ctx context.Context) bool

// LockFunc terminates the active decrypt session.
type LockFunc func(ctx context.Context) error

// SessionDeps holds optional agent-backed session behavior.
// Zero-value (all fields nil) means "no session available".
type SessionDeps struct {
	BaseContext context.Context
	Available   SessionAvailableFunc
	Decrypt     DecryptFunc
	Lock        LockFunc
}

// Credential represents a username/password pair.
type Credential struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Default  bool   `json:"default,omitempty"`
}

// Context represents a credential context with associated metadata.
type Context struct {
	GitIncludeFile string      `json:"git_include_file"`
	Credential     *Credential `json:"credential"`
	Domain         string      `json:"domain"`
}

// HostCredentials represents host-specific credentials.
type HostCredentials struct {
	Credentials []Credential `json:"credentials"`
}

// VaultData is the top-level structure stored in the encrypted file.
type VaultData struct {
	Contexts map[string]*Context         `json:"contexts"`
	Hosts    map[string]*HostCredentials `json:"hosts"`
}

// ContextEntry is returned by ListContexts with computed fields.
type ContextEntry struct {
	Name            string      `json:"name"`
	GitIncludeFile  string      `json:"git_include_file"`
	Domain          string      `json:"domain"`
	Credential      *Credential `json:"credential"`
	CredentialCount int         `json:"credential_count"`
}

// Manager handles age-encrypted credential storage.
type Manager struct {
	credentialPath string
	configDir      string // For age.pub, piv.json access
	backupDir      string
	maxBackups     int
	audit          *logging.AuditLogger

	// modeType is the typed mode for compile-time safety
	modeType Mode

	// Cache
	cache      *VaultData
	cacheMtime time.Time

	// Age identity (for provided mode - rekey/migration operations)
	identities []age.Identity
	recipient  age.Recipient

	// Store for software key storage (nil for hardware modes)
	store software.Store

	// sessionDeps holds injected session behavior for locked-mode operations
	sessionDeps SessionDeps
}

// Option configures orthogonal concerns that apply across all modes.
type Option func(*managerConfig)

// managerConfig holds configuration for NewManager options.
type managerConfig struct {
	paths       *config.Paths
	audit       *logging.AuditLogger
	maxBackups  int
	sessionDeps SessionDeps
}

// WithPaths sets explicit paths (default: config.DefaultPaths()).
func WithPaths(paths *config.Paths) Option {
	return func(c *managerConfig) { c.paths = paths }
}

// WithAuditLogger sets the audit logger for security events.
func WithAuditLogger(logger *logging.AuditLogger) Option {
	return func(c *managerConfig) { c.audit = logger }
}

// WithMaxBackups sets the maximum number of credential backups to retain.
func WithMaxBackups(n int) Option {
	return func(c *managerConfig) { c.maxBackups = n }
}

// WithSessionDeps injects optional locked-mode session behavior.
// The zero-value (all fields nil) means "no session available".
func WithSessionDeps(deps SessionDeps) Option {
	return func(c *managerConfig) { c.sessionDeps = deps }
}

func applyOptions(opts ...Option) *managerConfig {
	cfg := &managerConfig{
		paths:      config.DefaultPaths(),
		maxBackups: 5,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// managerState represents the vault manager's operational state.
type managerState int

const (
	stateUninitialized managerState = iota
	stateProvided                   // has pre-loaded identities (rekey/verify)
	stateLocked                     // needs unlock (agent not running or not yet checked)
)

// state returns the manager's state based ONLY on cached values.
// No I/O, no mutations - purely observational and idempotent.
func (m *Manager) state() managerState {
	if m.modeType == nil {
		return stateUninitialized
	}
	if len(m.identities) > 0 {
		return stateProvided
	}
	return stateLocked
}

// ModeString returns the mode as a string for logging/IPC.
func (m *Manager) ModeString() string {
	switch mode := m.modeType.(type) {
	case auto:
		return "auto"
	case softwareMode:
		return string(mode.store.Kind())
	case hardwareMode:
		return string(mode.kind)
	case provided:
		return "provided"
	default:
		return "unknown"
	}
}

// lockedError returns a mode-specific error message for locked vault.
func (m *Manager) lockedError() error {
	if mode, ok := m.modeType.(hardwareMode); ok {
		switch mode.kind {
		case hardware.PIV:
			return errors.New("vault locked (piv): insert YubiKey and run 'nssh unlock'")
		case hardware.FIDO2:
			return errors.New("vault locked (fido2): insert security key and run 'nssh unlock'")
		case hardware.SecureEnclave:
			return errors.New("vault locked (secureenclave): run 'nssh unlock'")
		}
	}
	return errors.New("vault locked (software): run 'nssh unlock'")
}

// auditInfo logs to the audit logger if configured (nil-safe).
func (m *Manager) auditInfo(msg string, args ...any) {
	if m.audit != nil {
		m.audit.Info(msg, args...)
	}
}

// AuditLogger exposes the underlying audit logger (nil if auditing disabled).
func (m *Manager) AuditLogger() *logging.AuditLogger {
	return m.audit
}

// NewManager creates a vault manager with the specified mode.
// Options are for orthogonal concerns (logging, paths) only.
func NewManager(mode Mode, opts ...Option) (*Manager, error) {
	cfg := applyOptions(opts...)

	switch m := mode.(type) {
	case auto:
		return newAuto(cfg)
	case softwareMode:
		return newSoftware(m.store, cfg)
	case hardwareMode:
		return newHardware(m.kind, cfg)
	case provided:
		return newProvided(m.identity, cfg)
	default:
		// Unreachable with sealed interface, but satisfies compiler
		return nil, fmt.Errorf("unknown mode type: %T", mode)
	}
}

// newAuto detects configuration from existing files.
// Mode is detected from filesystem (age.key.enc vs piv.json), not config.
func newAuto(cfg *managerConfig) (*Manager, error) {
	// Detect mode from filesystem (single source of truth)
	detectedMode, err := DetectSecurityMode(cfg.paths.ConfigDir)
	if err != nil {
		// Check for legacy key to provide helpful error
		if errors.Is(err, ErrNotInitialized) {
			keyPath := cfg.paths.AgeKeyFile
			if _, statErr := os.Stat(keyPath); statErr == nil {
				return nil, fmt.Errorf("legacy unprotected key found: run 'nssh self init' to migrate to protected storage")
			}
			return nil, fmt.Errorf("no vault initialized: run 'nssh self init'")
		}
		if errors.Is(err, ErrAmbiguousMode) {
			return nil, fmt.Errorf("ambiguous state: both software and hardware keystores found. Run 'nssh self rekey --rollback' to recover from a failed mode switch, or 'nssh self reset' to start fresh")
		}
		return nil, err
	}

	// Load config for audit logger settings and software store config
	appCfg, err := config.LoadDefault()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	var auditLogger *logging.AuditLogger
	if appCfg.Logging.Audit.Enabled {
		auditLogger, _ = createAuditLogger(appCfg, cfg.paths)
	}

	// Override with explicit options
	if cfg.audit != nil {
		auditLogger = cfg.audit
	}
	maxBackups := appCfg.Logging.Audit.MaxBackupFiles
	if cfg.maxBackups > 0 {
		maxBackups = cfg.maxBackups
	}

	switch detectedMode {
	case string(mode.PIV):
		// Hardware PIV mode - agent handles decryption
		return &Manager{
			credentialPath: cfg.paths.CredentialsFile,
			configDir:      cfg.paths.ConfigDir,
			backupDir:      cfg.paths.BackupDir,
			maxBackups:     maxBackups,
			audit:          auditLogger,
			modeType:       hardwareMode{kind: hardware.PIV},
			sessionDeps:    cfg.sessionDeps,
		}, nil

	case string(mode.Software):
		// Software mode - create passphrase store
		ks, ksAudit, err := newSoftwareStore(appCfg, cfg.paths)
		if err != nil {
			return nil, err
		}
		if ksAudit != nil {
			auditLogger = ksAudit
		}
		return &Manager{
			credentialPath: cfg.paths.CredentialsFile,
			configDir:      cfg.paths.ConfigDir,
			backupDir:      cfg.paths.BackupDir,
			maxBackups:     maxBackups,
			store:          ks,
			audit:          auditLogger,
			modeType:       softwareMode{store: ks},
			sessionDeps:    cfg.sessionDeps,
		}, nil

	default:
		return nil, fmt.Errorf("unknown mode: %s", detectedMode)
	}
}

// newSoftware creates a manager using a software store.
func newSoftware(store software.Store, cfg *managerConfig) (*Manager, error) {
	return &Manager{
		credentialPath: cfg.paths.CredentialsFile,
		configDir:      cfg.paths.ConfigDir,
		backupDir:      cfg.paths.BackupDir,
		maxBackups:     cfg.maxBackups,
		store:          store,
		audit:          cfg.audit,
		modeType:       softwareMode{store: store},
		sessionDeps:    cfg.sessionDeps,
	}, nil
}

// newHardware creates a manager for hardware-backed keys.
func newHardware(kind hardware.Kind, cfg *managerConfig) (*Manager, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("unknown hardware kind: %s", kind)
	}

	// Verify hardware mode is initialized
	switch kind {
	case hardware.PIV:
		pivPath := filepath.Join(cfg.paths.ConfigDir, "piv.json")
		pubKeyPath := filepath.Join(cfg.paths.ConfigDir, "age.pub")
		if _, err := os.Stat(pivPath); err != nil {
			return nil, fmt.Errorf("PIV mode not initialized: run 'nssh self init --mode piv'")
		}
		if _, err := os.Stat(pubKeyPath); err != nil {
			return nil, fmt.Errorf("PIV mode not initialized: missing age.pub")
		}
	case hardware.FIDO2:
		return nil, errors.New("FIDO2 mode not yet implemented")
	case hardware.SecureEnclave:
		return nil, errors.New("SecureEnclave mode not yet implemented")
	}

	return &Manager{
		credentialPath: cfg.paths.CredentialsFile,
		configDir:      cfg.paths.ConfigDir,
		backupDir:      cfg.paths.BackupDir,
		maxBackups:     cfg.maxBackups,
		audit:          cfg.audit,
		modeType:       hardwareMode{kind: kind},
		sessionDeps:    cfg.sessionDeps,
	}, nil
}

// newProvided creates a manager with a pre-decrypted identity.
func newProvided(identity *age.X25519Identity, cfg *managerConfig) (*Manager, error) {
	return &Manager{
		credentialPath: cfg.paths.CredentialsFile,
		configDir:      cfg.paths.ConfigDir,
		backupDir:      cfg.paths.BackupDir,
		maxBackups:     cfg.maxBackups,
		identities:     []age.Identity{identity},
		recipient:      identity.Recipient(),
		audit:          cfg.audit,
		modeType:       provided{identity: identity},
		sessionDeps:    cfg.sessionDeps,
	}, nil
}

// newSoftwareStore creates a software Store based on config settings.
// Called only when DetectSecurityMode has already confirmed software mode.
func newSoftwareStore(cfg *config.Config, paths *config.Paths) (software.Store, *logging.AuditLogger, error) {
	// Create audit logger for security events
	var logger *slog.Logger
	var auditLogger *logging.AuditLogger
	if cfg.Logging.Audit.Enabled {
		var err error
		auditLogger, err = createAuditLogger(cfg, paths)
		if err != nil {
			slog.Warn("failed to create audit logger", "err", err)
			// Continue without audit logging
		} else {
			logger = auditLogger.Logger
		}
	}

	ksCfg := software.Config{
		ConfigDir:           paths.ConfigDir,
		DataDir:             paths.DataDir,
		StateDir:            paths.StateDir,
		ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
		PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
		Logger:              logger,
		LockoutThreshold:    cfg.Agent.Security.Software.LockoutThreshold,
		LockoutDuration:     cfg.Agent.Security.Software.LockoutDuration.Duration(),
		MaxLockoutDuration:  cfg.Agent.Security.Software.MaxLockoutDuration.Duration(),
	}

	ks, err := software.New(ksCfg)
	return ks, auditLogger, err
}

// createAuditLogger creates an audit logger based on config.
func createAuditLogger(cfg *config.Config, paths *config.Paths) (*logging.AuditLogger, error) {
	return logging.NewAuditLogger(slog.LevelError, &cfg.Logging.Audit, paths.StateDir)
}

// loadIdentities loads the age identity from software.Store.
func (m *Manager) loadIdentities() error {
	if m.identities != nil {
		return nil
	}

	if m.store == nil {
		return errors.New("no software store available")
	}

	identity, err := m.store.Identity()
	if err != nil {
		return err
	}
	m.identities = []age.Identity{identity}
	if x, ok := identity.(*age.X25519Identity); ok {
		m.recipient = x.Recipient()
	}
	if m.recipient == nil {
		return fmt.Errorf("no valid age recipient found")
	}
	return nil
}

// NeedsUnlock returns true if the vault requires unlock before use.
// Returns false if a session is already available or identities are pre-loaded.
func (m *Manager) NeedsUnlock() bool {
	switch m.state() {
	case stateProvided:
		return false
	default:
		// Use injected session availability check
		if m.sessionDeps.Available != nil {
			ctx := m.sessionDeps.BaseContext
			if ctx == nil {
				ctx = context.Background()
			}
			return !m.sessionDeps.Available(ctx)
		}
		return true // No session deps means always needs unlock
	}
}

// Lock terminates the session and clears cached state.
func (m *Manager) Lock() error {
	// Clear cached identities
	m.identities = nil
	m.recipient = nil

	// Use injected lock function if available
	if m.sessionDeps.Lock != nil {
		ctx := m.sessionDeps.BaseContext
		if ctx == nil {
			ctx = context.Background()
		}
		return m.sessionDeps.Lock(ctx)
	}
	return nil
}

// GetRecipient returns the age recipient for encryption.
// Does NOT require unlock - reads from cached value, store, or age.pub file.
func (m *Manager) GetRecipient() (age.Recipient, error) {
	// Provided mode: derive from pre-loaded identity
	if len(m.identities) > 0 {
		if x, ok := m.identities[0].(*age.X25519Identity); ok {
			return x.Recipient(), nil
		}
	}

	// Cached recipient
	if m.recipient != nil {
		return m.recipient, nil
	}

	// Software mode with store
	if m.store != nil {
		return m.store.Recipient()
	}

	// Hardware modes: read from age.pub
	return m.loadRecipientFromFile()
}

// loadRecipientFromFile reads and caches the recipient from age.pub.
// Used by hardware modes (PIV, FIDO2, SecureEnclave) and as fallback.
func (m *Manager) loadRecipientFromFile() (age.Recipient, error) {
	pubPath := filepath.Join(m.configDir, "age.pub")
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}

	recipient, err := age.ParseX25519Recipient(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	m.recipient = recipient // Cache it
	return recipient, nil
}

// GetIdentity returns the current age identity for decryption.
// Requires the vault to be unlocked.
func (m *Manager) GetIdentity() (age.Identity, error) {
	if err := m.loadIdentities(); err != nil {
		return nil, err
	}
	if len(m.identities) == 0 {
		return nil, fmt.Errorf("no identity available")
	}
	return m.identities[0], nil
}

// CredentialPath returns the path to the credential file.
func (m *Manager) CredentialPath() string {
	return m.credentialPath
}

// ConfigDir returns the config directory path.
func (m *Manager) ConfigDir() string {
	return m.configDir
}

// SoftwareStore returns the software store for CLI unlock operations.
// Returns nil for hardware modes.
func (m *Manager) SoftwareStore() software.Store {
	return m.store
}
