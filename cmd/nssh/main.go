// Package main provides the nssh command-line interface.
// nssh is an SSH wrapper for power users that provides host management,
// encrypted credential storage, automatic password injection, and session recording.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/cli"
	"github.com/ntwrknrd/nssh/internal/cli/cp"
	"github.com/ntwrknrd/nssh/internal/cli/ctx"
	"github.com/ntwrknrd/nssh/internal/cli/host"
	"github.com/ntwrknrd/nssh/internal/cli/lock"
	"github.com/ntwrknrd/nssh/internal/cli/log"
	"github.com/ntwrknrd/nssh/internal/cli/self"
	"github.com/ntwrknrd/nssh/internal/cli/self/bench"
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/cli/unlock"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/logging"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	// version is set via ldflags at build time from git describe.
	// Falls back to "dev" for local builds without tags.
	version = "dev"

	// subcommands lists known subcommand names for routing
	subcommands = map[string]bool{
		"host":               true,
		"ctx":                true,
		"log":                true,
		"cp":                 true,
		"self":               true,
		"lock":               true,
		"unlock":             true,
		"connect":            true,
		"smart-connect":      true,
		"__list-subcommands": true,
		"__agent":            true, // Hidden agent daemon mode
		// Cobra's hidden shell completion commands
		"__complete":       true,
		"__completeNoDesc": true,
	}

	// sshFlagsWithValue are SSH-style short flags that consume the following arg.
	// Used to avoid misclassifying the value as the hostname during preprocessing.
	sshFlagsWithValue = map[string]bool{
		"-b": true, // bind address
		"-c": true, // cipher spec
		"-D": true, // dynamic forward
		"-E": true, // log file
		"-F": true, // config file
		"-I": true, // pkcs11 / smartcard
		"-i": true, // identity file
		"-J": true, // jump host
		"-L": true, // local forward
		"-l": true, // login name
		"-m": true, // mac algorithms
		"-O": true, // control command
		"-o": true, // ssh option
		"-p": true, // port
		"-Q": true, // query
		"-R": true, // remote forward
		"-S": true, // control socket
		"-W": true, // direct stream local->remote
		"-w": true, // tun devices
	}

	// globalFlags are flags parsed by the root command (not SSH passthrough).
	// These must stay before the subcommand name for Cobra to parse them.
	globalFlags = map[string]bool{
		"-v": true, // verbose
		"-V": true, // version
		"-h": true, // help
	}

	// Global flags
	verbose     bool
	showVersion bool
)

// getBuildInfo extracts commit hash and build time from Go's embedded build info.
// This is automatically populated by Go 1.18+ when building from a git repository.
func getBuildInfo() (commit, date string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
			if len(commit) > 7 {
				commit = commit[:7]
			}
		case "vcs.time":
			date = s.Value
		}
	}
	return commit, date
}

// main is the entry point for nssh.
// It checks for hidden agent mode first, then runs the CLI.
func main() {
	// Hidden agent mode - bypasses normal CLI
	if len(os.Args) >= 2 && os.Args[1] == "__agent" {
		runAgentMode()
		return
	}

	if err := run(); err != nil {
		// Handle --explain flag (shows extended help, not an error)
		if ui.IsExplainShown(err) {
			return
		}

		// Handle HostNotFoundError by spawning host add
		var notFound *cli.HostNotFoundError
		if errors.As(err, &notFound) {
			if spawnErr := spawnHostAdd(notFound.Hostname); spawnErr != nil {
				fmt.Fprintln(os.Stderr, "nssh:", spawnErr)
				os.Exit(1)
			}
			return
		}

		var exitErr *exit.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, "nssh:", exitErr.Message)
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, "nssh:", err)
		os.Exit(1)
	}
}

// run creates and executes the root command with preprocessed arguments.
func run() error {
	rootCmd := newRootCmd()

	// Transform args before Cobra routing
	// "nssh hostname" -> "nssh connect hostname"
	rootCmd.SetArgs(preprocessArgs(os.Args[1:]))

	return rootCmd.Execute()
}

// newRootCmd creates and configures the root Cobra command with all subcommands.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "nssh [host]",
		Short: "Smart connect to host",
		Long: `SSH wrapper for power users: manage hosts and credentials, inject passwords automatically,
and record sessions.`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if showVersion {
				self.RunVersionExit()
			}
			initLogging(verbose)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Print debug messages")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "Print command version")

	// Disable the default help command (use --help flag instead)
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

	// Define our own help flag with custom description (before Cobra adds its default)
	rootCmd.PersistentFlags().BoolP("help", "h", false, "Print command help")

	// Set version info and root command for self package
	commit, date := getBuildInfo()
	self.SetVersion(version, commit, date, strings.Join(agent.CompiledFeatures, ", "))
	self.SetRootCmd(rootCmd)

	// Add subcommands
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.AddCommand(newSmartConnectCmd())
	rootCmd.AddCommand(newHostCmd())
	rootCmd.AddCommand(newCtxCmd())
	rootCmd.AddCommand(newLogCmd())
	rootCmd.AddCommand(newCpCmd())
	rootCmd.AddCommand(newSelfCmd())

	lockCmd := lock.NewCmd()
	unlockCmd := unlock.NewCmd()
	ui.ApplyStyledHelp(lockCmd)
	ui.ApplyStyledHelp(unlockCmd)
	rootCmd.AddCommand(lockCmd)
	rootCmd.AddCommand(unlockCmd)

	rootCmd.AddCommand(newListSubcommandsCmd())

	ui.ApplyStyledHelp(rootCmd)
	return rootCmd
}

// preprocessArgs transforms "nssh hostname" -> "nssh smart-connect hostname"
// This provides explicit routing while preserving the simple UX.
//
// Key insight: Cobra parses flags at the current command level BEFORE routing to
// subcommands. So "nssh -p 2222 host" fails because -p is not a root command flag.
//
// Solution: Separate global flags (parsed by root) from SSH passthrough flags
// (parsed by subcommand), placing SSH flags AFTER the hostname.
//
// Examples:
//   - "nssh host"              -> "nssh smart-connect host"
//   - "nssh -v host"           -> "nssh -v smart-connect host"
//   - "nssh -p 2222 host"      -> "nssh smart-connect host -p 2222"
//   - "nssh -v -p 2222 host"   -> "nssh -v smart-connect host -p 2222"
//   - "nssh host -p 2222"      -> "nssh smart-connect host -p 2222"
func preprocessArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	// Separate args into categories
	var globalFlagArgs []string     // -v, -V, -h (go before subcommand)
	var sshPassthroughArgs []string // SSH flags like -p 2222 (go after hostname)
	var hostnameIdx = -1

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Non-flag argument
		if !strings.HasPrefix(arg, "-") {
			// Check if this is a known subcommand
			if subcommands[arg] {
				// Known subcommand - pass through unchanged
				return args
			}
			// First non-flag, non-subcommand is the hostname
			hostnameIdx = i
			// Everything after hostname goes to sshPassthroughArgs
			sshPassthroughArgs = append(sshPassthroughArgs, args[i+1:]...)
			break
		}

		// Flag argument - categorize it
		switch {
		case globalFlags[arg]:
			// Global flag (no value)
			globalFlagArgs = append(globalFlagArgs, arg)
		case len(arg) == 2 && sshFlagsWithValue[arg]:
			// SSH flag with value (e.g., -p 2222)
			sshPassthroughArgs = append(sshPassthroughArgs, arg)
			if i+1 < len(args) {
				i++
				sshPassthroughArgs = append(sshPassthroughArgs, args[i])
			}
		case strings.HasPrefix(arg, "--"):
			// Long flag - check if it's global
			if arg == "--verbose" || arg == "--version" || arg == "--help" {
				globalFlagArgs = append(globalFlagArgs, arg)
			} else {
				// Unknown long flag - pass through to SSH
				sshPassthroughArgs = append(sshPassthroughArgs, arg)
			}
		default:
			// Other short flags (e.g., -4, -6, -A, -N, etc.) - SSH passthrough
			sshPassthroughArgs = append(sshPassthroughArgs, arg)
		}
	}

	// No hostname found (e.g., "nssh -v --help")
	if hostnameIdx == -1 {
		return args
	}

	hostname := args[hostnameIdx]

	// Build result: [global flags] smart-connect [hostname] [ssh passthrough flags]
	result := make([]string, 0, len(args)+1)
	result = append(result, globalFlagArgs...)
	result = append(result, "smart-connect", hostname)
	result = append(result, sshPassthroughArgs...)
	return result
}

// initLogging configures the default slog logger based on verbosity.
func initLogging(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

// newConnectCmd creates the connect subcommand for direct SSH connections.
// This bypasses smart resolution - use for hosts not in SSH config.
func newConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect <host> [ssh-args...]",
		Short: "Connect to host",
		Long: `Connect directly to a host via SSH, bypassing smart routing.

This provides a raw SSH wrapper experience without fuzzy matching or
host-add fallback. Use this to connect to hosts whose names conflict
with subcommands (e.g., "host", "log", "cp", "self").

Example: nssh connect host -p 2222`,
		Args: func(cmd *cobra.Command, args []string) error {
			// Allow 0 args when --explain is used (handled by PreRunE)
			if explain, _ := cmd.Flags().GetBool("explain"); explain {
				return nil
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return connectHost(args[0], args[1:])
		},
	}

	// Stop parsing flags after first positional arg (hostname)
	// This allows SSH flags like "-p 22" to pass through to args
	cmd.Flags().SetInterspersed(false)

	ui.ApplyStyledHelp(cmd)
	return cmd
}

// newSmartConnectCmd creates the smart-connect subcommand (hidden, used via arg transformation).
// This provides smart hostname resolution with fuzzy matching and host-add fallback.
func newSmartConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "smart-connect <host> [ssh-args...]",
		Short:  "Connect to a host with smart resolution",
		Args:   cobra.MinimumNArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Extract username if user@host format was used
			user, host := parseUserHost(args[0])
			hostname, err := resolveHostname(host)
			if err != nil {
				return err
			}
			// Prepend -l flag if user was specified inline
			sshArgs := args[1:]
			if user != "" {
				sshArgs = append([]string{"-l", user}, sshArgs...)
			}
			return connectHost(hostname, sshArgs)
		},
	}

	cmd.Flags().SetInterspersed(false)
	return cmd
}

// connectHost handles the SSH connection.
func connectHost(hostname string, sshArgs []string) error {
	// Check if recording should wrap this session
	recorded, err := connector.MaybeWrapWithRecording(hostname, sshArgs)
	if err != nil {
		return err
	}
	if recorded {
		// Recording wrapper handled the session (either succeeded or failed)
		return nil
	}

	// Load config
	configTimer := connector.StartTiming(connector.TimingConfigLoad)
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	configTimer.Emit()

	// Set up audit logging for SSH connections (best-effort; non-fatal)
	var audit *logging.AuditLogger
	if cfg.Logging.Audit.Enabled {
		paths := config.DefaultPaths()
		audit, err = logging.NewAuditLogger(slog.LevelError, &cfg.Logging.Audit, paths.StateDir)
		if err != nil {
			slog.Warn("failed to initialize audit logger", "err", err)
			audit = nil
		} else {
			defer func() { _ = audit.Close() }()
		}
	}

	slog.Debug("connecting to host",
		"host", hostname,
		"ssh_args", sshArgs,
		"timeout", cfg.SSH.Connection.Timeout.Duration(),
	)

	if audit != nil {
		audit.Info("ssh_connect_start", "host", hostname, "ssh_args", sshArgs)
	}

	// Resolve username from SSH config or args
	username := resolveUsername(hostname, sshArgs, cfg)

	// Strip user@ prefix from hostname if present (handles direct connectHost calls)
	if idx := strings.LastIndex(hostname, "@"); idx != -1 {
		hostname = hostname[idx+1:]
	}

	// Look up include file for this host (used for credential resolution)
	includeFile := ""
	var hostEntry *sshconfig.HostEntry
	parser := sshconfig.NewParser()
	if host, err := parser.FindHost(hostname); err == nil && host != nil {
		hostEntry = host
		includeFile = filepath.Base(host.SourceFile)
	}

	// Resolve credentials
	credTimer := connector.StartTiming(connector.TimingCredentialLookup)
	var cred *vault.ResolvedCredential
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		slog.Debug("vault not available", "err", err)
	} else {
		// Auto-prompt for unlock if needed and TTY is available
		if mgr.NeedsUnlock() {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				slog.Debug("vault locked, prompting for unlock")
				if err := clisession.Unlock(mgr, false); err != nil {
					if err == ui.ErrInterrupted {
						os.Exit(130) // Standard exit code for SIGINT
					}
					slog.Warn("unlock failed", "err", err)
					// Continue without credentials
				}
			} else {
				slog.Debug("vault locked and no TTY available, skipping unlock")
			}
		}

		// Try include file-based resolution first, then domain-based
		if includeFile != "" {
			cred, err = mgr.ResolveCredential(hostname, includeFile, username)
			if err != nil {
				slog.Warn("credential resolution failed", "err", err)
			}
		}
		if cred == nil {
			cred, err = mgr.ResolveCredentialWithDomain(hostname, username)
			if err != nil {
				slog.Warn("domain credential resolution failed", "err", err)
			}
		}
	}
	credTimer.Emit()

	// If we have credentials, use the resolved username
	if cred != nil {
		username = cred.Username
		slog.Debug("resolved credential", "username", username, "source", cred.Source)
	}

	// Create connector
	var password *secret.Secret
	if cred != nil {
		password = cred.Password
	}
	conn := connector.NewConnector(hostname, username, password, sshArgs)
	if hostEntry != nil {
		conn.SetResolvedEndpoint(hostEntry.HostName, hostEntry.Port())
	}
	conn.SetAcceptOnceMode(cfg.SSH.Security.AcceptOnceMode)

	// Configure timeouts
	conn.SetTimeouts(&cfg.SSH.Connection)

	// Run connection
	connErr := conn.Run(context.Background())

	if audit != nil {
		if connErr == nil {
			audit.Info("ssh_connect_end", "host", hostname, "status", "success")
		} else {
			exitCode := 1
			var exitErr *exit.ExitError
			if errors.As(connErr, &exitErr) {
				exitCode = exitErr.Code
			}
			audit.Info("ssh_connect_end", "host", hostname, "status", "error", "exit_code", exitCode, "error", connErr.Error())
		}
	}

	// Check if connection failed with a compatibility error
	if connErr != nil && isCompatibilityError(connErr) {
		if handleCompatibilityFix(hostname, includeFile) {
			// Retry connection after fixes applied
			// Re-resolve credentials since the previous ones may have been destroyed
			slog.Debug("retrying connection after compatibility fixes")

			var retryPassword *secret.Secret
			if mgr != nil {
				var retryCred *vault.ResolvedCredential
				if includeFile != "" {
					retryCred, _ = mgr.ResolveCredential(hostname, includeFile, username)
				}
				if retryCred == nil {
					retryCred, _ = mgr.ResolveCredentialWithDomain(hostname, username)
				}
				if retryCred != nil {
					retryPassword = retryCred.Password
				}
			}

			conn2 := connector.NewConnector(hostname, username, retryPassword, sshArgs)
			conn2.SetAcceptOnceMode(cfg.SSH.Security.AcceptOnceMode)
			conn2.SetTimeouts(&cfg.SSH.Connection)
			return conn2.Run(context.Background())
		}
	}

	return connErr
}

// isCompatibilityError checks if an error is likely a SSH compatibility issue.
func isCompatibilityError(err error) bool {
	var exitErr *exit.ExitError
	if errors.As(err, &exitErr) {
		// SSH returns 255 for protocol/negotiation failures
		return exitErr.Code == exit.ExitConnectionFailed || exitErr.Code == 255
	}
	return false
}

// handleCompatibilityFix attempts to fix compatibility issues for a host.
// Iterates until all issues are resolved or no more fixes are applicable.
// Returns true if fixes were applied and connection should be retried.
func handleCompatibilityFix(hostname, includeFile string) bool {
	const maxIterations = 5

	// Find the host in SSH config
	parser := sshconfig.NewParser()

	hostEntry, parsedCfg, err := parser.FindHostWithLocation(hostname)
	if err != nil || hostEntry == nil {
		slog.Debug("host not found in config, cannot apply compat fixes", "host", hostname)
		return false
	}

	var allFixesApplied []compat.CompatType
	appliedSet := make(map[compat.CompatType]bool)

	for iteration := 1; iteration <= maxIterations; iteration++ {
		// Get fresh password for each test (previous may have been destroyed)
		testCfg := connector.TestConfig{
			Timeout: 10 * time.Second,
			Port:    hostEntry.Port(),
		}
		if cfg, err := config.LoadDefault(); err == nil && cfg != nil {
			testCfg.UseSystemKnownHosts = cfg.SSH.Security.CompatPersistProbes
		}
		mgr, err := clisession.NewManager(vault.Auto())
		if err == nil {
			cred, _ := mgr.ResolveCredential(hostname, includeFile, hostEntry.User())
			if cred != nil {
				testCfg.Password = cred.Password
			}
		}

		testResult, err := connector.TestConnection(context.Background(), hostEntry.HostName, hostEntry.User(), testCfg)
		if err != nil {
			slog.Debug("test connection failed", "err", err)
			break
		}

		// Check if connection succeeded (KEX worked, auth might have failed)
		// This catches auth failures like wrong password - those aren't compat issues
		if testResult.Success || compat.IsAuthFailureAfterKex(testResult.Stderr) {
			// KEX succeeded - not a compatibility issue
			slog.Debug("KEX succeeded, not a compatibility issue")
			break
		}

		// Check for compatibility errors
		compatTypes := compat.ParseCompatibilityError(testResult.Stderr)
		if len(compatTypes) == 0 {
			slog.Debug("no compatibility issues detected in iteration", "iteration", iteration)
			break
		}

		// Filter to only new fixes
		var newFixes []compat.CompatType
		for _, ct := range compatTypes {
			if !appliedSet[ct] {
				newFixes = append(newFixes, ct)
			}
		}

		if len(newFixes) == 0 {
			slog.Debug("no new fixes to apply", "iteration", iteration)
			break
		}

		// Show what issues were found (only on first iteration)
		if iteration == 1 {
			fmt.Println()
			ui.Info("Detected legacy SSH compatibility issues:")
			for _, ct := range newFixes {
				compatCfg := compat.CompatConfigs[ct]
				fmt.Printf("    - %s\n", compatCfg.Name)
			}
			fmt.Println()

			// Ask user if they want to apply fixes (only once)
			confirmed, err := ui.Confirm("Apply compatibility fixes to SSH config?", true)
			if err != nil || !confirmed {
				return false
			}
		} else {
			// Subsequent iterations - just show what's being fixed
			ui.Info("Applying additional fixes:")
			for _, ct := range newFixes {
				compatCfg := compat.CompatConfigs[ct]
				fmt.Printf("    - %s\n", compatCfg.Name)
			}
		}

		// Apply fixes
		if err := sshconfig.ApplyCompatFixes(hostEntry, newFixes); err != nil {
			ui.Error("Failed to apply fixes: %s", err)
			return false
		}

		// Write updated config
		if err := parser.WriteFile(parsedCfg); err != nil {
			ui.Error("Failed to write config: %s", err)
			return false
		}

		// Track applied fixes
		for _, ct := range newFixes {
			appliedSet[ct] = true
			allFixesApplied = append(allFixesApplied, ct)
		}

		slog.Debug("applied compat fixes", "iteration", iteration, "fixes", len(newFixes))
	}

	if len(allFixesApplied) > 0 {
		ui.Success("Compatibility fixes applied")
		fmt.Println()
		return true
	}

	return false
}

// parseUserHost splits user@host into username and hostname components.
// If no @ is present, returns empty username and the original input as hostname.
func parseUserHost(input string) (username, hostname string) {
	if idx := strings.LastIndex(input, "@"); idx != -1 {
		return input[:idx], input[idx+1:]
	}
	return "", input
}

// resolveUsername extracts username from SSH args, SSH config, or nssh config.
func resolveUsername(hostname string, sshArgs []string, cfg *config.Config) string {
	// Check for -l flag in args (highest priority)
	for i, arg := range sshArgs {
		if arg == "-l" && i+1 < len(sshArgs) {
			return sshArgs[i+1]
		}
		if strings.HasPrefix(arg, "-l") && len(arg) > 2 {
			return arg[2:]
		}
	}

	// Check for user@host format (hostname might contain @)
	if idx := strings.LastIndex(hostname, "@"); idx != -1 {
		return hostname[:idx]
	}

	// Check SSH config for User directive
	parser := sshconfig.NewParser()
	if host, err := parser.FindHost(hostname); err == nil && host != nil {
		if user := host.User(); user != "" {
			return user
		}
	}

	// Use default from nssh config
	return cfg.Host.Defaults.DefaultUser
}

// newHostCmd creates the host subcommand for managing SSH hosts.
func newHostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Manage SSH host files",
		Long:  "Manage SSH host configurations in your SSH config files.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(host.NewListCmd())
	cmd.AddCommand(host.NewAddCmd())
	cmd.AddCommand(host.NewGetCmd())
	cmd.AddCommand(host.NewEditCmd())
	cmd.AddCommand(host.NewRemoveCmd())
	cmd.AddCommand(host.NewSortCmd())

	ui.ApplyStyledHelpRecursive(cmd)
	return cmd
}

// newCtxCmd creates the ctx subcommand for managing credential contexts.
func newCtxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ctx",
		Short: "Manage credential contexts",
		Long:  "Manage credential contexts for organizing hosts and storing authentication.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(ctx.NewListCmd())
	cmd.AddCommand(ctx.NewAddCmd())
	cmd.AddCommand(ctx.NewGetCmd())
	cmd.AddCommand(ctx.NewEditCmd())
	cmd.AddCommand(ctx.NewRemoveCmd())

	// Opt-in to styled help for ctx command group
	ui.ApplyStyledHelpRecursive(cmd)

	return cmd
}

// newLogCmd creates the log subcommand for managing session recordings.
func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Manage session recordings",
		Long:  "Manage recorded SSH sessions including playback, export, and upload.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(log.NewListCmd())
	cmd.AddCommand(log.NewPlayCmd())
	cmd.AddCommand(log.NewDeleteCmd())
	cmd.AddCommand(log.NewUploadCmd())
	cmd.AddCommand(log.NewExportCmd())
	cmd.AddCommand(log.NewAuthCmd())
	cmd.AddCommand(log.NewSearchCmd())

	ui.ApplyStyledHelpRecursive(cmd)
	return cmd
}

// newCpCmd creates the cp subcommand for SCP file transfers.
func newCpCmd() *cobra.Command {
	return cp.NewCmd()
}

// newBenchCmd creates the bench subcommand for performance testing.
// Note: ApplyStyledHelpRecursive is called by the parent (self) command.
func newBenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run performance benchmarks",
		Long:  "Run performance benchmarks for SSH and SCP connections.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(bench.NewSSHCmd())
	cmd.AddCommand(bench.NewSCPCmd())

	return cmd
}

// newSelfCmd creates the self subcommand for managing nssh installation.
func newSelfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage nssh installation",
		Long:  "Manage nssh installation, shell integration, and updates.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(self.NewInitCmd())
	cmd.AddCommand(self.NewStatusCmd())
	cmd.AddCommand(self.NewReinstallCmd())
	cmd.AddCommand(self.NewUninstallCmd())
	cmd.AddCommand(self.NewResetCmd())
	cmd.AddCommand(self.NewVersionCmd())
	cmd.AddCommand(self.NewRekeyCmd())
	cmd.AddCommand(self.NewPivCmd())
	cmd.AddCommand(self.NewCfgCmd())
	cmd.AddCommand(newBenchCmd())

	ui.ApplyStyledHelpRecursive(cmd)
	return cmd
}

// newListSubcommandsCmd creates the hidden __list-subcommands command.
// This is used by shell integration scripts to dynamically detect subcommands,
// preventing drift between the script and CLI.
func newListSubcommandsCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__list-subcommands",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			// Print one subcommand per line (fish splits on newlines)
			for _, subcmd := range []string{"host", "ctx", "log", "cp", "self", "lock", "unlock", "connect"} {
				fmt.Println(subcmd)
			}
		},
	}
}

// resolveHostname performs smart hostname resolution:
// - Exact match: returns hostname unchanged
// - Single partial match: returns the matched hostname
// - Multiple partial matches: opens fuzzy finder with query pre-filled
// - No matches: returns HostNotFoundError to trigger host add
func resolveHostname(hostname string) (string, error) {
	parser := sshconfig.NewParser()

	result, err := parser.MatchHost(hostname)
	if err != nil {
		slog.Debug("failed to match host", "err", err)
		return hostname, nil
	}

	// Single match found
	if result.Host != nil {
		if result.Host.Host != hostname {
			slog.Debug("auto-resolved hostname", "input", hostname, "resolved", result.Host.Host)
		}
		return result.Host.Host, nil
	}

	// Multiple matches - fuzzy select with query pre-filled
	if len(result.Suggestions) > 0 {
		sort.Strings(result.Suggestions)
		selected, err := ui.FuzzySelectString("Select host", result.Suggestions, hostname)
		if err != nil {
			return "", fmt.Errorf("fuzzy select: %w", err)
		}
		if selected == "" {
			return "", fmt.Errorf("selection canceled")
		}
		return selected, nil
	}

	// No matches - signal to spawn host add
	return "", &cli.HostNotFoundError{Hostname: hostname}
}

// spawnHostAdd spawns nssh host add with the hostname pre-filled.
func spawnHostAdd(hostname string) error {
	fmt.Printf("Host '%s' not found. Adding new host...\n", hostname)

	cmd := exec.Command(os.Args[0], "host", "add", hostname)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// runAgentMode runs the agent daemon.
// This is invoked via "nssh __agent" from the Spawn or SpawnPIV function.
// In software mode, the identity is passed via inherited pipe (fd 3).
// In PIV mode, the YubiKey PIN is passed via the same pipe.
// Readiness is signaled via another pipe (fd 4).
func runAgentMode() {
	// Get pipes from inherited file descriptors
	dataPipe := os.NewFile(3, "data-pipe") // identity (software) or PIN (PIV)
	readyPipe := os.NewFile(4, "ready-pipe")

	// Helper to signal error to parent and exit
	signalError := func(msg string) {
		if readyPipe != nil {
			_, _ = readyPipe.WriteString("err:" + msg + "\n")
			_ = readyPipe.Close()
		}
		os.Exit(1)
	}

	if dataPipe == nil || readyPipe == nil {
		signalError("missing pipe file descriptors")
	}

	// Load config for agent settings
	cfg, err := config.LoadDefault()
	if err != nil {
		signalError("failed to load config: " + err.Error())
	}

	// Detect mode from filesystem (single source of truth)
	paths := config.DefaultPaths()
	mode, err := vault.DetectSecurityMode(paths.ConfigDir)
	if err != nil {
		signalError("failed to detect security mode: " + err.Error())
	}

	// Create provider based on detected mode
	var provider agent.Provider

	switch mode {
	case agent.ModePIV:
		// PIV mode: pipe contains PIN, not identity
		pinSecret, err := secret.NewFromReader(dataPipe, 256)
		_ = dataPipe.Close()
		if err != nil {
			signalError("failed to read PIN: " + err.Error())
		}

		// Extract PIN from secure memory for the callback
		var pin string
		if err := pinSecret.UseString(func(s string) error {
			pin = strings.TrimSpace(s)
			return nil
		}); err != nil {
			pinSecret.Destroy()
			signalError("failed to access PIN: " + err.Error())
		}
		pinSecret.Destroy()

		// Create PIV provider with PIN callback
		// Note: NewPIVProvider will block waiting for YubiKey touch
		pivProvider, err := agent.NewPIVProvider(
			config.DefaultPaths().ConfigDir,
			func() (string, error) { return pin, nil },
		)
		if err != nil {
			signalError("PIV provider: " + err.Error())
		}
		provider = pivProvider

	default: // "software" or unset
		// Software mode: pipe contains the age X25519 identity
		// Read identity from pipe directly into memguard-protected memory.
		// Age X25519 identity is 74 bytes: "AGE-SECRET-KEY-1" + 58 chars.
		// We use 256 as max to handle any whitespace/newlines.
		identitySecret, err := secret.NewFromReader(dataPipe, 256)
		_ = dataPipe.Close()
		if err != nil {
			signalError("failed to read identity: " + err.Error())
		}

		// Parse the age identity from secure memory
		var identity *age.X25519Identity
		if err := identitySecret.UseString(func(s string) error {
			var parseErr error
			identity, parseErr = age.ParseX25519Identity(strings.TrimSpace(s))
			return parseErr
		}); err != nil {
			identitySecret.Destroy()
			signalError("failed to parse identity: " + err.Error())
		}

		// Destroy the secret - identity is now held by the age library
		identitySecret.Destroy()

		// Create provider with identity
		provider = agent.NewSoftwareProvider(identity)
	}

	// Resolve recording paths for the archiver (honors recording config/env)
	recordingSettings := recording.LoadRecordingSettings()

	// Set up agent config - pass config.AgentConfig directly
	agentCfg := agent.RuntimeConfig{
		Agent:        &cfg.Agent,
		Archive:      &cfg.Logging.Session.Archive,
		Logger:       slog.Default(),
		ReadyPipe:    readyPipe, // Signal readiness after socket creation
		RecordingDir: recordingSettings.Directory,
	}

	// Set up audit logger
	logger, err := logging.NewAuditLogger(slog.LevelInfo, &cfg.Logging.Audit, paths.StateDir)
	if err == nil {
		agentCfg.Logger = logger.Logger
		defer func() { _ = logger.Close() }()
	}

	// Run the agent (blocks until shutdown)
	// Note: agent.Run() signals readiness via cfg.ReadyPipe after socket creation.
	// If agent.Run() returns an error, it means it failed before signaling readiness,
	// so we need to signal the error to the parent process via the ready pipe.
	if err := agent.Run(provider, agentCfg); err != nil {
		// Signal error to parent - the pipe is still open because agent.Run()
		// failed before it could signal readiness
		if readyPipe != nil {
			_, _ = readyPipe.WriteString("err:" + err.Error() + "\n")
			_ = readyPipe.Close()
		}
		//nolint:gocritic // os.Exit is intentional here; logger.Close() is deferred in parent scope
		os.Exit(1)
	}
}
