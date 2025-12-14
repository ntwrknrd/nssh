package sshconfig

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

// ApplyCompatFixes adds compatibility fix lines to a host entry.
// It removes any existing conflicting directives and inserts new ones.
func ApplyCompatFixes(host *HostEntry, compatTypes []compat.CompatType) error {
	if len(compatTypes) == 0 {
		return nil
	}

	// Build set of directives to remove
	directivesToRemove := make(map[string]bool)
	for _, ct := range compatTypes {
		cfg := compat.CompatConfigs[ct]
		directivesToRemove[strings.ToLower(cfg.Directive)] = true
	}

	// Filter out existing conflicting lines
	var newLines []string
	for _, line := range host.Lines {
		trimmed := strings.TrimSpace(line)
		keep := true
		for directive := range directivesToRemove {
			if strings.HasPrefix(strings.ToLower(trimmed), directive+" ") ||
				strings.HasPrefix(strings.ToLower(trimmed), directive+"\t") {
				keep = false
				break
			}
		}
		if keep {
			newLines = append(newLines, line)
		}
	}

	// Find insertion point (after HostName/Port, or after Host line)
	insertIdx := findCompatInsertionPoint(newLines)

	// Build lines to insert
	var insertLines []string
	for _, ct := range compatTypes {
		cfg := compat.CompatConfigs[ct]
		insertLines = append(insertLines, cfg.ConfigLines...)
	}

	// Insert compat lines
	host.Lines = insertAt(newLines, insertIdx, insertLines)

	// Update properties
	for _, ct := range compatTypes {
		cfg := compat.CompatConfigs[ct]
		// Extract directive value from config line
		for _, line := range cfg.ConfigLines {
			parts := strings.Fields(strings.TrimSpace(line))
			if len(parts) >= 2 {
				host.Properties[strings.ToLower(parts[0])] = strings.Join(parts[1:], " ")
			}
		}
	}

	return nil
}

// ApplyAuthType changes the authentication type of a host entry.
// authType should be "password", "keyboard-interactive", or "key".
func ApplyAuthType(host *HostEntry, authType string) error {
	// Remove existing auth-related lines
	authDirectives := map[string]bool{
		"pubkeyauthentication":     true,
		"passwordauthentication":   true,
		"preferredauthentications": true,
	}

	var newLines []string
	for _, line := range host.Lines {
		trimmed := strings.TrimSpace(line)
		keep := true
		for directive := range authDirectives {
			if strings.HasPrefix(strings.ToLower(trimmed), directive+" ") ||
				strings.HasPrefix(strings.ToLower(trimmed), directive+"\t") {
				keep = false
				break
			}
		}
		if keep {
			newLines = append(newLines, line)
		}
	}

	// Find insertion point (after User, or after Host line)
	insertIdx := findAuthInsertionPoint(newLines)

	// Build auth lines based on type
	var authLines []string
	switch authType {
	case "password":
		authLines = []string{
			"  PubkeyAuthentication no\n",
			"  PreferredAuthentications password\n",
		}
		host.Properties["pubkeyauthentication"] = "no"
		host.Properties["preferredauthentications"] = "password"
		delete(host.Properties, "passwordauthentication")
	case "keyboard-interactive":
		authLines = []string{
			"  PubkeyAuthentication no\n",
			"  PreferredAuthentications keyboard-interactive\n",
		}
		host.Properties["pubkeyauthentication"] = "no"
		host.Properties["preferredauthentications"] = "keyboard-interactive"
		delete(host.Properties, "passwordauthentication")
	case "key", "publickey":
		authLines = []string{
			"  PubkeyAuthentication yes\n",
			"  PasswordAuthentication no\n",
		}
		host.Properties["pubkeyauthentication"] = "yes"
		host.Properties["passwordauthentication"] = "no"
		delete(host.Properties, "preferredauthentications")
	default:
		return fmt.Errorf("unknown auth type: %s", authType)
	}

	// Insert auth lines
	host.Lines = insertAt(newLines, insertIdx, authLines)

	return nil
}

// HasCompatFix checks if a host entry already has a specific compatibility fix.
func HasCompatFix(host *HostEntry, compatType compat.CompatType) bool {
	cfg := compat.CompatConfigs[compatType]
	directive := strings.ToLower(cfg.Directive)
	_, exists := host.Properties[directive]
	return exists
}

// GetAppliedCompatFixes returns which compat fixes are already applied to a host.
func GetAppliedCompatFixes(host *HostEntry) []compat.CompatType {
	var applied []compat.CompatType
	for _, ct := range compat.AllCompatTypes() {
		if HasCompatFix(host, ct) {
			applied = append(applied, ct)
		}
	}
	return applied
}

// findCompatInsertionPoint finds where to insert compat lines.
// Prefers after HostName/Port, falls back to after Host line.
func findCompatInsertionPoint(lines []string) int {
	insertIdx := 1 // Default: after Host line

	// Pattern to find HostName or Port
	hostnameOrPort := regexp.MustCompile(`(?i)^\s*(hostname|port)\s+`)

	for i, line := range lines {
		if i == 0 {
			continue // Skip Host line
		}
		if hostnameOrPort.MatchString(line) {
			insertIdx = i + 1
		}
		// Stop at blank line (end of directives)
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	return insertIdx
}

// findAuthInsertionPoint finds where to insert auth lines.
// Prefers after User, falls back to end of directives.
func findAuthInsertionPoint(lines []string) int {
	insertIdx := 1 // Default: after Host line

	userPattern := regexp.MustCompile(`(?i)^\s*user\s+`)

	for i, line := range lines {
		if i == 0 {
			continue // Skip Host line
		}
		// Track last real directive
		if strings.TrimSpace(line) != "" {
			insertIdx = i + 1
		}
		// Prefer after User line
		if userPattern.MatchString(line) {
			insertIdx = i + 1
			break
		}
		// Stop at blank line
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	return insertIdx
}

// insertAt inserts lines at the specified index.
func insertAt(lines []string, idx int, insert []string) []string {
	if idx >= len(lines) {
		return append(lines, insert...)
	}
	result := make([]string, 0, len(lines)+len(insert))
	result = append(result, lines[:idx]...)
	result = append(result, insert...)
	result = append(result, lines[idx:]...)
	return result
}
