package self

import (
	"bufio"
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
		Long: `Remove nssh files and shell integration.

This command removes:
- Shell integration snippets from your shell's rc file
- Shell integration scripts
- Completion files
- The nssh binary (from ~/.local/bin)
- Installed dependencies (asciinema, agg, fzf from ~/.local/bin)

Optionally removes (unless --keep-config or --keep-recordings):
- Config file and credentials
- Session recordings

Use --dry-run to preview what would be removed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(keepConfig, keepRecordings, dryRun, yes)
		},
	}

	cmd.Flags().BoolVar(&keepConfig, "keep-config", false, "keep config and credentials")
	cmd.Flags().BoolVar(&keepRecordings, "keep-recordings", false, "keep session recordings")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompts")

	return cmd
}

func runUninstall(keepConfig, keepRecordings, dryRun, yes bool) error {
	paths := config.DefaultPaths()
	home := homeDir()

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

	// 1. Remove shell integration from rc file
	ui.SubSection("Shell Integration")
	shellInfo := DetectShell()

	if err := removeShellSnippet(shellInfo.RCFile, dryRun); err != nil {
		ui.Warning("Failed to remove shell snippet: %v", err)
		hasErrors = true
	} else {
		ui.Success("Removed shell snippet from %s", AbbreviatePath(shellInfo.RCFile))
	}

	// 2. Remove shell integration scripts
	shellScripts := []string{
		filepath.Join(paths.DataDir, "nssh-shell-integration.sh"),
		filepath.Join(paths.DataDir, "nssh-shell-integration.fish"),
	}

	for _, script := range shellScripts {
		if FileExists(script) {
			if err := removeFile(script, dryRun); err != nil {
				ui.Warning("Failed to remove %s: %v", AbbreviatePath(script), err)
				hasErrors = true
			} else {
				ui.Success("Removed %s", AbbreviatePath(script))
			}
		}
	}

	// 3. Remove completions
	ui.SubSection("Completions")
	completionFiles := []string{
		filepath.Join(home, ".config", "fish", "completions", "nssh.fish"),
		filepath.Join(home, ".zsh", "completions", "_nssh"),
		filepath.Join(home, ".bash_completion.d", "nssh"),
	}

	for _, compFile := range completionFiles {
		if FileExists(compFile) {
			if err := removeFile(compFile, dryRun); err != nil {
				ui.Warning("Failed to remove %s: %v", AbbreviatePath(compFile), err)
				hasErrors = true
			} else {
				ui.Success("Removed %s", AbbreviatePath(compFile))
			}
		}
	}

	// 4. Remove binary
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

	// 5. Remove installed dependencies (asciinema, agg, fzf from ~/.local/bin)
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

	// 6. Optionally remove config/credentials
	if !keepConfig {
		ui.SubSection("Configuration")
		configFiles := []string{
			paths.ConfigFile,
			paths.CredentialsFile,
			paths.AgeKeyFile,
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

	// 7. Optionally remove recordings
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

// removeShellSnippet removes the nssh shell integration block from an rc file.
func removeShellSnippet(rcFile string, dryRun bool) error {
	if !FileExists(rcFile) {
		return nil // Nothing to do
	}

	f, err := os.Open(rcFile)
	if err != nil {
		return err
	}

	var lines []string
	var modified bool
	skip := false
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()

		// Start skipping when we see the marker
		if strings.Contains(line, ShellIntegrationMarker) {
			skip = true
			modified = true
			continue
		}

		// Stop skipping after the if block ends (for bash/zsh)
		// The snippet format is:
		// # nssh shell integration
		// if [ -f "..." ]; then
		//     source "..."
		// fi
		if skip && strings.TrimSpace(line) == "fi" {
			skip = false
			continue
		}

		// For fish, the snippet is just:
		// # nssh shell integration
		// source ...
		if skip && strings.HasPrefix(strings.TrimSpace(line), "source ") && strings.Contains(line, "nssh") {
			skip = false
			continue
		}

		if !skip {
			lines = append(lines, line)
		}
	}
	_ = f.Close()

	if err := scanner.Err(); err != nil {
		return err
	}

	if !modified {
		return nil // Marker not found, nothing to do
	}

	if dryRun {
		return nil
	}

	// Write back
	return os.WriteFile(rcFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
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
