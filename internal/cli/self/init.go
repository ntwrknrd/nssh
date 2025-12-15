package self

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/shell"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/ntwrknrd/nssh/internal/vault/software"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// InitOptions configures the behavior of runInit.
type InitOptions struct {
	SkipShell bool // skip shell integration entirely
	DryRun    bool // preview mode, no changes made
	Yes       bool // auto-confirm all prompts
	Quiet     bool // minimal output (for reinstall)
}

// NewInitCmd creates the init subcommand.
func NewInitCmd() *cobra.Command {
	var opts InitOptions

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize nssh",
		Long: `Initialize nssh with configuration and shell integration.

Automatically detects your shell and offers to install:
- Shell helper functions (bash/zsh/fish)
- Shell completions
- Profile sourcing snippet

Credentials are protected with a passphrase using scrypt encryption.
Session caching keeps credentials unlocked for 4 hours (configurable).

Use --skip-shell to opt out of shell integration entirely.
Use -y to skip all confirmation prompts.

To switch security modes, use 'nssh self rekey --software' or 'nssh self rekey --hardware'.
To start fresh, use 'nssh self reset'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.SkipShell, "skip-shell", false, "skip shell integration")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "preview changes without applying")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "skip confirmation prompts")

	return cmd
}

func runInit(opts InitOptions) error {
	paths := config.DefaultPaths()
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Header (skip in quiet mode)
	if !opts.Quiet {
		ui.CommandStart("INSTALL NSSH")
	}

	if opts.DryRun {
		ui.Info("Dry run mode - no changes will be made")
		fmt.Println()
	}

	// Check if nssh binary is on PATH (skip in quiet mode - reinstall handles this)
	if !opts.Quiet {
		binaryPath := FindBinary()
		if binaryPath == "" {
			// Check if we're in project directory - can build from source
			projectRoot := FindProjectRoot()
			if projectRoot == "" {
				ui.Error("nssh binary not found on PATH")
				fmt.Println()
				fmt.Println("Install nssh first, then run init again.")
				return fmt.Errorf("nssh not found on PATH")
			}

			// Build from source and install to ~/.local/bin
			ui.Info("Binary not on PATH, building from source...")
			installDir := filepath.Join(homeDir(), ".local", "bin")
			binaryPath = filepath.Join(installDir, "nssh")

			if !opts.DryRun {
				if err := os.MkdirAll(installDir, 0755); err != nil {
					return fmt.Errorf("create install dir: %w", err)
				}

				buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/nssh")
				buildCmd.Dir = projectRoot
				buildCmd.Stdout = os.Stdout
				buildCmd.Stderr = os.Stderr

				if err := buildCmd.Run(); err != nil {
					ui.Error("Build failed: %v", err)
					return fmt.Errorf("build failed: %w", err)
				}
			}
			ui.Success("Installed: %s", AbbreviatePath(binaryPath))

			// Check if ~/.local/bin is on PATH
			if FindBinary() == "" && !opts.DryRun {
				ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
			}
		} else {
			ui.Success("CLI binary: %s", AbbreviatePath(binaryPath))
		}
	}

	// Ensure directories exist (silent in quiet mode)
	if !opts.DryRun {
		if err := paths.EnsureDirs(); err != nil {
			return fmt.Errorf("failed to create directories: %w", err)
		}
	}
	if !opts.Quiet {
		ui.Success("Directories ready")
	}

	// Setup credential protection (skip in quiet mode - reinstall only refreshes shell)
	if !opts.Quiet {
		if !opts.DryRun {
			if err := setupCredentialProtection(paths, cfg, opts.Yes); err != nil {
				return fmt.Errorf("failed to setup credential protection: %w", err)
			}
		} else {
			ui.Info("Credential protection: would be configured (dry run)")
		}
	}

	// Ensure SSH config has Include directive for conf.d (silent in quiet mode)
	if !opts.DryRun {
		if err := ensureSSHConfigInclude(paths); err != nil {
			ui.Warning("SSH config setup: %v", err)
		} else if !opts.Quiet {
			ui.Success("SSH config ready")
		}
	}

	// Check dependencies
	deps := Dependencies()
	hasMissing := false

	for _, dep := range deps {
		if dep.Path == "" {
			hasMissing = true
		}
	}

	// Show dependencies section (in quiet mode, only if something is missing)
	if !opts.Quiet || hasMissing {
		if !opts.Quiet {
			ui.SubSection("Dependencies")
		}

		for _, dep := range deps {
			if dep.Path != "" {
				// In quiet mode, skip showing found dependencies
				if !opts.Quiet {
					ui.Success("%s: %s", dep.Name, dep.Path)
				}
				continue
			}

			// Missing dependency - always show
			if dep.Required {
				ui.Error("%s: not found (required for %s)", dep.Name, dep.Purpose)
			} else {
				ui.Warning("%s: not found (optional, for %s)", dep.Name, dep.Purpose)
			}

			// Offer to install (skip in dry-run mode, skip in quiet mode)
			if !opts.DryRun && !opts.Quiet {
				if err := offerInstallDependency(dep, opts.Yes); err != nil {
					ui.Warning("  %v", err)
				}
			}
		}
	}

	// Shell integration
	if !opts.Quiet {
		ui.SubSection("Shell Integration")
	}
	if opts.SkipShell {
		if !opts.Quiet {
			ui.Info("Shell integration: skipped (--skip-shell)")
		}
	} else {
		if err := installShellIntegration(paths, opts.DryRun, opts.Yes); err != nil {
			ui.Warning("Shell integration failed: %v", err)
		} else {
			shellInfo := DetectShell()
			ui.Warning("Restart your shell or run: source %s", AbbreviatePath(shellInfo.RCFile))
		}
	}

	// Determine final status
	missingRequired := false
	for _, dep := range deps {
		if dep.Path == "" && dep.Required {
			missingRequired = true
		}
	}

	// Final status message (skip in quiet mode)
	if !opts.Quiet {
		ui.SubSection("Status")
	}
	var finalStatus ui.StatusType
	switch {
	case opts.DryRun:
		ui.Info("Dry run complete - no changes were made")
		finalStatus = ui.StatusNoop
	case missingRequired:
		ui.Warning("nssh initialized, but missing required dependencies!")
		fmt.Println()
		fmt.Println("Install ssh and scp before using nssh.")
		finalStatus = ui.StatusWarning
	default:
		if !opts.Quiet {
			ui.Success("nssh initialized successfully!")
		}
		finalStatus = ui.StatusSuccess
	}

	// Show next steps for new users (skip in quiet mode)
	if !opts.Quiet && finalStatus == ui.StatusSuccess {
		showNextSteps()
	}

	// Footer (skip in quiet mode)
	if !opts.Quiet {
		ui.CommandEnd(finalStatus)
	}

	return nil
}

func installShellIntegration(paths *config.Paths, dryRun, yes bool) error {
	shellInfo := DetectShell()

	if shellInfo.Name == "unknown" {
		ui.Warning("Unknown shell - skipping shell integration")
		return nil
	}

	// Confirm installation
	if !yes && !dryRun {
		result, _ := ui.Confirm(fmt.Sprintf("Install shell integration for %s?", shellInfo.Name), true)
		if !result {
			ui.Info("Shell integration: skipped (user declined)")
			return nil
		}
	}

	// Write shell integration script
	shareDir := paths.DataDir
	var scriptPath string
	var scriptContent string

	switch shellInfo.Name {
	case "fish":
		scriptPath = filepath.Join(shareDir, "nssh-shell-integration.fish")
		scriptContent = shell.FishIntegration
	default: // bash, zsh
		scriptPath = filepath.Join(shareDir, "nssh-shell-integration.sh")
		scriptContent = shell.BashZshIntegration
	}

	if !dryRun {
		if err := os.MkdirAll(shareDir, 0755); err != nil {
			return fmt.Errorf("create share dir: %w", err)
		}
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0644); err != nil {
			return fmt.Errorf("write script: %w", err)
		}
	}
	ui.Success("Shell script: %s", AbbreviatePath(scriptPath))

	// Install completions
	if err := installCompletions(shellInfo.Name, dryRun); err != nil {
		ui.Warning("Completions: %v", err)
	} else {
		ui.Success("Completions installed")
	}

	// Append source snippet to rc file
	if err := appendSourceSnippet(shellInfo, scriptPath, dryRun); err != nil {
		ui.Warning("RC file update: %v", err)
	} else {
		ui.Success("RC file updated: %s", AbbreviatePath(shellInfo.RCFile))
	}

	return nil
}

func installCompletions(shellName string, dryRun bool) error {
	if rootCmd == nil {
		return fmt.Errorf("root command not set")
	}

	home := homeDir()
	var completionPath string

	switch shellName {
	case "fish":
		completionPath = filepath.Join(home, ".config", "fish", "completions", "nssh.fish")
	case "zsh":
		// Use ~/.zsh/completions if it exists, otherwise create it
		completionDir := filepath.Join(home, ".zsh", "completions")
		completionPath = filepath.Join(completionDir, "_nssh")
	case "bash":
		completionDir := filepath.Join(home, ".bash_completion.d")
		completionPath = filepath.Join(completionDir, "nssh")
	default:
		return fmt.Errorf("unsupported shell: %s", shellName)
	}

	if dryRun {
		return nil
	}

	// Ensure completion directory exists
	completionDir := filepath.Dir(completionPath)
	if err := os.MkdirAll(completionDir, 0755); err != nil {
		return fmt.Errorf("create completion dir: %w", err)
	}

	// Generate completions directly using Cobra API
	f, err := os.Create(completionPath)
	if err != nil {
		return fmt.Errorf("create completion file: %w", err)
	}
	defer func() { _ = f.Close() }()

	switch shellName {
	case "bash":
		err = rootCmd.GenBashCompletion(f)
	case "zsh":
		err = rootCmd.GenZshCompletion(f)
	case "fish":
		err = rootCmd.GenFishCompletion(f, true)
	}
	if err != nil {
		return fmt.Errorf("generate completions: %w", err)
	}

	return nil
}

func appendSourceSnippet(shellInfo ShellInfo, scriptPath string, dryRun bool) error {
	// Check if already installed
	if checkShellIntegration(shellInfo.RCFile) {
		return nil // Already installed
	}

	var snippet string
	switch shellInfo.Name {
	case "fish":
		snippet = fmt.Sprintf(`
%s
source %s
`, ShellIntegrationMarker, scriptPath)
	default: // bash, zsh
		snippet = fmt.Sprintf(`
%s
if [ -f "%s" ]; then
    source "%s"
fi
`, ShellIntegrationMarker, scriptPath, scriptPath)
	}

	if dryRun {
		return nil
	}

	// Ensure rc file directory exists
	rcDir := filepath.Dir(shellInfo.RCFile)
	if err := os.MkdirAll(rcDir, 0755); err != nil {
		return fmt.Errorf("create rc dir: %w", err)
	}

	// Append to rc file
	f, err := os.OpenFile(shellInfo.RCFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open rc file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(snippet); err != nil {
		return fmt.Errorf("write snippet: %w", err)
	}

	return nil
}

// RunInitQuiet runs init with minimal output (used by reinstall).
// Only refreshes shell integration, does not touch credential protection.
func RunInitQuiet(dryRun bool) error {
	return runInit(InitOptions{
		DryRun: dryRun,
		Yes:    true,
		Quiet:  true,
	})
}

// SecurityState represents the current state of credential protection.
type SecurityState int

// Security states for credential protection detection.
const (
	StateNewInstall           SecurityState = iota // No existing credentials
	StateLegacyNeedsMigration                      // Unprotected legacy storage detected
	StateAlreadyInitialized                        // Protected storage already configured
)

// SecurityMode represents the top-level security mode choice.
type SecurityMode string

// Security modes for credential protection.
const (
	SecurityModeSoftware SecurityMode = "software" // Software-based encryption with passphrase
	SecurityModeHardware SecurityMode = "hardware" // Hardware security module (e.g., YubiKey)
)

// detectSecurityState determines the current credential protection state.
func detectSecurityState(paths *config.Paths) SecurityState {
	pubKeyPath := filepath.Join(paths.ConfigDir, "age.pub")

	// Check for protected storage first (age.pub exists = already initialized)
	if _, err := os.Stat(pubKeyPath); err == nil {
		return StateAlreadyInitialized
	}

	// Check all known legacy key locations
	for _, keyPath := range legacyKeyPaths() {
		if _, err := os.Stat(keyPath); err == nil {
			return StateLegacyNeedsMigration
		}
	}

	return StateNewInstall
}

// credentialsExist checks if a credentials file exists at the expected location.
func credentialsExist(paths *config.Paths) bool {
	_, err := os.Stat(paths.CredentialsFile)
	return err == nil
}

// verifyCurrentSetupCanDecrypt attempts to decrypt credentials with the current setup.
// Returns nil if successful, error otherwise.
func verifyCurrentSetupCanDecrypt(paths *config.Paths) error {
	mgr, err := clisession.NewManager(
		vault.Auto(),
		vault.WithPaths(paths),
	)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	// Try to list contexts (triggers decryption)
	_, err = mgr.ListContexts()
	return err
}

func isVaultLockedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, software.ErrNeedsUnlock) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "vault locked")
}

func canPromptForUnlock(skipPrompts bool) bool {
	if skipPrompts {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// setupCredentialProtection handles credential protection initialization.
func setupCredentialProtection(paths *config.Paths, cfg *config.Config, skipPrompts bool) error {
	state := detectSecurityState(paths)

	// Handle already initialized case - init is for first-time setup only
	if state == StateAlreadyInitialized {
		// Verify we can actually decrypt credentials with current setup
		if credentialsExist(paths) {
			if err := verifyCurrentSetupCanDecrypt(paths); err != nil {
				if isVaultLockedError(err) {
					ui.Warning("Credential protection is already configured but locked.")

					// Offer to unlock interactively when possible.
					if canPromptForUnlock(skipPrompts) {
						ui.Info("Unlock now to verify access?")
						proceed, perr := ui.Confirm("Unlock now?", true)
						if perr != nil {
							return fmt.Errorf("prompt unlock: %w", perr)
						}
						if proceed {
							unlockMgr, merr := clisession.NewManager(vault.Auto())
							if merr != nil {
								ui.Warning("Unlock failed: %v", merr)
							} else if uerr := clisession.Unlock(unlockMgr, false); uerr != nil {
								ui.Warning("Unlock failed: %v", uerr)
							} else if verr := verifyCurrentSetupCanDecrypt(paths); verr == nil {
								ui.Success("Credential protection: already configured")
								return nil
							}
						}
					} else {
						ui.Info("Run 'nssh unlock' to unlock the session.")
					}

					ui.Success("Credential protection: already configured (locked)")
					return nil
				}
				// Current setup can't decrypt - check if legacy key can help
				legacyKey := findExistingLegacyKey()
				if legacyKey != "" {
					ui.Warning("Current keys cannot decrypt credentials, but legacy key found")
					ui.Info("Removing broken keys and attempting migration from legacy key...")
					// Remove the broken keys so migration can create new ones
					_ = os.Remove(filepath.Join(paths.ConfigDir, "age.pub"))
					_ = os.Remove(filepath.Join(paths.ConfigDir, "age.key.enc"))
					// Switch to migration path
					state = StateLegacyNeedsMigration
				} else {
					ui.Error("Current keys cannot decrypt credentials: %v", err)
					ui.Error("No legacy key found to recover from")
					fmt.Println()
					ui.Info("Options:")
					fmt.Println("  1. Locate your original age key and copy it to ~/.config/nssh/age.key")
					fmt.Println("  2. Use 'nssh self reset' to start fresh (credentials will be lost)")
					return fmt.Errorf("broken state: credentials exist but cannot be decrypted")
				}
			} else {
				ui.Success("Credential protection: already configured")
				fmt.Println()
				ui.Info("To switch modes: nssh self rekey --software or --hardware")
				ui.Info("To start fresh: nssh self reset")
				return nil
			}
		} else {
			ui.Success("Credential protection: already configured")
			fmt.Println()
			ui.Info("To switch modes: nssh self rekey --software or --hardware")
			ui.Info("To start fresh: nssh self reset")
			return nil
		}
	}

	// CRITICAL: Check for orphaned credentials before proceeding.
	// If credentials exist but no key is found, block init to prevent data loss.
	if state == StateNewInstall {
		if err := checkOrphanedCredentials(); err != nil {
			return err
		}
	}

	// Handle migration from legacy key (always migrates to software mode)
	if state == StateLegacyNeedsMigration {
		ui.Info("Migrating from legacy unprotected storage...")
		if err := migrateLegacyToSoftware(paths, cfg); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		ui.Success("Migration complete")
		return nil
	}

	// New install - ask user to choose security mode
	ui.SubSection("Credential Protection")

	mode, err := selectSecurityMode()
	if err != nil {
		return err
	}

	switch mode {
	case SecurityModeHardware:
		return initHardwareMode(paths, cfg)
	default:
		return initSoftwareMode(paths, cfg)
	}
}

// selectSecurityMode prompts user to choose between software and hardware.
// In non-interactive environments (no TTY), defaults to software mode.
func selectSecurityMode() (SecurityMode, error) {
	// Auto-select software mode in non-interactive environments (CI, Docker)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return SecurityModeSoftware, nil
	}

	options := []ui.SelectOption{
		{Label: "Software (passphrase-protected key)", Value: "software"},
		{Label: "Hardware (YubiKey/security token)", Value: "hardware"},
	}

	choice, err := ui.Select("Select security mode", options)
	if err != nil {
		return "", err
	}

	return SecurityMode(choice), nil
}

// initSoftwareMode initializes passphrase-protected credentials.
// Supports NSSH_PASSPHRASE env var for non-interactive initialization (CI/testing).
func initSoftwareMode(paths *config.Paths, cfg *config.Config) error {
	ksCfg := software.Config{
		ConfigDir:           paths.ConfigDir,
		DataDir:             paths.DataDir,
		StateDir:            paths.StateDir,
		ScryptWorkFactor:    cfg.Agent.Security.Software.ScryptWorkFactor,
		PassphraseMinLength: cfg.Agent.Security.Software.PassphraseMinLength,
	}

	ks, err := software.New(ksCfg)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	// Check for non-interactive passphrase (CI/testing)
	// Note: force=false since setupCredentialProtection handles state checking
	if passphrase := os.Getenv("NSSH_PASSPHRASE"); passphrase != "" {
		if err := ks.InitializeWithPassphrase([]byte(passphrase), false); err != nil {
			return err
		}
	} else {
		if err := promptAndInitialize(ks); err != nil {
			return err
		}
	}

	ui.Success("Credentials protected with passphrase")
	return nil
}

// ensureSSHConfigInclude ensures ~/.ssh/config has an Include directive for conf.d.
// This is required for SSH to read host entries created by nssh.
func ensureSSHConfigInclude(paths *config.Paths) error {
	confD := filepath.Join(paths.SSHConfigDir, "conf.d")
	sshConfig := paths.SSHConfigFile

	// Create conf.d directory
	if err := os.MkdirAll(confD, 0700); err != nil {
		return fmt.Errorf("create conf.d: %w", err)
	}

	// Check if Include directive already exists
	includeDirective := fmt.Sprintf("Include %s/*", confD)

	content, err := os.ReadFile(sshConfig)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read ssh config: %w", err)
	}

	// Check if Include directive is already present
	if strings.Contains(string(content), includeDirective) ||
		strings.Contains(string(content), "Include ~/.ssh/conf.d/*") ||
		strings.Contains(string(content), "Include conf.d/*") {
		return nil // Already configured
	}

	// Prepend Include directive (must come before other directives)
	newContent := fmt.Sprintf("# nssh managed hosts\n%s\n\n%s", includeDirective, string(content))

	if err := os.WriteFile(sshConfig, []byte(newContent), 0600); err != nil {
		return fmt.Errorf("write ssh config: %w", err)
	}

	return nil
}

// showNextSteps displays guidance for new users after successful init.
func showNextSteps() {
	ui.SubSection("Next Steps")

	steps := []string{
		"Create a context: nssh ctx add <name>",
		"Add your first host: nssh host add",
		"Connect: nssh <hostname>",
	}

	ui.NumberedList(steps)
	fmt.Println()
	ui.Info("Run 'nssh --help' for more commands")
}

// offerInstallDependency offers to install a single missing dependency.
func offerInstallDependency(dep Dependency, autoYes bool) error {
	pm := DetectPackageManager()
	if pm == nil {
		return fmt.Errorf("no package manager found")
	}

	pkg := dep.PackageName(pm)
	installCmd := pm.InstallCommand(pkg)
	cmdStr := strings.Join(installCmd, " ")

	// Ask user (or auto-yes)
	if !autoYes {
		result, _ := ui.Confirm(fmt.Sprintf("  Install %s?", dep.Name), true)
		if !result {
			return nil
		}
	}

	ui.Info("  Running: %s", cmdStr)

	cmd := exec.Command(installCmd[0], installCmd[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	// Verify installation
	newDeps := Dependencies()
	for _, d := range newDeps {
		if d.Name == dep.Name && d.Path != "" {
			ui.Success("  %s: installed -> %s", d.Name, d.Path)
			return nil
		}
	}

	return nil
}
