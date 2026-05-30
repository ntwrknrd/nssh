package self

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/shell"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
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
		Short: "Initialize configuration",
		Long: `Initialize nssh with configuration and shell integration.

Automatically detects your shell and offers to install:
- Shell helper functions (bash/zsh/fish)
- Shell completions
- Profile sourcing snippet

Credentials are protected with a passphrase using scrypt encryption.
Session caching keeps credentials unlocked for 4 hours (configurable).

Use --skip-shell to opt out of shell integration entirely.
Use -y to skip all confirmation prompts.

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

	// Install example config if none exists (skip in quiet mode)
	if !opts.Quiet {
		if err := ensureInitConfig(paths, opts); err != nil {
			ui.Warning("Config setup: %v", err)
		}
		ui.Info("Credential provider authentication is owned by Pass, 1Password, or Bitwarden.")
	}

	// Ensure SSH config has Include directive for nssh.d (silent in quiet mode)
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

// ensureSSHConfigInclude ensures ~/.ssh/config has an Include directive for nssh.d.
// This is required for SSH to read host entries created by nssh.
func ensureSSHConfigInclude(paths *config.Paths) error {
	nsshD := filepath.Join(paths.SSHConfigDir, "nssh.d")
	sshConfig := paths.SSHConfigFile

	// Create nssh.d directory
	if err := os.MkdirAll(nsshD, 0700); err != nil {
		return fmt.Errorf("create nssh.d: %w", err)
	}

	// Check if Include directive already exists
	includeDirective := fmt.Sprintf("Include %s/*", nsshD)

	content, err := os.ReadFile(sshConfig)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read ssh config: %w", err)
	}

	// Check if Include directive is already present
	if strings.Contains(string(content), includeDirective) ||
		strings.Contains(string(content), "Include ~/.ssh/nssh.d/*") ||
		strings.Contains(string(content), "Include nssh.d/*") {
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
		"Create a group: nssh inv set -g <name>",
		"Add your first host: nssh inv set <host>",
		"Connect: nssh <hostname>",
	}

	ui.NumberedList(steps)
	fmt.Println()
	ui.Info("Run 'nssh --help' for more commands")
}

func ensureInitConfig(paths *config.Paths, opts InitOptions) error {
	var existing *config.Config
	if _, err := os.Stat(paths.ConfigFile); err == nil {
		cfg, loadErr := config.Load(paths.ConfigFile)
		if loadErr != nil {
			return loadErr
		}
		existing = cfg
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	req := initPlanRequest{
		Yes:      opts.Yes,
		Existing: existing,
	}
	if !opts.Yes {
		var err error
		req, err = promptInitPlanRequest(uiInitPrompter{}, existing)
		if err != nil {
			return err
		}
	}
	plan, err := buildInitPlan(req)
	if err != nil {
		return err
	}

	ui.Info("%s", plan.Summary())
	if opts.DryRun {
		return nil
	}
	if err := applyInitPlan(paths, plan); err != nil {
		return err
	}
	ui.Success("Config file: %s", AbbreviatePath(paths.ConfigFile))
	return nil
}

// getLatestGitHubRelease fetches the latest release version from GitHub API.
func getLatestGitHubRelease(owner, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parse release: %w", err)
	}

	return release.TagName, nil
}

// installFromGitHubRelease downloads and installs a binary from GitHub releases.
func installFromGitHubRelease(dep Dependency) error {
	if dep.GitHub == nil {
		return fmt.Errorf("no GitHub release info for %s", dep.Name)
	}

	gh := dep.GitHub

	// Get latest version
	version, err := getLatestGitHubRelease(gh.Owner, gh.Repo)
	if err != nil {
		return fmt.Errorf("get latest version: %w", err)
	}
	ui.Info("  Latest version: %s", version)

	// Determine asset filename for current platform
	assetName := gh.AssetPattern(version, runtime.GOOS, runtime.GOARCH)
	downloadURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		gh.Owner, gh.Repo, version, assetName)

	ui.Info("  Downloading %s...", assetName)

	// Download the asset
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Install to ~/.local/bin
	installDir := filepath.Join(homeDir(), ".local", "bin")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("create install dir: %w", err)
	}

	binPath := filepath.Join(installDir, dep.Name)

	// Handle archive extraction if needed
	if gh.IsArchive {
		binaryName := gh.BinaryName
		if binaryName == "" {
			binaryName = dep.Name
		}
		content, err := extractBinaryFromTarGz(resp.Body, binaryName)
		if err != nil {
			return fmt.Errorf("extract archive: %w", err)
		}
		if err := os.WriteFile(binPath, content, 0755); err != nil {
			return fmt.Errorf("write binary: %w", err)
		}
	} else {
		// Direct binary download
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read binary: %w", err)
		}
		if err := os.WriteFile(binPath, content, 0755); err != nil {
			return fmt.Errorf("write binary: %w", err)
		}
	}

	return nil
}

// extractBinaryFromTarGz extracts a specific binary from a tar.gz archive.
func extractBinaryFromTarGz(r io.Reader, binaryName string) ([]byte, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}

		// Look for the binary (might be in root or subdirectory)
		name := filepath.Base(header.Name)
		if name == binaryName && header.Typeflag == tar.TypeReg {
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read binary from archive: %w", err)
			}
			return content, nil
		}
	}

	return nil, fmt.Errorf("binary %q not found in archive", binaryName)
}

// offerInstallDependency offers to install a single missing dependency from GitHub releases.
func offerInstallDependency(dep Dependency, autoYes bool) error {
	if dep.GitHub == nil {
		return fmt.Errorf("no GitHub release info for %s", dep.Name)
	}

	// Ask user (or auto-yes)
	if !autoYes {
		result, _ := ui.Confirm(fmt.Sprintf("  Install %s?", dep.Name), true)
		if !result {
			return nil
		}
	}

	ui.Info("  Installing from GitHub releases...")
	if err := installFromGitHubRelease(dep); err != nil {
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

	// Binary installed but not on PATH
	installDir := filepath.Join(homeDir(), ".local", "bin")
	binPath := filepath.Join(installDir, dep.Name)
	ui.Success("  %s: installed -> %s", dep.Name, AbbreviatePath(binPath))
	ui.Warning("  Note: %s may not be on your PATH", AbbreviatePath(installDir))
	return nil
}
