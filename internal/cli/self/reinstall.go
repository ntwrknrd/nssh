package self

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

const installScriptURL = "https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh"

// NewReinstallCmd creates the reinstall subcommand.
func NewReinstallCmd() *cobra.Command {
	var dev bool
	var hardware bool

	cmd := &cobra.Command{
		Use:   "reinstall",
		Short: "Reinstall nssh from latest GitHub release",
		Long: `Download and install the latest nssh release from GitHub.

This command:
1. Downloads the install script from GitHub
2. Installs the latest release binary to ~/.local/bin
3. Refreshes shell integration

Use --hardware for YubiKey/PIV support.
Use --dev to build from source instead (for development workflows).

To reinitialize credentials, use 'nssh self rekey' or 'nssh self reset'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dev {
				return runReinstallDev(hardware)
			}
			return runReinstallRelease(hardware)
		},
	}

	cmd.Flags().BoolVar(&dev, "dev", false, "build from source instead of downloading release")
	cmd.Flags().BoolVar(&hardware, "hardware", false, "build with hardware security support (YubiKey/PIV)")

	return cmd
}

func runReinstallRelease(hardware bool) error {
	ui.CommandStart("REINSTALL NSSH")

	// Track warnings for final status
	hasWarnings := false

	// Run install script from GitHub
	ui.SubSection("Download and Install")
	ui.Info("Fetching latest release from GitHub...")

	// Build install command with optional --hardware flag
	var shellCmd string
	if hardware {
		shellCmd = fmt.Sprintf("curl -fsSL %s | sh -s -- --hardware", installScriptURL)
	} else {
		shellCmd = fmt.Sprintf("curl -fsSL %s | sh", installScriptURL)
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

	// Refresh shell integration
	ui.SubSection("Shell Integration")
	if err := RunInitQuiet(false); err != nil {
		ui.Warning("Shell integration refresh failed: %v", err)
		hasWarnings = true
	} else {
		ui.Success("Shell integration refreshed")
	}

	// Check if ~/.local/bin is on PATH
	installDir := filepath.Join(homeDir(), ".local", "bin")
	if FindBinary() == "" {
		fmt.Println()
		ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
		hasWarnings = true
	}

	// Footer with appropriate status
	if hasWarnings {
		ui.CommandEnd(ui.StatusWarning)
	} else {
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}

func runReinstallDev(hardware bool) error {
	ui.CommandStart("REINSTALL NSSH (DEV)")

	// Track warnings for final status
	hasWarnings := false

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

	// Build command varies based on hardware flag
	var buildCmd *exec.Cmd
	if hardware {
		ui.Info("Building nssh with hardware support...")
		buildCmd = exec.Command("go", "build", "-tags", "hardware", "-o", binPath, "./cmd/nssh")
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	} else {
		ui.Info("Building nssh...")
		buildCmd = exec.Command("go", "build", "-o", binPath, "./cmd/nssh")
	}
	buildCmd.Dir = projectRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		ui.Error("Build failed: %v", err)
		if hardware {
			ui.Info("Hardware builds require CGO and PC/SC libraries")
			ui.Info("  macOS: PCSC.framework is built-in")
			ui.Info("  Linux: apt install libpcsclite-dev pcscd")
		}
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}
	if hardware {
		ui.Success("Installed (with hardware): %s", AbbreviatePath(binPath))
	} else {
		ui.Success("Installed: %s", AbbreviatePath(binPath))
	}

	// Refresh shell integration
	if err := RunInitQuiet(false); err != nil {
		ui.Warning("Shell integration refresh failed: %v", err)
		hasWarnings = true
	}

	// Check if ~/.local/bin is on PATH
	if FindBinary() == "" {
		fmt.Println()
		ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
		hasWarnings = true
	}

	// Footer with appropriate status
	if hasWarnings {
		ui.CommandEnd(ui.StatusWarning)
	} else {
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}
