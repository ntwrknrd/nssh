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
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// InitOptions configures the behavior of runInit.
type InitOptions struct {
	DryRun                  bool     // preview mode, no changes made
	Quiet                   bool     // minimal output
	CredentialProviderTypes []string // optional credential provider setup targets
	InventoryProviderTypes  []string // optional inventory provider setup targets
}

type initWarningError struct {
	message string
}

func (e initWarningError) Error() string {
	return e.message
}

type initConfigResult int

type initConfigOutcome struct {
	result             initConfigResult
	inventoryProviders []initInventoryProviderRequest
}

const (
	initConfigContinue initConfigResult = iota
	initConfigStop
)

// NewInitCmd creates the init subcommand.
func NewInitCmd() *cobra.Command {
	var opts InitOptions

	cmd := &cobra.Command{
		Use:   "init [flags]",
		Short: "Initialize configuration",
		Long: `Initialize nssh configuration.

Credentials are resolved through configured providers such as SOPS+age, 1Password,
or Bitwarden. The runtime agent brokers credential provider access.

To start fresh, use 'nssh self reset'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			if err := validateInitProviderFlags(opts); err != nil {
				return err
			}
			return runInit(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "preview changes without applying")
	cmd.Flags().StringArrayVar(&opts.CredentialProviderTypes, "cred", nil, "add credential provider (sops-age, 1password, bitwarden)")
	cmd.Flags().StringArrayVar(&opts.InventoryProviderTypes, "inv", nil, "add inventory provider (local, netbox, containerlab)")

	return cmd
}

func validateInitProviderFlags(opts InitOptions) error {
	for _, providerType := range opts.CredentialProviderTypes {
		switch providerType {
		case config.CredentialProviderSOPSAge, config.CredentialProvider1Password, config.CredentialProviderBitwarden:
		default:
			return fmt.Errorf("unsupported credential provider %q", providerType)
		}
	}
	for _, providerType := range opts.InventoryProviderTypes {
		switch providerType {
		case config.ProviderLocal, config.ProviderNetBox, config.ProviderContainerlab:
		default:
			return fmt.Errorf("unsupported inventory provider %q", providerType)
		}
	}
	return nil
}

func (o InitOptions) hasProviderSetup() bool {
	return len(o.CredentialProviderTypes) > 0 || len(o.InventoryProviderTypes) > 0
}

func runInit(opts InitOptions) error {
	paths := config.DefaultPaths()
	initOutcome := initConfigOutcome{result: initConfigContinue}

	// Header (skip in quiet mode)
	if !opts.Quiet {
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

			if !opts.DryRun {
				builtNSSH, builtAskpass, err := buildSourceInstall(projectRoot, installDir)
				if err != nil {
					ui.Error("Build failed: %v", err)
					return fmt.Errorf("build failed: %w", err)
				}
				binaryPath = builtNSSH
				ui.Success("Installed: %s", AbbreviatePath(binaryPath))
				ui.Success("Installed: %s", AbbreviatePath(builtAskpass))
			} else {
				binaryPath = filepath.Join(installDir, "nssh")
				ui.Success("Installed: %s", AbbreviatePath(binaryPath))
				ui.Success("Installed: %s", AbbreviatePath(filepath.Join(installDir, "nssh-askpass")))
			}

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
		outcome, err := ensureInitConfig(paths, opts)
		if err != nil {
			if isInitWarning(err) || ui.IsUserAbort(err) {
				ui.Warning("%v", err)
				return nil
			}
			return err
		}
		initOutcome = outcome
		if outcome.result == initConfigStop {
			return nil
		}
		if opts.hasProviderSetup() {
			return nil
		}
		ui.Info("Credential provider authentication is owned by SOPS+age, 1Password, or Bitwarden.")
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
				if err := offerInstallDependency(dep); err != nil {
					ui.Warning("  %v", err)
				}
			}
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
		showNextSteps(paths, initOutcome.inventoryProviders)
	}

	// Footer (skip in quiet mode)
	if !opts.Quiet {
	}

	return nil
}

func buildSourceInstall(projectRoot, installDir string) (string, string, error) {
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return "", "", fmt.Errorf("create install dir: %w", err)
	}
	binPath := filepath.Join(installDir, "nssh")
	askpassPath := filepath.Join(installDir, "nssh-askpass")
	for _, target := range []struct {
		output string
		pkg    string
	}{
		{output: binPath, pkg: "./cmd/nssh"},
		{output: askpassPath, pkg: "./cmd/nssh-askpass"},
	} {
		buildCmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", target.output, target.pkg)
		buildCmd.Dir = projectRoot
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return "", "", err
		}
	}
	return binPath, askpassPath, nil
}

// showNextSteps displays guidance for new users after successful init.
func showNextSteps(paths *config.Paths, inventoryProviders []initInventoryProviderRequest) {
	ui.SubSection("Next Steps")

	steps := initNextSteps(paths, inventoryProviders)

	ui.NumberedList(steps)
	fmt.Println()
	ui.Info("Run 'nssh --help' for more commands")
}

func initNextSteps(paths *config.Paths, inventoryProviders []initInventoryProviderRequest) []string {
	configFile := "~/.config/nssh/config.yaml"
	configDir := "~/.config/nssh"
	if paths != nil {
		configFile = AbbreviatePath(paths.ConfigFile)
		configDir = AbbreviatePath(paths.ConfigDir)
	}

	steps := []string{
		"Review " + configFile + " and uncomment only the root examples you want active.",
	}
	if len(inventoryProviders) == 0 {
		return append(steps,
			"Add inventory later with nssh self init --inv local, --inv netbox, or --inv containerlab.",
			"Connect after inventory exists: nssh <hostname>.",
		)
	}

	for _, provider := range inventoryProviders {
		path := providerConfigDisplayPath(configDir, "inventory", provider.Name)
		switch provider.Type {
		case config.ProviderLocal:
			steps = append(steps,
				"Local inventory: edit "+path+" to create groups/hosts, or use nssh inv set -g local/<group> and nssh inv set <host>.",
			)
		case config.ProviderNetBox:
			steps = append(steps,
				"NetBox inventory: edit "+path+" to create match groups, export the configured URL/token env vars, then run nssh inv refresh "+provider.Name+".",
			)
		case config.ProviderContainerlab:
			steps = append(steps,
				"Containerlab inventory: edit "+path+" to set kind/state match groups and verify jump_host, then run nssh inv refresh "+provider.Name+".",
			)
		}
	}
	steps = append(steps, "Inspect resolved inventory with nssh inv list and nssh inv get <host> before connecting.")
	return steps
}

func providerConfigDisplayPath(configDir, subdir, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "provider"
	}
	return filepath.Join(configDir, subdir, name+".yaml")
}

func ensureInitConfig(paths *config.Paths, opts InitOptions) (initConfigOutcome, error) {
	if _, err := os.Stat(paths.ConfigFile); err == nil {
		if opts.hasProviderSetup() {
			cfg, loadErr := config.Load(paths.ConfigFile)
			if loadErr != nil {
				return initConfigOutcome{result: initConfigStop}, loadErr
			}
			if err := applyProviderSetups(paths, cfg, opts, uiInitPrompter{}); err != nil {
				return initConfigOutcome{result: initConfigStop}, err
			}
			return initConfigOutcome{result: initConfigContinue}, nil
		}
		ui.Warning("Config file exists, skipping init: %s", AbbreviatePath(paths.ConfigFile))
		ui.Info("Run 'nssh self reset' to start fresh.")
		return initConfigOutcome{result: initConfigStop}, nil
	} else if err != nil && !os.IsNotExist(err) {
		return initConfigOutcome{result: initConfigStop}, err
	}

	var req initPlanRequest
	var err error
	if opts.hasProviderSetup() {
		req, err = explicitInitPlanRequest(uiInitPrompter{}, opts)
		if err != nil {
			return initConfigOutcome{result: initConfigStop}, err
		}
	} else {
		req, err = promptInitPlanRequest(uiInitPrompter{}, nil)
		if err != nil {
			return initConfigOutcome{result: initConfigStop}, err
		}
	}
	plan, err := buildInitPlan(req)
	if err != nil {
		return initConfigOutcome{result: initConfigStop}, err
	}

	ui.Info("%s", plan.Summary())
	if opts.DryRun {
		return initConfigOutcome{result: initConfigContinue, inventoryProviders: plan.InventoryProviders}, nil
	}
	if err := prepareCredentialProviders(req.CredentialProviders, uiInitPrompter{}, opts.DryRun); err != nil {
		return initConfigOutcome{result: initConfigStop}, err
	}
	if err := applyInitPlan(paths, plan); err != nil {
		return initConfigOutcome{result: initConfigStop}, err
	}
	ui.Success("Config file: %s", AbbreviatePath(paths.ConfigFile))
	return initConfigOutcome{result: initConfigContinue, inventoryProviders: plan.InventoryProviders}, nil
}

func isInitWarning(err error) bool {
	_, ok := err.(initWarningError)
	return ok
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
func offerInstallDependency(dep Dependency) error {
	if dep.GitHub == nil {
		return fmt.Errorf("no GitHub release info for %s", dep.Name)
	}

	result, _ := ui.Confirm(fmt.Sprintf("  Install %s?", dep.Name), true)
	if !result {
		return nil
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
