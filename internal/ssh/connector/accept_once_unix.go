//go:build unix

package connector

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
)

// pinnedKey stores the host key type and fingerprint observed during the
// initial prompt. Used to pin the AcceptOnce retry to the exact key.
type pinnedKey struct {
	typeName    string // e.g., "ED25519"
	fingerprint string // e.g., "SHA256:abcd..."
}

// populateTempKnownHosts writes exactly one pinned host key (captured during
// AcceptOnce) into the temporary known_hosts file. This prevents a key-swap
// between the initial prompt and the restarted connection.
func (c *Connector) populateTempKnownHosts() error {
	if c.tempKnownHosts == "" {
		return fmt.Errorf("temp known_hosts path not set")
	}
	if c.pinnedHostKey == nil || c.pinnedHostKey.fingerprint == "" {
		return fmt.Errorf("cannot use Accept once: no pinned host key available")
	}

	keyType := strings.ToLower(c.pinnedHostKey.typeName)
	if keyType == "" {
		return fmt.Errorf("cannot use Accept once: missing key type")
	}

	// Determine real host/port for keyscan
	hostToScan := c.resolvedHost
	if hostToScan == "" {
		hostToScan = c.hostname
	}

	port := c.resolvedPort
	if cliPort := c.parsePortFromSSHArgs(); cliPort != "" {
		port = cliPort
	}
	if port == "" {
		port = "22"
	}

	args := []string{"-t", keyType}
	if port != "22" {
		args = append(args, "-p", port)
	}
	args = append(args, hostToScan)

	output, err := exec.Command("ssh-keyscan", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keyscan failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pubKey, _, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(line))
		if parseErr != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		if fp != c.pinnedHostKey.fingerprint {
			continue
		}
		if err := os.WriteFile(c.tempKnownHosts, []byte(line+"\n"), 0600); err != nil {
			return fmt.Errorf("write temp known_hosts: %w", err)
		}
		return nil
	}

	return fmt.Errorf("failed to pin host key: fingerprint mismatch after ssh-keyscan")
}

// parsePortFromSSHArgs extracts an explicit port from sshArgs (-p or -o Port=...)
// before the -- separator. Returns empty string if none found.
func (c *Connector) parsePortFromSSHArgs() string {
	for i := 0; i < len(c.sshArgs); i++ {
		arg := c.sshArgs[i]
		if arg == "--" {
			break
		}
		if arg == "-p" && i+1 < len(c.sshArgs) {
			return c.sshArgs[i+1]
		}
		if strings.HasPrefix(arg, "-p") && len(arg) > 2 {
			return arg[2:]
		}
		if arg == "-o" && i+1 < len(c.sshArgs) {
			next := strings.ToLower(c.sshArgs[i+1])
			if strings.HasPrefix(next, "port=") {
				return c.sshArgs[i+1][5:]
			}
			i++
		}
	}
	return ""
}
