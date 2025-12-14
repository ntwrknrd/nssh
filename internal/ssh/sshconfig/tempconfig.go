package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

// TempConfig manages a temporary SSH config file for testing.
// It allows testing SSH connections against a host entry before
// committing changes to the real SSH config.
type TempConfig struct {
	// Path is the path to the temporary config file.
	Path string

	// Host is the host entry being tested.
	Host *HostEntry

	// cleanup removes the temp file when called.
	cleanup func()
}

// NewTempConfig creates a temporary SSH config file with the given host entry.
// The returned TempConfig.Path can be used with `ssh -F <path>`.
// It also includes any Host * settings from the user's SSH config to ensure
// the test uses the same algorithm restrictions as regular SSH connections.
// Call Cleanup() when done to remove the temp file.
func NewTempConfig(host *HostEntry) (*TempConfig, error) {
	tmpFile, err := os.CreateTemp("", "nssh-test-config-*")
	if err != nil {
		return nil, fmt.Errorf("create temp config: %w", err)
	}

	tc := &TempConfig{
		Path: tmpFile.Name(),
		Host: host,
		cleanup: func() {
			_ = os.Remove(tmpFile.Name())
		},
	}

	// Write initial config (includes Host * settings)
	if err := tc.write(tmpFile); err != nil {
		_ = tmpFile.Close()
		tc.Cleanup()
		return nil, err
	}

	if err := tmpFile.Close(); err != nil {
		tc.Cleanup()
		return nil, fmt.Errorf("close temp config: %w", err)
	}

	return tc, nil
}

// write writes the host entry to the given file.
// It includes Host * settings from the user's SSH config to ensure
// the test uses the same algorithm restrictions as regular SSH connections.
func (tc *TempConfig) write(f *os.File) error {
	// Write the host entry lines
	for _, line := range tc.Host.Lines {
		if _, err := f.WriteString(line); err != nil {
			return fmt.Errorf("write temp config: %w", err)
		}
		// Add newline if the line doesn't end with one
		if !strings.HasSuffix(line, "\n") {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("write temp config: %w", err)
			}
		}
	}

	// Then append Host * settings from user's SSH config
	// These will apply as defaults for any settings not in the specific host entry
	wildcardSettings := extractWildcardSettings()
	if len(wildcardSettings) > 0 {
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("write temp config: %w", err)
		}
		for _, line := range wildcardSettings {
			if _, err := f.WriteString(line); err != nil {
				return fmt.Errorf("write temp config: %w", err)
			}
			if !strings.HasSuffix(line, "\n") {
				if _, err := f.WriteString("\n"); err != nil {
					return fmt.Errorf("write temp config: %w", err)
				}
			}
		}
	}

	return nil
}

// extractWildcardSettings reads the user's SSH config and returns Host * block lines.
func extractWildcardSettings() []string {
	paths := config.DefaultPaths()
	sshConfig := paths.SSHConfigFile

	f, err := os.Open(sshConfig)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var wildcardLines []string
	inWildcard := false
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(strings.ToLower(line))

		// Check for Host * or Host *? (wildcard patterns)
		if strings.HasPrefix(trimmed, "host ") {
			pattern := strings.TrimSpace(trimmed[5:])
			// Check if this is a wildcard pattern (*, *?, etc.)
			if pattern == "*" || strings.HasPrefix(pattern, "* ") || pattern == "*?" {
				inWildcard = true
				wildcardLines = append(wildcardLines, line)
				continue
			}
			inWildcard = false
			continue
		}

		// Check for Match all
		if strings.HasPrefix(trimmed, "match ") && strings.Contains(trimmed, "all") {
			inWildcard = true
			wildcardLines = append(wildcardLines, line)
			continue
		}

		// If we hit another directive that starts a new block, stop collecting
		if strings.HasPrefix(trimmed, "match ") {
			inWildcard = false
			continue
		}

		// Collect lines if we're in a wildcard block
		if inWildcard && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || line == "") {
			// Skip Include directives - they can cause recursion issues
			if !strings.HasPrefix(trimmed, "include ") {
				wildcardLines = append(wildcardLines, line)
			}
		} else if inWildcard && trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			// Non-indented line means new block
			inWildcard = false
		}
	}

	return wildcardLines
}

// Update writes the current host entry state to the temp file.
// Use this after modifying the host entry (e.g., applying compat fixes).
func (tc *TempConfig) Update() error {
	f, err := os.Create(tc.Path)
	if err != nil {
		return fmt.Errorf("open temp config for update: %w", err)
	}
	defer func() { _ = f.Close() }()

	return tc.write(f)
}

// Cleanup removes the temporary config file.
// Safe to call multiple times.
func (tc *TempConfig) Cleanup() {
	if tc.cleanup != nil {
		tc.cleanup()
		tc.cleanup = nil
	}
}
