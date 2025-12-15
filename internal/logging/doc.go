// Package logging provides audit logging for security events.
//
// This package implements dual-output logging: stderr for immediate feedback
// and a persistent audit file for security compliance and debugging.
//
// # Audit Log
//
// The audit log records security-relevant events:
//   - SSH connection attempts and results
//   - Vault unlock/lock operations
//   - Credential access
//   - Agent lifecycle events
//
// The audit file is located at:
//
//	$XDG_STATE_HOME/nssh/audit.log  (default: ~/.local/state/nssh/audit.log)
//
// # Log Rotation
//
// Automatic rotation occurs when the audit file exceeds the configured size
// (default 10MB). Rotated files are named audit.log.1, audit.log.2, etc.,
// with a maximum of 3 rotated files kept.
//
// # Configuration
//
// Audit logging is configured in config.toml:
//
//	[logging.audit]
//	enabled = true
//	max_size = "10MB"
//
// # Usage
//
//	logger, err := logging.NewAuditLogger(slog.LevelInfo, &cfg.Logging.Audit, stateDir)
//	if err != nil {
//	    return err
//	}
//	defer logger.Close()
//
//	logger.Info("ssh_connect", "host", hostname, "user", username)
package logging
