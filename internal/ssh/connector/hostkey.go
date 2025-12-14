package connector

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ntwrknrd/nssh/internal/ui"
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

// handleHostKeyPrompt detects host key prompts and shows interactive menu.
// Returns (handled, result) where handled indicates if a prompt was detected,
// and result indicates how the caller should proceed.
func (c *Connector) handleHostKeyPrompt(output []byte) (bool, HostKeyResult) {
	if matchHostKeyChanged(output) {
		// SECURITY: Changed host key is a potential MITM attack
		c.showHostKeyChangedWarning(output)
		action := c.promptHostKeyAction(true)
		return true, c.respondToHostKey(action)
	}

	if matchUnknownHost(output) {
		// First connection - show fingerprint and prompt
		keyType, fingerprint := extractFingerprint(output)
		c.showNewHostInfo(keyType, fingerprint)
		action := c.promptHostKeyAction(false)
		if action == HostKeyAcceptOnce && keyType != "" && fingerprint != "" {
			c.pinnedHostKey = &pinnedKey{typeName: keyType, fingerprint: fingerprint}
		}
		return true, c.respondToHostKey(action)
	}

	return false, HostKeyResultContinue
}

// promptHostKeyAction shows interactive menu using huh.
func (c *Connector) promptHostKeyAction(dangerMode bool) HostKeyAction {
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

	var options []string
	if dangerMode {
		// For changed keys, make rejection the obvious choice
		options = []string{
			"Reject - possible attack! (recommended)",
			"Accept anyway (dangerous)",
		}
	} else {
		options = []string{
			"Reject (disconnect)",
			"Accept once (this session only)",
			"Accept always (add to known_hosts)",
		}
	}

	defer c.setRawMode()

	idx, err := ui.SelectIndex("Host key verification", options, c.GetStdinReader())
	if err != nil || idx < 0 {
		// On error or cancel, default to reject
		return HostKeyReject
	}

	if dangerMode {
		if idx == 0 {
			return HostKeyReject
		}
		return HostKeyAcceptAlways
	}

	switch idx {
	case 0:
		return HostKeyReject
	case 1:
		return HostKeyAcceptOnce
	default:
		return HostKeyAcceptAlways
	}
}

// showNewHostInfo displays information about a new host's key.
func (c *Connector) showNewHostInfo(keyType, fingerprint string) {
	c.restoreTerminal()
	defer c.setRawMode()

	fp := fingerprint
	if keyType != "" && fingerprint != "" {
		fp = fmt.Sprintf("%s %s", keyType, fingerprint)
	}
	if fp == "" {
		fp = "(fingerprint not available)"
	}

	fmt.Println()
	panel := ui.NewPanel("New Host").
		Row("Host:", c.hostname).
		Row("Fingerprint:", fp).
		WithFooter("")
	panel.Print()
	fmt.Println()
	fmt.Println(ui.Gray("This is the first time connecting to this host."))
	fmt.Println()
}

// showHostKeyChangedWarning displays a prominent warning about changed host keys.
func (c *Connector) showHostKeyChangedWarning(output []byte) {
	c.restoreTerminal()
	defer c.setRawMode()

	keyType, fingerprint := extractFingerprint(output)
	fp := ""
	if keyType != "" && fingerprint != "" {
		fp = fmt.Sprintf("%s %s", keyType, fingerprint)
	}

	fmt.Println()
	fmt.Println(ui.Red("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"))
	fmt.Println(ui.Red("@    WARNING: REMOTE HOST IDENTIFICATION HAS    @"))
	fmt.Println(ui.Red("@              CHANGED!                         @"))
	fmt.Println(ui.Red("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"))
	fmt.Println()
	fmt.Println(ui.Yellow("IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!"))
	fmt.Println(ui.Yellow("Someone could be eavesdropping on you right now (man-in-the-middle attack)!"))
	fmt.Println()

	panel := ui.NewPanel("").
		Row("Host:", c.hostname).
		Row("New fingerprint:", fp).
		WithWarning()
	panel.Print()

	fmt.Println()
	fmt.Println("The host key has changed. If this is expected (server reinstall,")
	fmt.Println("key rotation), you may proceed. Otherwise, DO NOT CONTINUE.")
	fmt.Println()
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
