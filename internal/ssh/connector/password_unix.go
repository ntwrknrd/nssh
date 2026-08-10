//go:build unix

package connector

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ntwrknrd/nssh/internal/secret"
)

func passwordPromptInjectionEnabled() bool {
	return false
}

func (c *Connector) resolvePassword(ctx context.Context) (*secret.Secret, error) {
	c.passwordMu.Lock()
	if c.password != nil {
		pw := c.password
		c.passwordMu.Unlock()
		return pw, nil
	}
	// Resolve only after OpenSSH asks for a password. Existing control sessions
	// can complete without prompting, and prefetching would touch credentials
	// the connection never needs.
	if c.passwordResolver == nil {
		c.passwordMu.Unlock()
		return nil, nil
	}
	resolver := c.passwordResolver
	c.passwordMu.Unlock()

	lookupTimer := StartTiming(TimingCredentialLookupLazy)
	pw, err := resolver(ctx)
	if err != nil {
		return nil, err
	}
	lookupTimer.Emit()
	c.passwordMu.Lock()
	c.password = pw
	c.passwordMu.Unlock()
	return pw, nil
}

// injectPassword writes the password to the PTY.
func (c *Connector) injectPassword(ctx context.Context) error {
	password, err := c.resolvePassword(ctx)
	if err != nil {
		return err
	}
	if password == nil {
		return fmt.Errorf("no password configured")
	}

	writeTimer := StartTiming(TimingPasswordWrite)
	err = password.Use(func(pw []byte) error {
		// Write password + newline to PTY master
		if _, err := c.ptyFile.Write(pw); err != nil {
			return err
		}
		_, err := c.ptyFile.Write([]byte{'\n'})
		return err
	})

	if err == nil {
		writeTimer.Emit()
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
	if c.recentPasswordSent() {
		c.passwordMu.Lock()
		password := c.password
		c.passwordMu.Unlock()
		if password == nil {
			return result
		}
		// Strip leading newlines (echo from password submission)
		result = bytes.TrimLeft(result, "\r\n")

		// Check if password appears in output (echo from misconfigured server)
		if err := password.Use(func(pw []byte) error {
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

func (c *Connector) prepareDisplayOutput(data []byte, suppressPrompt bool) []byte {
	return c.filterOutput(data, suppressPrompt)
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
