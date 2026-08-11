package sshconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var includeRe = regexp.MustCompile(`(?i)^include\s+(.+)$`)

// FindIncludeFiles scans the main SSH config for Include directives and returns
// the list of resolved file paths that exist, including nested includes.
func (p *Parser) FindIncludeFiles() ([]string, error) {
	seen := make(map[string]bool)
	var result []string

	// Use a queue for breadth-first traversal of includes
	queue := []string{p.configFile}
	seen[p.configFile] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		includes, err := p.findIncludeFilesFrom(current)
		if err != nil {
			return nil, err
		}

		for _, inc := range includes {
			if !seen[inc] {
				seen[inc] = true
				result = append(result, inc)
				queue = append(queue, inc) // Check this file for its own includes
			}
		}
	}

	return result, nil
}

// findIncludeFilesFrom extracts Include directives from a config file.
func (p *Parser) findIncludeFilesFrom(configPath string) ([]string, error) {
	f, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	baseDir := filepath.Dir(configPath)
	var result []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		match := includeRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		// Parse include targets (may be space-separated or quoted)
		targets := splitIncludeTargets(match[1])
		for _, target := range targets {
			paths := p.resolveIncludeTarget(target, baseDir)
			for _, resolved := range paths {
				if !seen[resolved] {
					seen[resolved] = true
					result = append(result, resolved)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// splitIncludeTargets splits the Include directive value into individual targets.
// Handles both space-separated and quoted paths.
func splitIncludeTargets(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	var result []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(value); i++ {
		c := value[i]

		if !inQuote && (c == '"' || c == '\'') {
			inQuote = true
			quoteChar = c
			continue
		}

		if inQuote && c == quoteChar {
			inQuote = false
			quoteChar = 0
			continue
		}

		if !inQuote && (c == ' ' || c == '\t') {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(c)
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// resolveIncludeTarget expands ~ and globs, returning existing file paths.
func (p *Parser) resolveIncludeTarget(target, baseDir string) []string {
	// Expand ~
	if strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			target = filepath.Join(home, target[2:])
		}
	} else if strings.HasPrefix(target, "~") {
		// Handle ~user - not commonly used, skip for now
		home, err := os.UserHomeDir()
		if err == nil {
			target = filepath.Join(home, target[1:])
		}
	}

	// Make absolute if relative
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDir, target)
	}

	// Try glob expansion
	matches, err := filepath.Glob(target)
	if err != nil || len(matches) == 0 {
		// No matches from glob, check if file exists directly
		if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
			return []string{target}
		}
		return nil
	}

	// Filter to only existing files
	var result []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			result = append(result, m)
		}
	}

	return result
}

// GetAllHosts returns all hosts from all Include files plus the main config.
func (p *Parser) GetAllHosts() ([]*HostEntry, error) {
	includes, err := p.FindIncludeFiles()
	if err != nil {
		return nil, err
	}

	var allHosts []*HostEntry

	// Parse main config (but typically hosts are in Include files)
	mainCfg, err := p.ParseFile(p.configFile)
	if err != nil {
		return nil, err
	}
	allHosts = append(allHosts, mainCfg.Hosts...)

	// Parse each Include file
	for _, incPath := range includes {
		cfg, err := p.ParseFile(incPath)
		if err != nil {
			// Log warning but continue
			continue
		}
		allHosts = append(allHosts, cfg.Hosts...)
	}

	return allHosts, nil
}

// FindHost searches all config files for a host by pattern.
func (p *Parser) FindHost(pattern string) (*HostEntry, error) {
	hosts, err := p.GetAllHosts()
	if err != nil {
		return nil, err
	}
	return FindHostByPattern(hosts, pattern), nil
}

// FindHostWithLocation searches for a host and returns which file it's in.
func (p *Parser) FindHostWithLocation(pattern string) (*HostEntry, *ParsedConfig, error) {
	includes, err := p.FindIncludeFiles()
	if err != nil {
		return nil, nil, err
	}

	// Check all include files first (more likely location)
	for _, incPath := range includes {
		cfg, err := p.ParseFile(incPath)
		if err != nil {
			continue
		}

		if host := FindHostByPattern(cfg.Hosts, pattern); host != nil {
			return host, cfg, nil
		}
	}

	// Check main config
	mainCfg, err := p.ParseFile(p.configFile)
	if err != nil {
		return nil, nil, err
	}

	if host := FindHostByPattern(mainCfg.Hosts, pattern); host != nil {
		return host, mainCfg, nil
	}

	return nil, nil, nil
}

// DeriveHostID generates a short Host identifier from an FQDN.
// For "server.example.com" returns "server".
// For names without dots, returns as-is (already a short identifier).
func DeriveHostID(input string) string {
	if idx := strings.Index(input, "."); idx > 0 {
		return input[:idx]
	}
	return input // Already a short identifier
}

// MatchResult represents the result of a fuzzy host match.
type MatchResult struct {
	Host        *HostEntry // Matched host (nil if no match or ambiguous)
	Suggestions []string   // Suggested hostnames if multiple matches
}

// MatchHost finds a host by exact or partial match.
// Returns a MatchResult with either a single matched host or suggestions.
func (p *Parser) MatchHost(query string) (*MatchResult, error) {
	// First try exact match
	if host, err := p.FindHost(query); err == nil && host != nil {
		return &MatchResult{Host: host}, nil
	}

	// Get all hosts for partial matching
	allHosts, err := p.GetAllHosts()
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)

	// Try exact match on short name
	for _, h := range allHosts {
		if strings.EqualFold(DeriveHostID(h.Host), query) {
			return &MatchResult{Host: h}, nil
		}
	}

	// Try prefix match on Host or HostName
	var prefixMatches []*HostEntry
	for _, h := range allHosts {
		if strings.HasPrefix(strings.ToLower(h.Host), queryLower) ||
			strings.HasPrefix(strings.ToLower(h.HostName), queryLower) {
			prefixMatches = append(prefixMatches, h)
		}
	}
	if len(prefixMatches) == 1 {
		return &MatchResult{Host: prefixMatches[0]}, nil
	}

	// Try contains match on Host or HostName
	var containsMatches []*HostEntry
	for _, h := range allHosts {
		if strings.Contains(strings.ToLower(h.Host), queryLower) ||
			strings.Contains(strings.ToLower(h.HostName), queryLower) {
			containsMatches = append(containsMatches, h)
		}
	}
	if len(containsMatches) == 1 {
		return &MatchResult{Host: containsMatches[0]}, nil
	}

	// Multiple matches - return suggestions
	if len(containsMatches) > 0 {
		suggestions := make([]string, len(containsMatches))
		for i, h := range containsMatches {
			suggestions[i] = h.Host
		}
		return &MatchResult{Suggestions: suggestions}, nil
	}

	// No matches
	return &MatchResult{}, nil
}
