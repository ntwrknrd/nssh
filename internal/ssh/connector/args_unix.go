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
	options, command := splitSSHArgs(c.sshArgs)
	args := make([]string, 0, len(options)+len(command)+8)
	if len(command) == 0 && !hasExplicitTTYOption(options) {
		args = append(args, "-tt")
	}
	args = append(args, RenderSSHOptions(c.sshOptions, c.sshVerbosity)...)

	// Build target (user@host or just host). OpenSSH config is disabled with
	// -F none; host-specific settings are already rendered into argv.
	target := c.hostname
	if c.username != "" {
		target = fmt.Sprintf("%s@%s", c.username, target)
	}

	// Add connection timeout if configured
	if c.timeouts != nil && c.timeouts.Timeout.Duration() > 0 {
		args = append(args,
			"-o", fmt.Sprintf("ConnectTimeout=%d", int(c.timeouts.Timeout.Duration().Seconds())),
		)
	}

	if c.resolvedPort != "" && c.resolvedPort != "22" && c.parsePortFromSSHArgs() == "" {
		args = append(args, "-p", c.resolvedPort)
	}

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

func splitSSHArgs(args []string) (options, command []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func hasExplicitTTYOption(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-t", "-tt", "-T":
			return true
		}
	}
	return false
}
