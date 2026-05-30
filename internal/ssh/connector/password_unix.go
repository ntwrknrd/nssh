//go:build unix

package connector

import (
	"bytes"
	"fmt"
	"log/slog"
	"time"
)

// injectPassword writes the password to the PTY.
func (c *Connector) injectPassword() error {
	if c.password == nil {
		return fmt.Errorf("no password configured")
	}

	err := c.password.Use(func(pw []byte) error {
		// Write password + newline to PTY master
		if _, err := c.ptyFile.Write(pw); err != nil {
			return err
		}
		_, err := c.ptyFile.Write([]byte{'\n'})
		return err
	})

	if err == nil {
		c.passwordSentAt = time.Now()
	}
	return err
}

// filterOutput scrubs sensitive data from PTY output before display.
// If suppressPrompt is true, removes password prompt lines from output.
func (c *Connector) filterOutput(data []byte, suppressPrompt bool) []byte {
	result := data

	// Suppress password prompt line if requested
	if suppressPrompt {
		result = removePasswordPromptLines(result)
	}

	// Only filter password echo if we recently sent a password
	if c.recentPasswordSent() && c.password != nil {
		// Strip leading newlines (echo from password submission)
		result = bytes.TrimLeft(result, "\r\n")

		// Check if password appears in output (echo from misconfigured server)
		if err := c.password.Use(func(pw []byte) error {
			if bytes.Contains(result, pw) {
				result = bytes.ReplaceAll(result, pw, []byte("********"))
			}
			return nil
		}); err != nil {
			slog.Debug("failed to access password for filtering", "err", err)
		}
	}

	return result
}

// removePasswordPromptLines filters out lines containing password prompts.
func removePasswordPromptLines(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	var filtered [][]byte

	for _, line := range lines {
		if !matchPasswordPrompt(line) {
			filtered = append(filtered, line)
		}
	}

	// If we filtered everything, return empty
	if len(filtered) == 0 {
		return nil
	}

	return bytes.Join(filtered, []byte("\n"))
}

// recentPasswordSent returns true if password was sent in the last filter window.
func (c *Connector) recentPasswordSent() bool {
	if !c.passwordSent {
		return false
	}
	return time.Since(c.passwordSentAt) < DefaultPasswordFilterWindow
}
