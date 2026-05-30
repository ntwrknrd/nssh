package self

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewUninstallCmd creates the uninstall subcommand.
func NewUninstallCmd() *cobra.Command {
	var (
		keepConfig     bool
		keepRecordings bool
		dryRun         bool
		yes            bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall nssh",
		Long: `Remove nssh files.

This command removes:
- The nssh binary (from ~/.local/bin)
- Installed dependencies (asciinema, agg, fzf from ~/.local/bin)

Optionally removes (unless --keep-config or --keep-recordings):
- nssh config and local state
- Session recordings

Use --dry-run to preview what would be removed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(keepConfig, keepRecordings, dryRun, yes)
		},
	}

	cmd.Flags().BoolVar(&keepConfig, "keep-config", false, "keep nssh config and local state")
	cmd.Flags().BoolVar(&keepRecordings, "keep-recordings", false, "keep session recordings")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompts")

	return cmd
}

func runUninstall(keepConfig, keepRecordings, dryRun, yes bool) error {
	paths := config.DefaultPaths()

	ui.CommandStart("UNINSTALL NSSH")

	if dryRun {
		ui.Info("Dry run mode - no changes will be made")
		fmt.Println()
	}

	// Confirm uninstallation
	if !yes && !dryRun {
		result, _ := ui.Confirm("Are you sure you want to uninstall nssh?", false)
		if !result {
			ui.Info("Uninstall canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		fmt.Println()
	}

	// Track what we're removing
	hasErrors := false

	// 1. Remove binary
	ui.SubSection("Binary")
	binaryPath := FindBinary()
	if binaryPath != "" && FileExists(binaryPath) {
		if err := removeFile(binaryPath, dryRun); err != nil {
			ui.Warning("Failed to remove binary: %v", err)
			hasErrors = true
		} else {
			ui.Success("Removed %s", AbbreviatePath(binaryPath))
		}
	} else {
		ui.Info("Binary not found on PATH")
	}

	// 2. Remove installed dependencies (asciinema, agg, fzf from ~/.local/bin)
	installedDeps := InstalledDependencyPaths()
	if len(installedDeps) > 0 {
		ui.SubSection("Installed Dependencies")
		for _, depPath := range installedDeps {
			if err := removeFile(depPath, dryRun); err != nil {
				ui.Warning("Failed to remove %s: %v", AbbreviatePath(depPath), err)
				hasErrors = true
			} else {
				ui.Success("Removed %s", AbbreviatePath(depPath))
			}
		}
	}

	// 3. Optionally remove config/local state
	if !keepConfig {
		ui.SubSection("Configuration")
		configFiles := []string{
			paths.ConfigFile,
		}

		for _, cfgFile := range configFiles {
			if FileExists(cfgFile) {
				if err := removeFile(cfgFile, dryRun); err != nil {
					ui.Warning("Failed to remove %s: %v", AbbreviatePath(cfgFile), err)
					hasErrors = true
				} else {
					ui.Success("Removed %s", AbbreviatePath(cfgFile))
				}
			}
		}

		// Remove config directories if empty
		for _, dir := range []string{paths.ConfigDir, paths.DataDir, paths.StateDir} {
			if DirExists(dir) {
				if err := removeDirIfEmpty(dir, dryRun); err != nil {
					// Not an error - directory might not be empty
					fmt.Printf("  %s %s (not empty)\n", ui.Gray("-"), AbbreviatePath(dir))
				} else {
					ui.Success("Removed %s", AbbreviatePath(dir))
				}
			}
		}
	} else {
		ui.Info("Keeping config files (--keep-config)")
	}

	// 4. Optionally remove recordings
	if !keepRecordings {
		ui.SubSection("Recordings")
		if DirExists(paths.RecordingsDir) {
			// Count recordings
			var count int
			_ = filepath.WalkDir(paths.RecordingsDir, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(path, ".cast") {
					count++
				}
				return nil
			})

			if count > 0 {
				if !yes && !dryRun {
					result, _ := ui.Confirm(fmt.Sprintf("Remove %d recordings?", count), false)
					if !result {
						ui.Info("Keeping recordings")
						goto skipRecordings
					}
				}
			}

			if err := removeDir(paths.RecordingsDir, dryRun); err != nil {
				ui.Warning("Failed to remove recordings: %v", err)
				hasErrors = true
			} else {
				ui.Success("Removed %s (%d recordings)", AbbreviatePath(paths.RecordingsDir), count)
			}
		}
	} else {
		ui.Info("Keeping recordings (--keep-recordings)")
	}
skipRecordings:

	// Summary
	fmt.Println()
	switch {
	case dryRun:
		ui.Info("Dry run complete - no changes were made")
		ui.CommandEnd(ui.StatusNoop)
	case hasErrors:
		ui.Warning("Uninstall completed with warnings")
		ui.CommandEnd(ui.StatusWarning)
	default:
		ui.Success("nssh uninstalled successfully")
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}

// removeFile removes a single file.
func removeFile(path string, dryRun bool) error {
	if dryRun {
		return nil
	}
	return os.Remove(path)
}

// removeDir removes a directory and all its contents.
func removeDir(path string, dryRun bool) error {
	if dryRun {
		return nil
	}
	return os.RemoveAll(path)
}

// removeDirIfEmpty removes a directory only if it's empty.
func removeDirIfEmpty(path string, dryRun bool) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("directory not empty")
	}
	if dryRun {
		return nil
	}
	return os.Remove(path)
}
