//go:build unix

// Package connector provides PTY-based SSH connection management with
// credential injection and session recording support.
//
// The connector spawns SSH as a child process in a pseudo-terminal, enabling
// interactive sessions with automatic password injection and host key handling.
//
// # Connection Flow
//
// The [Connector] handles the complete SSH session lifecycle:
//
//  1. Spawns ssh in a PTY for interactive I/O
//  2. Detects and responds to password prompts
//  3. Handles host key verification (accept-once mode optional)
//  4. Proxies terminal I/O between user and SSH process
//  5. Propagates window resize events (SIGWINCH)
//
// # Credential Injection
//
// When credentials are provided, the connector monitors SSH output for
// password prompts and injects the password automatically. Passwords are
// held in secure memory via the [secret] package and wiped after use.
//
// # Host Key Handling
//
// The connector supports an "accept-once" mode where unknown host keys are
// automatically accepted for the current session only, without persisting
// to known_hosts. This is useful for temporary or ephemeral connections.
//
// # Recording Integration
//
// Sessions can be recorded via asciinema. Use [MaybeWrapWithRecording] to
// check recording configuration and spawn a recording wrapper if enabled.
// The recording wrapper re-executes nssh under asciinema.
//
// # Connection Testing
//
// The [TestConnection] function performs a non-interactive connection test
// to diagnose connectivity and compatibility issues. This is used by the
// automatic compatibility fix system.
//
// # Example
//
//	conn := connector.NewConnector(hostname, username, password, sshArgs)
//	conn.SetTimeouts(&cfg.SSH.Connection)
//	if err := conn.Run(ctx); err != nil {
//	    // Handle connection error
//	}
package connector
