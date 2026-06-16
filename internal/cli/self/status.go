package self

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// version, commit, and date are set via ldflags at build time
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// SetVersion sets the version info (called from main).
func SetVersion(v, c, d string) {
	version = v
	commit = c
	date = d
}

// NewStatusCmd creates the status subcommand.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status",
		Long: `Display the current installation status of nssh, including:
- Version information
- Binary location
- Configuration files
- Credential provider mappings and inventory auth setup

This helps diagnose setup issues and shows next steps for configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus()
		},
	}
}

func runStatus() error {
	paths := config.DefaultPaths()

	ui.SubSection("Version", true)
	var versionStr string
	if commit != "" {
		versionStr = fmt.Sprintf("%s (%s, %s, %s/%s)", version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	} else {
		versionStr = fmt.Sprintf("%s (%s, %s/%s)", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
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

	// Configuration
	ui.SubSection("Configuration")
	printFileStatus(paths.ConfigFile, "Config file")
	if cfg, err := config.LoadDefault(); err == nil {
		ui.StatusLineNeutral("Provider instances", fmt.Sprintf("%d", len(cfg.Credential.Provider)))
		ui.StatusLineNeutral("Host auth overrides", fmt.Sprintf("%d", len(cfg.Inventory.Host)))
		ui.StatusLineNeutral("Group auth mappings", fmt.Sprintf("%d", inventoryGroupAuthCount(cfg)))
	} else {
		ui.StatusLineNeutral("Credential providers", "config unavailable: "+err.Error())
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

	return nil
}

func inventoryGroupAuthCount(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	count := 0
	for _, provider := range cfg.Inventory.Provider {
		for _, group := range provider.Group {
			if group.Auth.IsSet() {
				count++
			}
		}
	}
	return count
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
