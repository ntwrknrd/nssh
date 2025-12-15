// Package agent implements the nssh credential agent daemon.
//
// The agent is a background process that holds decrypted credentials in memory,
// providing secure credential access without requiring repeated passphrase entry.
// It communicates with nssh clients via a Unix domain socket using a JSON-based
// protocol.
//
// # Lifecycle
//
// The agent is spawned by [Spawn] or [SpawnPIV] when the vault is unlocked,
// and terminates automatically based on configurable timeouts:
//
//   - Idle timeout: terminates after period of inactivity (default 1h)
//   - Max lifetime: hard cap regardless of activity (default 24h)
//   - Manual lock: terminated via "nssh lock" command
//
// # Security Modes
//
// The agent supports multiple credential protection backends via the [Provider]
// interface:
//
//   - Software mode: passphrase-protected age identity with scrypt KDF
//   - PIV mode: YubiKey hardware-backed decryption (requires CGO build)
//
// # Protocol
//
// Clients communicate via JSON messages over the Unix socket. Operations include:
//
//   - OpDecrypt: decrypt age-encrypted credential data
//   - OpStatus: query agent status and remaining lifetime
//   - OpLock: terminate the agent session
//
// # Background Tasks
//
// The agent runs background goroutines for:
//
//   - Recording archival: monitors live session recordings and archives them
//   - Connection handling: serves client requests with concurrency limits
//
// # Example
//
// Typical usage pattern (handled by the unlock command):
//
//	// Spawn agent with software provider
//	if err := agent.Spawn(identity); err != nil {
//	    return err
//	}
//
//	// Later, check if agent is running
//	if agent.IsRunning() {
//	    // Use agent for decryption via session package
//	}
package agent
