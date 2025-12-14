package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
)

// CompatFixResult holds the outcome of an iterative compatibility fix attempt.
type CompatFixResult struct {
	Success       bool                  // Whether connection ultimately succeeded
	Iterations    int                   // Number of test/fix iterations
	FixesApplied  []compat.CompatType   // Which fixes were applied
	TestResult    *connector.TestResult // Final test result
	StoppedReason string                // Why the loop stopped
}

// StoppedReason constants
const (
	ReasonConnectionSucceeded = "connection_succeeded"
	ReasonAuthFailedAfterKex  = "auth_failed_after_kex_success"
	ReasonNoCompatIssues      = "no_more_compatibility_issues"
	ReasonNoProgress          = "no_progress"
	ReasonMaxIterations       = "max_iterations_reached"
	ReasonFixApplicationError = "fix_application_error"
	ReasonTimeout             = "timeout"
	ReasonCommandNotFound     = "command_not_found"
	ReasonNonCompatError      = "non_compatibility_error"
)

// IterativeCompatFix tests a connection and iteratively applies compatibility
// fixes until the connection succeeds or no more fixes are applicable.
//
// Parameters:
//   - parser: SSH config parser
//   - cfg: Parsed config containing the host
//   - host: Host entry to test/fix
//   - maxIterations: Maximum fix attempts (default 5)
//   - verbose: Whether to print progress messages
//
// Returns a CompatFixResult with the outcome.
func IterativeCompatFix(
	parser *sshconfig.Parser,
	cfg *sshconfig.ParsedConfig,
	host *sshconfig.HostEntry,
	maxIterations int,
	verbose bool,
) *CompatFixResult {
	if maxIterations <= 0 {
		maxIterations = 5
	}

	result := &CompatFixResult{
		FixesApplied: make([]compat.CompatType, 0),
	}

	// Resolve credentials for testing
	password := resolveTestCredential(host)

	ctx := context.Background()

	for iteration := 1; iteration <= maxIterations; iteration++ {
		result.Iterations = iteration

		if verbose {
			ui.Info("Testing connection to %s (%d/%d)...", host.Host, iteration, maxIterations)
		}

		// Test connection using Host identifier for proper SSH config matching
		testCfg := connector.TestConfig{
			Timeout:  10 * time.Second,
			Password: password,
		}
		testResult, err := connector.TestConnection(ctx, host.Host, host.User(), testCfg)
		if err != nil {
			result.TestResult = &connector.TestResult{
				Success:  false,
				ExitCode: 1,
				Stderr:   err.Error(),
			}
			result.StoppedReason = ReasonNonCompatError
			return result
		}
		result.TestResult = testResult

		// Check for success
		if testResult.Success {
			result.Success = true
			result.StoppedReason = ReasonConnectionSucceeded
			if verbose && len(result.FixesApplied) > 0 {
				ui.Success("Connection successful after applying fixes")
			}
			return result
		}

		// Parse stderr for compatibility issues
		compatTypes := compat.ParseCompatibilityError(testResult.Stderr)

		// If no compat issues found, check other exit conditions
		if len(compatTypes) == 0 {
			// Check if KEX succeeded but auth failed (means compat is resolved)
			if len(result.FixesApplied) > 0 && compat.IsAuthFailureAfterKex(testResult.Stderr) {
				result.Success = true
				result.StoppedReason = ReasonAuthFailedAfterKex
				if verbose {
					ui.Info("Compatibility fixes applied (auth skipped in test mode)")
				}
				return result
			}

			// Check for specific exit codes
			switch testResult.ExitCode {
			case 124:
				result.StoppedReason = ReasonTimeout
			case 127:
				result.StoppedReason = ReasonCommandNotFound
			case 255:
				result.StoppedReason = ReasonNoCompatIssues
			default:
				result.StoppedReason = ReasonNonCompatError
			}
			return result
		}

		// Filter to only new fixes (not already applied)
		var newFixes []compat.CompatType
		appliedSet := make(map[compat.CompatType]bool)
		for _, ct := range result.FixesApplied {
			appliedSet[ct] = true
		}
		for _, ct := range compatTypes {
			if !appliedSet[ct] {
				newFixes = append(newFixes, ct)
			}
		}

		if len(newFixes) == 0 {
			if verbose {
				ui.Warning("Same compatibility issues persist, no new fixes available")
			}
			result.StoppedReason = ReasonNoProgress
			return result
		}

		// Display what we're fixing
		if verbose {
			for _, ct := range newFixes {
				cfg := compat.CompatConfigs[ct]
				ui.Warning("Applying: %s", cfg.Name)
			}
		}

		// Apply fixes to the host entry
		if err := sshconfig.ApplyCompatFixes(host, newFixes); err != nil {
			if verbose {
				ui.Error("Failed to apply fixes: %s", err)
			}
			result.StoppedReason = ReasonFixApplicationError
			return result
		}

		// Write updated config
		if err := parser.WriteFile(cfg); err != nil {
			if verbose {
				ui.Error("Failed to write config: %s", err)
			}
			result.StoppedReason = ReasonFixApplicationError
			return result
		}

		result.FixesApplied = append(result.FixesApplied, newFixes...)
	}

	// Max iterations reached - do one final test
	testCfg := connector.TestConfig{
		Timeout:  10 * time.Second,
		Password: password,
	}
	finalResult, _ := connector.TestConnection(ctx, host.Host, host.User(), testCfg)
	if finalResult != nil {
		result.TestResult = finalResult
		result.Success = finalResult.Success
	}
	result.StoppedReason = ReasonMaxIterations

	return result
}

// resolveTestCredential attempts to get a password for connection testing.
func resolveTestCredential(host *sshconfig.HostEntry) *secret.Secret {
	// Only try to get credentials if host uses password auth
	if !host.UsesPassword() {
		return nil
	}

	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return nil
	}

	// Unlock vault if needed and TTY is available
	_ = clisession.TryUnlockIfTTY(mgr)

	// Try to resolve credential
	cred, err := mgr.ResolveCredential(host.Host, filepath.Base(host.SourceFile), host.User())
	if err != nil || cred == nil || cred.Password == nil {
		return nil
	}

	// Return the secret directly (already a *secret.Secret)
	return cred.Password
}

// DisplayCompatResult shows the result of a compatibility fix attempt.
func DisplayCompatResult(result *CompatFixResult, hostname string) {
	if result.Success {
		// Success message already shown during iteration
		if result.StoppedReason == ReasonAuthFailedAfterKex {
			fmt.Println()
			ui.Info("Note: Test connection failed at authentication (expected with password-only hosts)")
			ui.Info("Compatibility fixes succeeded - nssh connections will work normally")
		}
	} else {
		reason := result.StoppedReason
		switch reason {
		case ReasonTimeout:
			reason = "connection timed out"
		case ReasonCommandNotFound:
			reason = "ssh command not found"
		case ReasonNoProgress:
			reason = "fixes not effective"
		case ReasonNoCompatIssues:
			reason = "not a compatibility issue"
		}

		ui.Warning("Compatibility fixes incomplete: %s", reason)
		if len(result.FixesApplied) > 0 {
			ui.Info("Applied before failure:")
			for _, ct := range result.FixesApplied {
				cfg := compat.CompatConfigs[ct]
				ui.Info("  - %s", cfg.Name)
			}
		}
	}
}

// TestHostWithTempConfig tests a connection using a temporary config file
// and iteratively applies compatibility fixes without modifying the real config.
//
// This function is used during host addition to test if a host works before
// committing it to the SSH config. The returned host entry includes any
// compatibility fixes that were applied.
//
// Parameters:
//   - host: Host entry to test (will be cloned, original not modified)
//   - maxIterations: Maximum fix attempts (default 5)
//   - verbose: Whether to print progress messages
//   - passwordOverride: Optional password to use instead of vault lookup (can be nil)
//
// Returns:
//   - result: The CompatFixResult with outcome details
//   - finalHost: The host entry with any compat fixes applied (nil on error)
//   - err: Error if temp config creation or other setup failed
func TestHostWithTempConfig(
	host *sshconfig.HostEntry,
	maxIterations int,
	verbose bool,
	passwordOverride *secret.Secret,
) (*CompatFixResult, *sshconfig.HostEntry, error) {
	if maxIterations <= 0 {
		maxIterations = 5
	}

	// Clone the host entry so we don't modify the original
	testHost := cloneHostEntry(host)

	// Create temp config file
	tempConfig, err := sshconfig.NewTempConfig(testHost)
	if err != nil {
		return nil, nil, fmt.Errorf("create temp config: %w", err)
	}
	defer tempConfig.Cleanup()

	result := &CompatFixResult{
		FixesApplied: make([]compat.CompatType, 0),
	}

	// Use password override if provided, otherwise resolve from vault
	var password *secret.Secret
	if passwordOverride != nil {
		password = passwordOverride
	} else {
		password = resolveTestCredential(testHost)
	}

	ctx := context.Background()

	for iteration := 1; iteration <= maxIterations; iteration++ {
		result.Iterations = iteration

		if verbose {
			ui.Info("Testing connection to %s (%d/%d)...", testHost.Host, iteration, maxIterations)
		}

		// Test connection using the temp config file
		testCfg := connector.TestConfig{
			Timeout:    10 * time.Second,
			Password:   password,
			ConfigFile: tempConfig.Path,
			// Port is in the temp config Host entry, no need to pass explicitly
		}

		// Use Host identifier so it matches the temp config's Host entry pattern.
		// This ensures host-specific settings (KexAlgorithms, etc.) are applied.
		testResult, err := connector.TestConnection(ctx, testHost.Host, testHost.User(), testCfg)
		if err != nil {
			result.TestResult = &connector.TestResult{
				Success:  false,
				ExitCode: 1,
				Stderr:   err.Error(),
			}
			result.StoppedReason = ReasonNonCompatError
			// Return testHost if fixes were applied, so caller can use them
			if len(result.FixesApplied) > 0 {
				return result, testHost, nil
			}
			return result, nil, nil
		}
		result.TestResult = testResult

		// Check for success
		if testResult.Success {
			result.Success = true
			result.StoppedReason = ReasonConnectionSucceeded
			if verbose && len(result.FixesApplied) > 0 {
				ui.Success("Connection successful after applying fixes")
			}
			return result, testHost, nil
		}

		// Parse stderr for compatibility issues
		compatTypes := compat.ParseCompatibilityError(testResult.Stderr)

		// If no compat issues found, check other exit conditions
		if len(compatTypes) == 0 {
			// Check if KEX succeeded but auth failed (means compat is resolved)
			if len(result.FixesApplied) > 0 && compat.IsAuthFailureAfterKex(testResult.Stderr) {
				result.Success = true
				result.StoppedReason = ReasonAuthFailedAfterKex
				if verbose {
					ui.Success("Connection successful after applying fixes")
				}
				return result, testHost, nil
			}

			// Check for specific exit codes
			switch testResult.ExitCode {
			case 124:
				result.StoppedReason = ReasonTimeout
			case 127:
				result.StoppedReason = ReasonCommandNotFound
			case 255:
				result.StoppedReason = ReasonNoCompatIssues
			default:
				result.StoppedReason = ReasonNonCompatError
			}
			// Return testHost if fixes were applied, so caller can use them
			if len(result.FixesApplied) > 0 {
				return result, testHost, nil
			}
			return result, nil, nil
		}

		// Filter to only new fixes (not already applied)
		var newFixes []compat.CompatType
		appliedSet := make(map[compat.CompatType]bool)
		for _, ct := range result.FixesApplied {
			appliedSet[ct] = true
		}
		for _, ct := range compatTypes {
			if !appliedSet[ct] {
				newFixes = append(newFixes, ct)
			}
		}

		if len(newFixes) == 0 {
			if verbose {
				ui.Warning("Same compatibility issues persist, no new fixes available")
			}
			result.StoppedReason = ReasonNoProgress
			// Return testHost with partial fixes applied so caller can use them
			return result, testHost, nil
		}

		// Display what we're fixing
		if verbose {
			for _, ct := range newFixes {
				cfg := compat.CompatConfigs[ct]
				ui.Warning("Applying: %s", cfg.Name)
			}
		}

		// Apply fixes to the test host entry
		if err := sshconfig.ApplyCompatFixes(testHost, newFixes); err != nil {
			if verbose {
				ui.Error("Failed to apply fixes: %s", err)
			}
			result.StoppedReason = ReasonFixApplicationError
			// Return testHost with any previously applied fixes
			if len(result.FixesApplied) > 0 {
				return result, testHost, nil
			}
			return result, nil, nil
		}

		// Update temp config with the modified host
		if err := tempConfig.Update(); err != nil {
			if verbose {
				ui.Error("Failed to update temp config: %s", err)
			}
			result.StoppedReason = ReasonFixApplicationError
			// Return testHost with any previously applied fixes
			if len(result.FixesApplied) > 0 {
				return result, testHost, nil
			}
			return result, nil, nil
		}

		result.FixesApplied = append(result.FixesApplied, newFixes...)
	}

	// Max iterations reached - do one final test
	testCfg := connector.TestConfig{
		Timeout:    10 * time.Second,
		Password:   password,
		ConfigFile: tempConfig.Path,
	}
	finalResult, _ := connector.TestConnection(ctx, testHost.Host, testHost.User(), testCfg)
	if finalResult != nil {
		result.TestResult = finalResult
		if finalResult.Success {
			result.Success = true
			return result, testHost, nil
		}
		// Also check for auth failure after KEX
		if compat.IsAuthFailureAfterKex(finalResult.Stderr) {
			result.Success = true
			result.StoppedReason = ReasonAuthFailedAfterKex
			return result, testHost, nil
		}
	}
	result.StoppedReason = ReasonMaxIterations

	return result, nil, nil
}

// cloneHostEntry creates a deep copy of a HostEntry.
func cloneHostEntry(h *sshconfig.HostEntry) *sshconfig.HostEntry {
	clone := &sshconfig.HostEntry{
		Host:       h.Host,
		HostName:   h.HostName,
		Patterns:   make([]string, len(h.Patterns)),
		Lines:      make([]string, len(h.Lines)),
		SourceFile: h.SourceFile,
		Properties: make(map[string]string, len(h.Properties)),
	}
	copy(clone.Patterns, h.Patterns)
	copy(clone.Lines, h.Lines)
	for k, v := range h.Properties {
		clone.Properties[k] = v
	}
	return clone
}

// WriteDebugFile saves connection test debug info to a file.
func WriteDebugFile(hostname string, result *connector.TestResult, extraInfo string) string {
	debugDir := "/tmp/nssh"
	if err := os.MkdirAll(debugDir, 0700); err != nil {
		return ""
	}

	filename := fmt.Sprintf("nssh-debug-%s-%d.txt", hostname, time.Now().Unix())
	path := filepath.Join(debugDir, filename)

	content := fmt.Sprintf("Hostname: %s\n", hostname)
	content += fmt.Sprintf("Exit code: %d\n", result.ExitCode)
	content += fmt.Sprintf("Success: %v\n", result.Success)
	if extraInfo != "" {
		content += fmt.Sprintf("\n%s\n", extraInfo)
	}
	content += "\n--- SSH Output ---\n"
	content += result.Stderr

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return ""
	}

	return path
}
