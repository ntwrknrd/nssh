package self

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// version, commit, date, and features are set via ldflags at build time
var (
	version  = "dev"
	commit   = ""
	date     = ""
	features = ""
)

// SetVersion sets the version info (called from main).
func SetVersion(v, c, d, f string) {
	version = v
	commit = c
	date = d
	features = f
}

// NewStatusCmd creates the status subcommand.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show installation status",
		Long: `Display the current installation status of nssh, including:
- Version information
- Binary location
- Configuration files
- Credentials and encryption setup
- Shell integration status

This helps diagnose setup issues and shows next steps for configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus()
		},
	}
}

func runStatus() error {
	paths := config.DefaultPaths()

	ui.CommandStart("NSSH STATUS")

	// Version (skip newline - CommandStart already added one)
	ui.SubSection("Version", true)
	var versionStr string
	if commit != "" {
		versionStr = fmt.Sprintf("%s (%s, %s, %s/%s)", version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	} else {
		versionStr = fmt.Sprintf("%s (%s, %s/%s)", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	}
	if features != "" {
		versionStr += fmt.Sprintf(" [%s]", features)
	}
	ui.StatusLineNeutralText(versionStr)

	// Dependencies
	ui.SubSection("Dependencies")
	deps := Dependencies()
	for _, dep := range deps {
		label := dep.Name
		if dep.Required {
			label += " (required)"
		} else {
			label += " (optional)"
		}

		if dep.Path != "" {
			printStatus(true, label, dep.Path)
		} else {
			printStatus(false, label, fmt.Sprintf("not found - %s", dep.Purpose))
		}
	}

	// CLI binary
	binaryPath := FindBinary()
	if binaryPath != "" {
		printStatus(true, "CLI binary", AbbreviatePath(binaryPath))
	} else {
		printStatus(false, "CLI binary", "not found on PATH")
	}

	// Shell Integration (after dependencies)
	ui.SubSection("Shell Integration")

	shellInfo := DetectShell()
	printStatus(true, "Detected shell", shellInfo.Name)

	shellIntegrated := checkShellIntegration(shellInfo.RCFile)
	if shellIntegrated {
		printStatus(true, "Shell integration", "installed in "+AbbreviatePath(shellInfo.RCFile))
	} else {
		printStatus(false, "Shell integration", "not installed")
	}

	completionInstalled := checkCompletions(shellInfo.Name)
	if completionInstalled {
		printStatus(true, "Completions", "installed")
	} else {
		printStatus(false, "Completions", "not installed")
	}

	// Configuration
	ui.SubSection("Configuration")
	printFileStatus(paths.ConfigFile, "Config file")
	printFileStatus(paths.CredentialsFile, "Credentials")
	printFileStatus(filepath.Join(paths.ConfigDir, "age.pub"), "Public key")

	// Detect mode from filesystem and show appropriate key file
	detectedMode, detectErr := vault.DetectSecurityMode(paths.ConfigDir)
	if detectErr == nil {
		switch detectedMode {
		case agent.ModePIV:
			printFileStatus(filepath.Join(paths.ConfigDir, "piv.json"), "PIV keystore")
		default:
			printFileStatus(filepath.Join(paths.ConfigDir, "age.key.enc"), "Encrypted key")
		}
		securityMode := detectedMode
		if securityMode != agent.ModeSoftware {
			securityMode = fmt.Sprintf("hardware (%s)", securityMode)
		}
		ui.StatusLineNeutral("Security mode", securityMode)
	} else {
		ui.StatusLineNeutral("Security mode", "not initialized")
	}

	// SSH Config (with inventory hosts count)
	ui.SubSection("SSH Config")
	printFileStatus(paths.SSHConfigFile, "SSH config")

	// SSH nssh.d directory
	nsshD := filepath.Join(paths.SSHConfigDir, "nssh.d")
	if DirExists(nsshD) {
		files, _ := filepath.Glob(filepath.Join(nsshD, "*"))
		fileCount := 0
		for _, f := range files {
			if info, err := os.Stat(f); err == nil && !info.IsDir() {
				fileCount++
			}
		}
		// Count hosts across all include files
		hostCount := 0
		parser := sshconfig.NewParser()
		if includeFiles, err := parser.FindIncludeFiles(); err == nil {
			for _, includeFile := range includeFiles {
				if parsed, err := parser.ParseFile(includeFile); err == nil {
					hostCount += len(parsed.Hosts)
				}
			}
		}
		printStatus(true, "Include dir", fmt.Sprintf("%s (%d files, %d hosts)", AbbreviatePath(nsshD), fileCount, hostCount))
	} else {
		printStatus(false, "Include dir", AbbreviatePath(nsshD)+" (not found)")
	}

	// Logging
	ui.SubSection("Logging")

	// Audit log
	auditLogPath := filepath.Join(paths.StateDir, "audit.log")
	printFileStatus(auditLogPath, "Audit log")

	// Recordings directory with total size
	if DirExists(paths.RecordingsDir) {
		var castCount int
		var totalSize int64
		_ = filepath.WalkDir(paths.RecordingsDir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".cast") {
				castCount++
				if info, err := d.Info(); err == nil {
					totalSize += info.Size()
				}
			}
			return nil
		})
		var stats string
		if castCount == 0 {
			stats = "empty"
		} else {
			stats = formatBytes(totalSize)
		}
		ui.StatusLineNeutral("Recordings", fmt.Sprintf("%s (%s)", AbbreviatePath(paths.RecordingsDir), stats))
	} else {
		printStatus(false, "Recordings", "not configured")
	}

	// Session status (at the bottom - most actionable info)
	ui.SubSection("Session")
	if client, err := agent.Connect(); err == nil {
		if status, err := client.Status(); err == nil {
			if status.Mode == agent.ModeCache {
				ui.StatusLineNeutral("Status", "credential cache active")
			} else {
				printStatus(true, "Status", "unlocked")
			}
			ui.StatusLineNeutral("Idle in", formatDuration(status.RemainingIdle))
			ui.StatusLineNeutral("Ends in", formatDuration(status.RemainingLife))
		} else {
			// If we cannot read status, treat the agent as locked and avoid showing
			// expiry data that might be stale.
			printStatus(false, "Status", "locked")
		}
		_ = client.Close()
	} else {
		printStatus(false, "Status", "locked")
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func printStatus(ok bool, label, value string) {
	ui.StatusLine(ok, label, value)
}

func printFileStatus(path, label string) {
	if FileExists(path) {
		printStatus(true, label, AbbreviatePath(path))
	} else {
		printStatus(false, label, AbbreviatePath(path)+" (not found)")
	}
}

// formatBytes formats bytes into human-readable form (KB, MB, GB).
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatDuration formats seconds into a human-readable duration.
func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func checkShellIntegration(rcFile string) bool {
	f, err := os.Open(rcFile)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), ShellIntegrationMarker) {
			return true
		}
	}
	return false
}

func checkCompletions(shell string) bool {
	home := homeDir()

	switch shell {
	case "fish":
		return FileExists(filepath.Join(home, ".config", "fish", "completions", "nssh.fish"))
	case "zsh":
		// Check common zsh completion locations
		locations := []string{
			filepath.Join(home, ".oh-my-zsh", "completions", "_nssh"),
			filepath.Join(home, ".zsh", "completions", "_nssh"),
			"/usr/local/share/zsh/site-functions/_nssh",
		}
		for _, loc := range locations {
			if FileExists(loc) {
				return true
			}
		}
		return false
	case "bash":
		// Check common bash completion locations
		locations := []string{
			filepath.Join(home, ".bash_completion.d", "nssh"),
			"/etc/bash_completion.d/nssh",
			"/usr/local/etc/bash_completion.d/nssh",
		}
		for _, loc := range locations {
			if FileExists(loc) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
