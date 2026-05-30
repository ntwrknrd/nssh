//go:build unix

package connector

import (
	"fmt"
	"log/slog"
	"os"
)

// buildSSHArgs constructs SSH arguments, adding temp known_hosts if needed.
// SSH syntax: ssh [options] hostname [command]
// The -- separator marks the start of the remote command, which must come AFTER hostname.
func (c *Connector) buildSSHArgs() ([]string, error) {
	args := []string{"-tt"} // Force PTY allocation

	// Build target (user@host or just host)
	// Use the Host identifier/alias so SSH config Host pattern matching works correctly.
	// This ensures host-specific settings (KexAlgorithms, Ciphers, etc.) are applied.
	// Users who want consistent ControlMaster sockets can enable CanonicalizeHostname.
	target := c.hostname
	if c.username != "" {
		target = fmt.Sprintf("%s@%s", c.username, target)
	}

	// Split sshArgs into options (before --) and command (-- and after)
	// SSH requires: ssh [options] hostname [-- command]
	var options, command []string
	separatorIdx := -1
	for i, arg := range c.sshArgs {
		if arg == "--" {
			separatorIdx = i
			break
		}
	}

	if separatorIdx >= 0 {
		options = c.sshArgs[:separatorIdx]
		command = c.sshArgs[separatorIdx:] // Includes --
	} else {
		options = c.sshArgs
	}

	// Add connection timeout if configured
	if c.timeouts != nil && c.timeouts.Timeout.Duration() > 0 {
		args = append(args,
			"-o", fmt.Sprintf("ConnectTimeout=%d", int(c.timeouts.Timeout.Duration().Seconds())),
		)
	}

	// Port is handled by SSH config Host matching or user's -p flag in sshArgs.
	// No need to add it explicitly when using the Host identifier as target.

	// Add SSH options
	args = append(args, options...)

	// Add temp known_hosts options if needed
	if c.useTemporaryKnownHosts && c.tempKnownHosts == "" {
		// Create temp file that will be discarded after session
		f, err := os.CreateTemp("", "nssh-known-hosts-*")
		if err != nil {
			slog.Warn("failed to create temp known_hosts, using real file", "err", err)
		} else {
			c.tempKnownHosts = f.Name()
			if err := c.populateTempKnownHosts(); err != nil {
				return nil, err
			}
			if err := f.Close(); err != nil {
				slog.Debug("failed to close temp known_hosts file", "err", err)
			}
			args = append(args,
				"-o", "UserKnownHostsFile="+c.tempKnownHosts,
				"-o", "StrictHostKeyChecking=yes",
			)
		}
	}

	// Add target hostname
	args = append(args, target)

	// Add remote command (if any)
	if len(command) > 0 {
		args = append(args, command...)
	}

	return args, nil
}
