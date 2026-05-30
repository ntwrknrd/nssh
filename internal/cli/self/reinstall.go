package self

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

const installScriptURL = "https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh"

// NewReinstallCmd creates the reinstall subcommand.
func NewReinstallCmd() *cobra.Command {
	var dev bool
	var release string

	cmd := &cobra.Command{
		Use:   "reinstall",
		Short: "Install latest from GitHub",
		Long: `Download and install the latest nssh release from GitHub.

This command:
1. Downloads the install script from GitHub
2. Installs the latest release binary to ~/.local/bin

Use --release to install a specific GitHub release tag.
Use --dev to build from source instead.

To reinitialize credentials, use 'nssh self rekey' or 'nssh self reset'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dev && release != "" {
				return fmt.Errorf("--release cannot be used with --dev")
			}
			if dev {
				return runReinstallDev()
			}
			return runReinstallRelease(release)
		},
	}

	cmd.Flags().BoolVar(&dev, "dev", false, "build from source")
	cmd.Flags().StringVar(&release, "release", "", "GitHub release tag")

	return cmd
}

func runReinstallRelease(release string) error {
	ui.CommandStart("REINSTALL NSSH")

	// Run install script from GitHub
	ui.SubSection("Download and Install")
	ui.Info("Fetching latest release from GitHub...")

	shellCmd, err := installShellCommand(release)
	if err != nil {
		return err
	}

	// Use sh -c to pipe curl output to sh
	installCmd := exec.Command("sh", "-c", shellCmd)
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		ui.Error("Installation failed: %v", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	// Check if ~/.local/bin is on PATH
	installDir := filepath.Join(homeDir(), ".local", "bin")
	if FindBinary() == "" {
		fmt.Println()
		ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

var releasePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func installShellCommand(release string) (string, error) {
	release, err := normalizeRelease(release)
	if err != nil {
		return "", err
	}
	if release == "" {
		return fmt.Sprintf("curl -fsSL %s | sh", installScriptURL), nil
	}
	return fmt.Sprintf("curl -fsSL %s | sh -s -- --release %s", installScriptURL, release), nil
}

func normalizeRelease(release string) (string, error) {
	release = strings.TrimSpace(release)
	if release == "" {
		return "", nil
	}
	if !releasePattern.MatchString(release) {
		return "", fmt.Errorf("invalid release tag %q", release)
	}
	if release[0] >= '0' && release[0] <= '9' {
		return "v" + release, nil
	}
	return release, nil
}

func runReinstallDev() error {
	ui.CommandStart("REINSTALL NSSH (DEV)")

	// Find project root
	projectRoot := FindProjectRoot()
	if projectRoot == "" {
		ui.Error("Not in nssh project directory")
		fmt.Println()
		fmt.Println("This command must be run from within the nssh source directory.")
		fmt.Println("The directory must contain a go.mod file.")
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}
	ui.Info("Project root: %s", AbbreviatePath(projectRoot))

	// Build and install to ~/.local/bin
	ui.SubSection("Build")
	installDir := filepath.Join(homeDir(), ".local", "bin")
	binPath := filepath.Join(installDir, "nssh")

	if err := os.MkdirAll(installDir, 0755); err != nil {
		ui.Error("Failed to create install dir: %v", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	ui.Info("Building nssh...")
	buildCmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", binPath, "./cmd/nssh")
	buildCmd.Dir = projectRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		ui.Error("Build failed: %v", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}
	ui.Success("Installed: %s", AbbreviatePath(binPath))

	// Check if ~/.local/bin is on PATH
	if FindBinary() == "" {
		fmt.Println()
		ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
