package self

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

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
	shellCmd, err := installShellCommand(release)
	if err != nil {
		return err
	}

	result, err := runInstallerWithEvents(shellCmd)
	if err != nil {
		if result.Output != "" {
			fmt.Fprint(os.Stderr, result.Output)
		}
		if result.Errors != "" {
			fmt.Fprint(os.Stderr, result.Errors)
		}
		fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
		return &exit.ExitError{Code: 1}
	}
	if result.Path == "" {
		result.Path = filepath.Join(homeDir(), ".local", "bin", "nssh")
	}
	if result.Version == "" {
		result.Version = "unknown"
	}
	fmt.Printf("Installed %s (%s)\n", AbbreviatePath(result.Path), result.Version)

	// Check if ~/.local/bin is on PATH
	installDir := filepath.Join(homeDir(), ".local", "bin")
	if FindBinary() == "" {
		fmt.Println()
		ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
		return nil
	}

	return nil
}

var releasePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func installShellCommand(release string) (string, error) {
	release, err := normalizeRelease(release)
	if err != nil {
		return "", err
	}
	if script := localInstallScript(); script != "" {
		if release == "" {
			return fmt.Sprintf("sh %s --events", shellQuote(script)), nil
		}
		return fmt.Sprintf("sh %s --events --release %s", shellQuote(script), release), nil
	}
	if release == "" {
		return fmt.Sprintf("curl -fsSL %s | sh -s -- --events", installScriptURL), nil
	}
	return fmt.Sprintf("curl -fsSL %s | sh -s -- --events --release %s", installScriptURL, release), nil
}

func localInstallScript() string {
	projectRoot := FindProjectRoot()
	if projectRoot == "" {
		return ""
	}
	script := filepath.Join(projectRoot, "scripts", "install.sh")
	if FileExists(script) {
		return script
	}
	return ""
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type installerResult struct {
	Version string
	Path    string
	Output  string
	Errors  string
}

func runInstallerWithEvents(shellCmd string) (installerResult, error) {
	var result installerResult
	var stdoutText strings.Builder
	var stderrText strings.Builder

	err := ui.RunWithStatusSpinner("Installing nssh", func(update func(string)) error {
		installCmd := exec.Command("sh", "-c", shellCmd)
		stdout, err := installCmd.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err := installCmd.StderrPipe()
		if err != nil {
			return err
		}
		if err := installCmd.Start(); err != nil {
			return err
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(&stderrText, stderr)
		}()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			kind, data, ok := parseInstallEvent(line)
			if !ok {
				stdoutText.WriteString(line)
				stdoutText.WriteByte('\n')
				continue
			}
			switch kind {
			case "status":
				update(data)
			case "version":
				result.Version = data
			case "path":
				result.Path = data
			}
		}
		scanErr := scanner.Err()
		waitErr := installCmd.Wait()
		wg.Wait()
		if scanErr != nil && waitErr == nil {
			return scanErr
		}
		return waitErr
	})

	result.Output = stdoutText.String()
	result.Errors = stderrText.String()
	return result, err
}

func parseInstallEvent(line string) (string, string, bool) {
	event, data, ok := strings.Cut(line, "\t")
	if !ok {
		return "", "", false
	}
	data = strings.TrimSpace(data)
	switch event {
	case "NSSH_INSTALL_STATUS":
		return "status", data, true
	case "NSSH_INSTALL_VERSION":
		return "version", data, true
	case "NSSH_INSTALL_PATH":
		return "path", data, true
	default:
		return "", "", false
	}
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

	// Find project root
	projectRoot := FindProjectRoot()
	if projectRoot == "" {
		ui.Error("Not in nssh project directory")
		fmt.Println()
		fmt.Println("This command must be run from within the nssh source directory.")
		fmt.Println("The directory must contain a go.mod file.")
		return &exit.ExitError{Code: 1}
	}

	// Build and install to ~/.local/bin
	installDir := filepath.Join(homeDir(), ".local", "bin")
	binPath := filepath.Join(installDir, "nssh")
	askpassPath := filepath.Join(installDir, "nssh-askpass")

	if err := os.MkdirAll(installDir, 0755); err != nil {
		ui.Error("Failed to create install dir: %v", err)
		return &exit.ExitError{Code: 1}
	}

	var buildOutput []byte
	err := ui.RunWithSpinner("", func() error {
		for _, target := range []struct {
			output string
			pkg    string
		}{
			{output: binPath, pkg: "./cmd/nssh"},
			{output: askpassPath, pkg: "./cmd/nssh-askpass"},
		} {
			buildCmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", target.output, target.pkg)
			buildCmd.Dir = projectRoot
			out, runErr := buildCmd.CombinedOutput()
			buildOutput = append(buildOutput, out...)
			if runErr != nil {
				return runErr
			}
		}
		return nil
	})
	if len(buildOutput) > 0 {
		fmt.Fprint(os.Stderr, string(buildOutput))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
		return &exit.ExitError{Code: 1}
	}
	fmt.Printf("Built %s\n", AbbreviatePath(binPath))
	fmt.Printf("Built %s\n", AbbreviatePath(askpassPath))

	// Check if ~/.local/bin is on PATH
	if FindBinary() == "" {
		fmt.Println()
		ui.Warning("Add to PATH: export PATH=\"%s:$PATH\"", AbbreviatePath(installDir))
		return nil
	}

	return nil
}
