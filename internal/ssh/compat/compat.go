package compat

import (
	"regexp"
	"strings"
)

// CompatType identifies a compatibility fix type.
type CompatType string

// Compatibility fix types for SSH negotiation issues.
const (
	CompatLegacyKex     CompatType = "legacy-kex"
	CompatLegacyMACs    CompatType = "legacy-macs"
	CompatLegacyHostKey CompatType = "legacy-hostkey"
	CompatLegacyPubkey  CompatType = "legacy-pubkey"

	CompatKex     CompatType = CompatLegacyKex
	CompatMACs    CompatType = CompatLegacyMACs
	CompatHostKey CompatType = CompatLegacyHostKey
)

// CompatConfig defines a compatibility fix with its config lines and error patterns.
type CompatConfig struct {
	Name          string           // Human-readable name
	Description   string           // What this fix does
	Option        string           // OpenSSH -o option rendering
	ConfigLines   []string         // SSH config lines to add (with leading spaces)
	Directive     string           // SSH directive name (for removal detection)
	ErrorPatterns []*regexp.Regexp // Patterns that match SSH stderr errors
}

// CompatConfigs maps compat types to their configurations.
// Based on Python's COMPAT_CONFIGS in fixer.py.
var CompatConfigs = map[CompatType]CompatConfig{
	CompatLegacyKex: {
		Name:        "Legacy Key Exchange",
		Description: "Add legacy KexAlgorithms for older SSH servers",
		Option:      "KexAlgorithms=+diffie-hellman-group14-sha1,+diffie-hellman-group1-sha1",
		ConfigLines: []string{"  KexAlgorithms +diffie-hellman-group14-sha1,+diffie-hellman-group1-sha1\n"},
		Directive:   "KexAlgorithms",
		ErrorPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)no matching key exchange method found`),
			regexp.MustCompile(`(?i)unable to negotiate [^:]+: no matching key exchange`),
		},
	},
	CompatLegacyMACs: {
		Name:        "Legacy MACs",
		Description: "Add legacy MAC algorithms for older SSH servers",
		Option:      "MACs=+hmac-sha1,+hmac-sha1-96",
		ConfigLines: []string{"  MACs +hmac-sha1,+hmac-sha1-96\n"},
		Directive:   "MACs",
		ErrorPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)no matching macs? found`),
			regexp.MustCompile(`(?i)unable to negotiate [^:]+: no matching mac`),
		},
	},
	CompatLegacyHostKey: {
		Name:        "Legacy Host Key Algorithms",
		Description: "Add legacy host key algorithms for older SSH servers",
		Option:      "HostKeyAlgorithms=+ssh-rsa",
		ConfigLines: []string{"  HostKeyAlgorithms +ssh-rsa\n"},
		Directive:   "HostKeyAlgorithms",
		ErrorPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)no matching host key type found`),
			regexp.MustCompile(`(?i)unable to negotiate [^:]+: no matching host key`),
		},
	},
	CompatLegacyPubkey: {
		Name:        "Legacy Public Key Algorithms",
		Description: "Add legacy accepted public key algorithms for older SSH servers",
		Option:      "PubkeyAcceptedAlgorithms=+ssh-rsa",
		ConfigLines: []string{"  PubkeyAcceptedAlgorithms +ssh-rsa\n"},
		Directive:   "PubkeyAcceptedAlgorithms",
		ErrorPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)no mutual signature algorithm`),
			regexp.MustCompile(`(?i)signature algorithm ssh-rsa not in PubkeyAcceptedAlgorithms`),
		},
	},
}

// AllCompatTypes returns all compatibility types in application order.
func AllCompatTypes() []CompatType {
	return []CompatType{CompatLegacyKex, CompatLegacyMACs, CompatLegacyHostKey, CompatLegacyPubkey}
}

// ParseCompatibilityError scans SSH stderr output for known compatibility errors.
// Returns a list of compat types that should be applied to fix the detected issues.
func ParseCompatibilityError(stderr string) []CompatType {
	var needed []CompatType
	for _, compatType := range AllCompatTypes() {
		config := CompatConfigs[compatType]
		for _, pattern := range config.ErrorPatterns {
			if pattern.MatchString(stderr) {
				needed = append(needed, compatType)
				break
			}
		}
	}
	return needed
}

// IsAuthFailureAfterKex checks if the connection failed at authentication
// after a successful key exchange. This indicates compatibility issues
// were resolved but auth failed (expected with BatchMode password hosts).
func IsAuthFailureAfterKex(stderr string) bool {
	hasSuccessfulKex := kexSuccessPattern.MatchString(stderr)
	hasAuthFailure := authFailurePattern.MatchString(stderr)
	return hasSuccessfulKex && hasAuthFailure
}

// DidAuthSucceed checks if authentication succeeded based on SSH verbose output.
func DidAuthSucceed(stderr string) bool {
	return authSuccessPattern.MatchString(stderr)
}

// ExtractAuthMethod extracts the authentication method from SSH verbose output.
// Returns empty string if no auth method is found.
func ExtractAuthMethod(stderr string) string {
	match := authMethodPattern.FindStringSubmatch(stderr)
	if len(match) > 1 {
		return strings.ToLower(match[1])
	}
	return ""
}

// Pre-compiled patterns for SSH output parsing
var (
	// KEX success: "debug1: kex: algorithm: curve25519-sha256"
	kexSuccessPattern = regexp.MustCompile(`debug1:.*kex:.*algorithm:`)

	// Auth failure patterns
	authFailurePattern = regexp.MustCompile(`(?i)(Permission denied|No more authentication methods)`)

	// Auth success: "Authenticated to hostname (via proxy) using "password"."
	authSuccessPattern = regexp.MustCompile(`(?i)Authenticated to [^\s]+(?:\s+\([^)]+\))?`)

	// Auth method extraction: Authenticated to host using "password"
	authMethodPattern = regexp.MustCompile(`(?i)Authenticated to [^\s]+(?:\s+\([^)]+\))?\s+using\s+"([^"]+)"`)
)
