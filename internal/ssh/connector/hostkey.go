package connector

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"
)

// HostKeyAction represents user's choice for host key handling.
type HostKeyAction int

// Host key actions for interactive verification prompts.
const (
	HostKeyReject       HostKeyAction = iota // Reject connection
	HostKeyAcceptOnce                        // Accept for this session only (temp known_hosts)
	HostKeyAcceptAlways                      // Answer "yes" - SSH adds to real known_hosts
)

// HostKeyResult indicates how the caller should proceed after host key handling.
type HostKeyResult int

// Host key result codes indicating how the caller should proceed.
const (
	HostKeyResultContinue HostKeyResult = iota // Connection proceeds normally
	HostKeyResultAbort                         // User rejected, exit
	HostKeyResultRestart                       // Need to restart with temp known_hosts
)

// HostKeyPrompt describes a host-key verification prompt.
type HostKeyPrompt struct {
	Host        string
	KeyType     string
	Fingerprint string
	Changed     bool
	Stdin       io.Reader
}

// HostKeyPromptFunc resolves an interactive host-key verification prompt.
type HostKeyPromptFunc func(HostKeyPrompt) HostKeyAction

// handleHostKeyPrompt detects host key prompts and shows interactive menu.
// Returns (handled, result) where handled indicates if a prompt was detected,
// and result indicates how the caller should proceed.
func (c *Connector) handleHostKeyPrompt(output []byte) (bool, HostKeyResult) {
	if matchHostKeyChanged(output) {
		// SECURITY: Changed host key is a potential MITM attack
		keyType, fingerprint := extractFingerprint(output)
		action := c.promptHostKeyAction(HostKeyPrompt{
			Host:        c.hostname,
			KeyType:     keyType,
			Fingerprint: fingerprint,
			Changed:     true,
			Stdin:       c.stdinReaderLocked(),
		})
		return true, c.respondToHostKey(action)
	}

	if matchUnknownHost(output) {
		// First connection - show fingerprint and prompt
		keyType, fingerprint := extractFingerprint(output)
		action := c.promptHostKeyAction(HostKeyPrompt{
			Host:        c.hostname,
			KeyType:     keyType,
			Fingerprint: fingerprint,
			Changed:     false,
			Stdin:       c.stdinReaderLocked(),
		})
		if action == HostKeyAcceptOnce && keyType != "" && fingerprint != "" {
			c.pinnedHostKey = &pinnedKey{typeName: keyType, fingerprint: fingerprint}
		}
		return true, c.respondToHostKey(action)
	}

	return false, HostKeyResultContinue
}

// promptHostKeyAction resolves an interactive host-key action.
func (c *Connector) promptHostKeyAction(prompt HostKeyPrompt) HostKeyAction {
	// Check if we have an interactive terminal
	if !isTerminal(os.Stdin.Fd()) {
		// Non-interactive: check if user explicitly allowed auto-acceptance
		if c.hasPermissiveStrictHostKeyChecking() {
			slog.Debug("non-interactive mode with permissive StrictHostKeyChecking, auto-accepting")
			return HostKeyAcceptAlways
		}
		// Non-interactive: default to reject for safety
		return HostKeyReject
	}

	if c.hostKeyPrompt == nil {
		return HostKeyReject
	}

	c.restoreTerminal()
	defer c.setRawMode()
	return c.hostKeyPrompt(prompt)
}

// respondToHostKey writes the appropriate response to the PTY.
func (c *Connector) respondToHostKey(action HostKeyAction) HostKeyResult {
	switch action {
	case HostKeyReject:
		if _, err := c.ptyFile.WriteString("no\n"); err != nil {
			slog.Debug("failed to write host key rejection", "err", err)
		}
		return HostKeyResultAbort

	case HostKeyAcceptOnce:
		// AcceptOnce requires restarting SSH with a temp known_hosts file.
		// We can't change SSH's behavior mid-connection, so we:
		// 1. Answer "no" to abort current connection
		// 2. Signal caller to restart with -o UserKnownHostsFile=<temp>
		// Note: User will briefly see "Host key verification failed." before restart.
		if _, err := c.ptyFile.WriteString("no\n"); err != nil {
			slog.Debug("failed to write host key response", "err", err)
		}
		c.useTemporaryKnownHosts = true
		return HostKeyResultRestart

	case HostKeyAcceptAlways:
		if _, err := c.ptyFile.WriteString("yes\n"); err != nil {
			slog.Debug("failed to write host key acceptance", "err", err)
		}
		return HostKeyResultContinue
	}

	return HostKeyResultContinue
}

// isTerminal reports whether fd is a terminal.
func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// hasPermissiveStrictHostKeyChecking checks if user passed SSH options that
// explicitly allow auto-accepting new host keys (e.g., -o StrictHostKeyChecking=no
// or -o StrictHostKeyChecking=accept-new). This enables non-interactive/CI usage.
func (c *Connector) hasPermissiveStrictHostKeyChecking() bool {
	for i := 0; i < len(c.sshArgs); i++ {
		arg := c.sshArgs[i]
		if arg == "--" {
			break // Stop at command separator
		}
		// Handle -o StrictHostKeyChecking=value (with space)
		if arg == "-o" && i+1 < len(c.sshArgs) {
			next := strings.ToLower(c.sshArgs[i+1])
			if strings.HasPrefix(next, "stricthostkeychecking=") {
				val := strings.TrimPrefix(next, "stricthostkeychecking=")
				if val == "no" || val == "accept-new" {
					return true
				}
			}
			i++ // Skip the value we just checked
		}
		// Handle -oStrictHostKeyChecking=value (no space)
		lowerArg := strings.ToLower(arg)
		if strings.HasPrefix(lowerArg, "-ostricthostkeychecking=") {
			val := strings.TrimPrefix(lowerArg, "-ostricthostkeychecking=")
			if val == "no" || val == "accept-new" {
				return true
			}
		}
	}
	return false
}
