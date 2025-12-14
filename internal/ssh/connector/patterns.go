package connector

import (
	"bytes"
	"regexp"
)

// Password prompt patterns - split into fast (literal) and slow (regex) paths.
var (
	// Simple suffix patterns (lowercase, checked with bytes.HasSuffix)
	passwordSuffixes = [][]byte{
		[]byte("password: "),
		[]byte("password:"),
		[]byte("passcode: "),
		[]byte("passcode:"),
	}

	// Simple contains patterns (lowercase, checked with bytes.Contains)
	passwordContains = [][]byte{
		[]byte("enter passphrase"),
		[]byte("password required"),
	}

	// Complex patterns that need regex (wildcards in pattern)
	passwordComplexPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)password for [^:]+:\s*$`),
		regexp.MustCompile(`(?i)[^@]+@[^']+s password:\s*$`), // user@host's password:
	}
)

// Host key prompt literals (lowercase, checked with bytes.Contains)
var (
	unknownHostLiteral    = []byte("are you sure you want to continue connecting")
	hostKeyChangedLiteral = []byte("remote host identification has changed")

	// Fingerprint extraction needs regex for capture groups
	fingerprintRe = regexp.MustCompile(`(?i)(\w+) key fingerprint is (SHA256:[A-Za-z0-9+/=]+)`)
)

// Auth failure literals (lowercase, checked with bytes.Contains)
var authFailureLiterals = [][]byte{
	[]byte("permission denied"),
	[]byte("authentication failed"),
	[]byte("try again"),
	[]byte("access denied"),
}

// matchPasswordPrompt checks if the buffer ends with a password prompt.
// Uses fast bytes operations for simple patterns, falls back to regex for complex ones.
func matchPasswordPrompt(buf []byte) bool {
	lower := bytes.ToLower(buf)

	// Fast path: simple suffix checks
	for _, suffix := range passwordSuffixes {
		if bytes.HasSuffix(lower, suffix) {
			return true
		}
	}

	// Fast path: simple contains checks
	for _, pattern := range passwordContains {
		if bytes.Contains(lower, pattern) {
			return true
		}
	}

	// Slow path: complex patterns need regex
	for _, re := range passwordComplexPatterns {
		if re.Match(buf) {
			return true
		}
	}
	return false
}

// matchAuthFailure checks if the output indicates an authentication failure.
func matchAuthFailure(buf []byte) bool {
	lower := bytes.ToLower(buf)
	for _, pattern := range authFailureLiterals {
		if bytes.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// matchUnknownHost checks if the output contains an unknown host key prompt.
func matchUnknownHost(buf []byte) bool {
	return bytes.Contains(bytes.ToLower(buf), unknownHostLiteral)
}

// matchHostKeyChanged checks if the output contains a host key changed warning.
func matchHostKeyChanged(buf []byte) bool {
	return bytes.Contains(bytes.ToLower(buf), hostKeyChangedLiteral)
}

// extractFingerprint extracts the key type and fingerprint from SSH output.
// Returns empty strings if no fingerprint found.
func extractFingerprint(output []byte) (keyType, fingerprint string) {
	matches := fingerprintRe.FindSubmatch(output)
	if len(matches) >= 3 {
		return string(matches[1]), string(matches[2])
	}
	return "", ""
}
