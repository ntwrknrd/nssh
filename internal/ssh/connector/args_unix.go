//go:build unix

package connector

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ntwrknrd/nssh/internal/ssh/sshargs"
)

// buildSSHArgs constructs SSH arguments, adding temp known_hosts if needed.
// SSH syntax: ssh [options] -- hostname [command]. The terminator keeps the
// generated target positional even when it begins with a dash.
func (c *Connector) buildSSHArgs() ([]string, error) {
	options, command := splitSSHArgs(c.sshArgs)
	pinnedOptions, options := SplitPinnedHostKeyOptions(options)
	args := make([]string, 0, len(options)+len(command)+8)
	if len(command) == 0 && !hasExplicitTTYOption(options) {
		args = append(args, "-tt")
	}

	// Build target (user@host or just host). OpenSSH config is disabled with
	// -F none; host-specific settings are already rendered into argv.
	target := c.hostname
	if c.username != "" {
		target = fmt.Sprintf("%s@%s", c.username, target)
	}

	if c.useTemporaryKnownHosts && c.tempKnownHosts == "" {
		f, err := os.CreateTemp("", "nssh-known-hosts-*")
		if err != nil {
			slog.Warn("failed to create temp known_hosts, using real file", "err", err)
		} else {
			c.tempKnownHosts = f.Name()
			if err := c.populateTempKnownHosts(); err != nil {
				_ = f.Close()
				return nil, err
			}
			if err := f.Close(); err != nil {
				slog.Debug("failed to close temp known_hosts file", "err", err)
			}
		}
	}

	var enforcedOptions []string
	if c.tempKnownHosts != "" && c.pinnedHostKey != nil && c.pinnedHostKey.hostKeyAlgorithms != "" {
		enforcedOptions = append(enforcedOptions, "-o", "HostKeyAlgorithms="+c.pinnedHostKey.hostKeyAlgorithms)
	}
	if c.tempKnownHosts != "" {
		enforcedOptions = append(enforcedOptions,
			"-o", "UserKnownHostsFile="+c.tempKnownHosts,
			"-o", "StrictHostKeyChecking=yes",
		)
	}
	enforcedOptions = append(enforcedOptions, pinnedOptions...)
	args = append(args, ComposeSSHOptions(SSHOptionPlan{
		Enforced:     enforcedOptions,
		Runtime:      options,
		Resolved:     c.sshOptions,
		SSHVerbosity: c.sshVerbosity,
	})...)

	// Add connection timeout if configured
	if c.timeouts != nil && c.timeouts.Timeout.Duration() > 0 && EffectiveSSHOption(args, "ConnectTimeout") == "" {
		args = append(args,
			"-o", fmt.Sprintf("ConnectTimeout=%d", int(c.timeouts.Timeout.Duration().Seconds())),
		)
	}

	if c.resolvedPort != "" && c.resolvedPort != "22" && EffectiveSSHOption(args, "Port") == "" {
		args = append(args, "-p", c.resolvedPort)
	}

	if hasAskpassEnv(c.env) && EffectiveSSHOption(args, "NumberOfPasswordPrompts") == "" {
		args = append(args, "-o", "NumberOfPasswordPrompts=1")
	}

	// Terminate OpenSSH option parsing before the generated target.
	args = append(args, "--", target)
	if len(command) > 0 {
		args = append(args, command...)
	}

	return args, nil
}

// SplitPinnedHostKeyOptions separates the strict options produced by an
// operator-approved host-key preparation. Callers must render pinned before
// ordinary SSH configuration because OpenSSH keeps the first value it sees.
func SplitPinnedHostKeyOptions(options []string) (pinned, rest []string) {
	hasKnownHosts := false
	hasStrictChecking := false
	i := 0
	for i+1 < len(options) && options[i] == "-o" {
		key, value, ok := splitOpenSSHOption(options[i+1])
		if !ok || !isPinnedHostKeyOption(key) {
			break
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "userknownhostsfile":
			hasKnownHosts = strings.TrimSpace(value) != ""
		case "stricthostkeychecking":
			hasStrictChecking = strings.EqualFold(strings.TrimSpace(value), "yes")
		}
		pinned = append(pinned, options[i], options[i+1])
		i += 2
	}
	if !hasKnownHosts || !hasStrictChecking {
		return nil, options
	}
	return pinned, options[i:]
}

func isPinnedHostKeyOption(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "hostkeyalgorithms", "userknownhostsfile", "stricthostkeychecking":
		return true
	default:
		return false
	}
}

func hasAskpassEnv(env []string) bool {
	for _, entry := range env {
		if entry == "SSH_ASKPASS" || strings.HasPrefix(entry, "SSH_ASKPASS=") {
			return true
		}
	}
	return false
}

func splitSSHArgs(args []string) (options, command []string) {
	return sshargs.Split(args)
}

func hasExplicitTTYOption(args []string) bool {
	requested := false
	sshargs.Walk(args, func(option sshargs.Option) bool {
		if option.Name == 't' || option.Name == 'T' {
			requested = true
			return false
		}
		return true
	})
	return requested
}
