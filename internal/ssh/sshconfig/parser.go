package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

// HostEntry represents a parsed SSH Host block.
type HostEntry struct {
	// Host is the primary identifier (first pattern from "Host" line)
	Host string

	// HostName is the resolved address SSH connects to (from HostName directive, defaults to Host)
	HostName string

	// Patterns contains all patterns from the Host line
	Patterns []string

	// Lines contains the raw config lines including the Host directive
	Lines []string

	// SourceFile is the path to the file containing this entry
	SourceFile string

	// Properties contains parsed key-value pairs (lowercase keys)
	Properties map[string]string
}

// User returns the User property or empty string.
func (h *HostEntry) User() string {
	return h.Properties["user"]
}

// Port returns the Port property or "22".
func (h *HostEntry) Port() string {
	if p, ok := h.Properties["port"]; ok {
		return p
	}
	return "22"
}

// UsesPassword returns true if the host uses password authentication.
func (h *HostEntry) UsesPassword() bool {
	// Check PubkeyAuthentication
	if v, ok := h.Properties["pubkeyauthentication"]; ok {
		if strings.EqualFold(v, "no") {
			return true
		}
	}

	// Check PreferredAuthentications
	if v, ok := h.Properties["preferredauthentications"]; ok {
		lower := strings.ToLower(v)
		if strings.Contains(lower, "password") || strings.Contains(lower, "keyboard-interactive") {
			return true
		}
	}

	return false
}

// ParsedConfig represents a parsed SSH config file.
type ParsedConfig struct {
	// Path to the parsed file
	Path string

	// HeaderLines are lines before the first Host block
	HeaderLines []string

	// Hosts are the parsed host entries
	Hosts []*HostEntry
}

// Parser handles SSH config file operations.
type Parser struct {
	// configFile is the main SSH config path (default: ~/.ssh/config)
	configFile string
}

// NewParser creates a parser with default paths.
func NewParser() *Parser {
	paths := config.DefaultPaths()

	return &Parser{
		configFile: paths.SSHConfigFile,
	}
}

// NewParserWithPaths creates a parser with explicit paths.
func NewParserWithPaths(configFile, backupDir string, maxBackups int) *Parser {
	return &Parser{
		configFile: configFile,
	}
}

// regex patterns
var (
	hostDirectiveRe = regexp.MustCompile(`(?i)^host\s+(.+)$`)
	propertyRe      = regexp.MustCompile(`^\s+(\S+)\s+(.+)$`)
)

// ParseFile parses an SSH config file into header and host entries.
func (p *Parser) ParseFile(path string) (*ParsedConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ParsedConfig{Path: path}, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	result := &ParsedConfig{
		Path:        path,
		HeaderLines: make([]string, 0),
		Hosts:       make([]*HostEntry, 0),
	}

	scanner := bufio.NewScanner(f)
	var currentHost *HostEntry
	inHeader := true

	for scanner.Scan() {
		line := scanner.Text()

		// Check for Host directive
		if match := hostDirectiveRe.FindStringSubmatch(line); match != nil {
			// Save previous host if exists
			if currentHost != nil {
				result.Hosts = append(result.Hosts, currentHost)
			}

			// Parse patterns from Host line
			patterns := splitHostnames(match[1])
			if len(patterns) == 0 {
				continue
			}

			// Skip wildcard hosts (treat as header)
			if strings.HasPrefix(patterns[0], "*") || strings.HasPrefix(patterns[0], "?") {
				if inHeader || len(result.Hosts) == 0 {
					result.HeaderLines = append(result.HeaderLines, line+"\n")
					currentHost = nil
					continue
				}
			}

			inHeader = false
			currentHost = &HostEntry{
				Host:       patterns[0],
				HostName:   patterns[0], // Default to Host, updated if HostName directive found
				Patterns:   patterns,
				Lines:      []string{line + "\n"},
				SourceFile: path,
				Properties: make(map[string]string),
			}
		} else {
			// Add line to current context
			if inHeader || currentHost == nil {
				result.HeaderLines = append(result.HeaderLines, line+"\n")
			} else {
				currentHost.Lines = append(currentHost.Lines, line+"\n")

				// Parse property
				if match := propertyRe.FindStringSubmatch(line); match != nil {
					key := strings.ToLower(match[1])
					value := strings.TrimSpace(match[2])
					currentHost.Properties[key] = value

					// Update HostName field when directive is found
					if key == "hostname" {
						currentHost.HostName = value
					}
				}
			}
		}
	}

	// Save last host
	if currentHost != nil {
		result.Hosts = append(result.Hosts, currentHost)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}

	return result, nil
}

// splitHostnames parses the "Host" line value into individual hostnames.
func splitHostnames(hostLine string) []string {
	fields := strings.Fields(strings.TrimSpace(hostLine))
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			result = append(result, f)
		}
	}
	return result
}

// WriteFile writes a parsed config back to disk atomically.
func (p *Parser) WriteFile(cfg *ParsedConfig) error {
	// Create temp file in same directory for atomic rename
	dir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".ssh-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up on error
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	// Write header
	for _, line := range cfg.HeaderLines {
		if _, err := tmpFile.WriteString(line); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("write header: %w", err)
		}
	}

	// Write hosts
	for _, host := range cfg.Hosts {
		for _, line := range host.Lines {
			if _, err := tmpFile.WriteString(line); err != nil {
				_ = tmpFile.Close()
				return fmt.Errorf("write host: %w", err)
			}
		}
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Set secure permissions
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, cfg.Path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	tmpPath = "" // Prevent cleanup
	return nil
}

// FindInsertionIndex finds where to insert a host to maintain alphabetical order.
func FindInsertionIndex(hosts []*HostEntry, newHost string) int {
	lowerNew := strings.ToLower(newHost)
	for i, host := range hosts {
		if strings.ToLower(host.Host) > lowerNew {
			return i
		}
	}
	return len(hosts)
}

// SortHosts sorts hosts alphabetically by Host.
func SortHosts(hosts []*HostEntry) {
	sort.Slice(hosts, func(i, j int) bool {
		ai := strings.ToLower(hosts[i].Host)
		aj := strings.ToLower(hosts[j].Host)

		aiWild := strings.HasPrefix(ai, "*") || strings.HasPrefix(ai, "?")
		ajWild := strings.HasPrefix(aj, "*") || strings.HasPrefix(aj, "?")

		// Place wildcard patterns after non-wildcards to avoid sorting them to the top.
		if aiWild && !ajWild {
			return false
		}
		if ajWild && !aiWild {
			return true
		}
		return ai < aj
	})
}

// FindHostByPattern searches for a host by pattern in a list.
func FindHostByPattern(hosts []*HostEntry, pattern string) *HostEntry {
	for _, host := range hosts {
		for _, p := range host.Patterns {
			if strings.EqualFold(p, pattern) {
				return host
			}
		}
	}
	return nil
}

// RemoveHost removes a host from the list by pattern.
func RemoveHost(hosts []*HostEntry, pattern string) []*HostEntry {
	result := make([]*HostEntry, 0, len(hosts))
	for _, host := range hosts {
		found := false
		for _, p := range host.Patterns {
			if strings.EqualFold(p, pattern) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, host)
		}
	}
	return result
}

// CreateHostEntry creates a new HostEntry with the given parameters.
// host is the identifier/alias used in the Host line (what users type to connect).
// hostname is the resolved address SSH connects to (HostName directive, defaults to host if empty).
func CreateHostEntry(host, hostname, user string, port int, usesPassword bool, sourceFile string) *HostEntry {
	// Default hostname to host if not provided
	if hostname == "" {
		hostname = host
	}

	lines := []string{
		fmt.Sprintf("Host %s\n", host),
		fmt.Sprintf("  HostName %s\n", hostname), // Always include HostName
	}

	props := make(map[string]string)
	props["hostname"] = hostname

	if port != 22 {
		lines = append(lines, fmt.Sprintf("  Port %d\n", port))
		props["port"] = fmt.Sprintf("%d", port)
	}

	if user != "" {
		lines = append(lines, fmt.Sprintf("  User %s\n", user))
		props["user"] = user
	}

	if usesPassword {
		lines = append(lines,
			"  PubkeyAuthentication no\n",
			"  PreferredAuthentications keyboard-interactive,password\n",
		)
		props["pubkeyauthentication"] = "no"
		props["preferredauthentications"] = "keyboard-interactive,password"
	} else {
		lines = append(lines,
			"  PubkeyAuthentication yes\n",
			"  PasswordAuthentication no\n",
		)
		props["pubkeyauthentication"] = "yes"
		props["passwordauthentication"] = "no"
	}

	// Add trailing newline for separation
	lines = append(lines, "\n")

	return &HostEntry{
		Host:       host,
		HostName:   hostname,
		Patterns:   []string{host},
		Lines:      lines,
		SourceFile: sourceFile,
		Properties: props,
	}
}
