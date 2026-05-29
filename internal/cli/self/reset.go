package self

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// Exit codes for reset command.
const (
	exitResetSuccess   = 0
	exitResetError     = 1
	exitResetCancelled = 2
)

// ResetSummary holds statistics about what would be deleted.
type ResetSummary struct {
	ConfigDir      string
	DataDir        string
	StateDir       string
	HostCount      int
	ContextCount   int
	RecordingCount int
	TotalBytes     int64
}

// NewResetCmd creates the reset subcommand.
func NewResetCmd() *cobra.Command {
	var (
		dryRun bool
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration",
		Long: `Reset nssh by deleting all configuration and credentials.

This command permanently deletes:
- nssh provider configuration and local state
- Session recordings
- nssh configuration

Shell integration in your rc file will remain but become a harmless no-op.
SSH config (~/.ssh/) will not be modified.

Use --dry-run to preview what would be deleted.
Use --force to skip the confirmation prompt (for scripts).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(dryRun, force)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview what would be deleted without making changes")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt (for scripts)")

	return cmd
}

func runReset(dryRun, force bool) error {
	paths := config.DefaultPaths()

	ui.CommandStart("RESET NSSH")

	if dryRun {
		ui.Info("Dry run mode - no changes will be made")
		fmt.Println()
	}

	// Enumerate what would be deleted
	summary := enumerateDeletions(paths)

	// Show deletion summary
	ui.SubSection("The following will be permanently deleted")
	ui.Deletion("nssh provider configuration and local state")
	if summary.RecordingCount > 0 {
		ui.Deletion("Session recordings (%d files)", summary.RecordingCount)
	} else {
		ui.Deletion("Session recordings")
	}
	ui.Deletion("nssh configuration")
	fmt.Println()

	// Show what's preserved
	shellInfo := DetectShell()
	ui.Info("Shell integration in %s will remain (harmless no-op)", AbbreviatePath(shellInfo.RCFile))
	ui.Info("SSH config (~/.ssh/) will not be modified")
	fmt.Println()

	// Show directories
	ui.SubSection("Directories")
	showDirStatus(summary.ConfigDir, "config")
	showDirStatus(summary.DataDir, "data")
	showDirStatus(summary.StateDir, "state")
	fmt.Println()

	// Confirmation (unless --force or --dry-run)
	if !force && !dryRun {
		response, err := ui.Input("Type DESTROY to confirm", "")
		if err != nil {
			// User canceled (Ctrl+C)
			ui.Info("Reset canceled")
			ui.CommandEnd(ui.StatusAbort)
			return &exit.ExitError{Code: exitResetCancelled}
		}

		if strings.TrimSpace(response) != "DESTROY" {
			ui.Info("Reset canceled (you must type DESTROY exactly)")
			ui.CommandEnd(ui.StatusAbort)
			return &exit.ExitError{Code: exitResetCancelled}
		}
		fmt.Println()
	}

	// Dry run stops here
	if dryRun {
		ui.Info("Dry run complete - no changes were made")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Execute deletion
	ui.SubSection("Deleting")

	// 1. Stop agent first (before deleting socket directory)
	if agent.IsRunning() {
		ui.Info("Stopping agent...")
		if client, err := agent.Connect(); err == nil {
			_ = client.Lock()
			_ = client.Close()
		}

		stopped := false
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !agent.IsRunning() {
				stopped = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		if stopped {
			ui.Success("Agent stopped")
		} else {
			ui.Warning("Agent did not stop promptly; continuing with reset")
		}
	}

	// 2. Delete directories in order: state, data, config
	hasErrors := false

	// State directory (socket, recordings, lockout)
	if DirExists(summary.StateDir) {
		if err := os.RemoveAll(summary.StateDir); err != nil {
			ui.Warning("Failed to remove %s: %v", AbbreviatePath(summary.StateDir), err)
			hasErrors = true
		} else {
			ui.Success("Removed %s", AbbreviatePath(summary.StateDir))
		}
	}

	// Data directory
	if DirExists(summary.DataDir) {
		if err := os.RemoveAll(summary.DataDir); err != nil {
			ui.Warning("Failed to remove %s: %v", AbbreviatePath(summary.DataDir), err)
			hasErrors = true
		} else {
			ui.Success("Removed %s", AbbreviatePath(summary.DataDir))
		}
	}

	// Config directory
	if DirExists(summary.ConfigDir) {
		if err := os.RemoveAll(summary.ConfigDir); err != nil {
			ui.Warning("Failed to remove %s: %v", AbbreviatePath(summary.ConfigDir), err)
			hasErrors = true
		} else {
			ui.Success("Removed %s", AbbreviatePath(summary.ConfigDir))
		}
	}

	// Final status
	fmt.Println()
	if hasErrors {
		ui.Warning("Reset completed with warnings")
		ui.CommandEnd(ui.StatusWarning)
		return &exit.ExitError{Code: exitResetError, Message: "reset completed with errors"}
	}

	ui.Success("nssh reset to initial state")
	ui.Info("Run 'nssh self init' to set up again")
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// enumerateDeletions gathers statistics about what would be deleted.
func enumerateDeletions(paths *config.Paths) ResetSummary {
	summary := ResetSummary{
		ConfigDir: paths.ConfigDir,
		DataDir:   paths.DataDir,
		StateDir:  paths.StateDir,
	}

	// Count recordings
	if DirExists(paths.RecordingsDir) {
		_ = filepath.WalkDir(paths.RecordingsDir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".cast") {
				summary.RecordingCount++
			}
			return nil
		})
	}

	// Calculate total bytes (best effort)
	for _, dir := range []string{summary.ConfigDir, summary.DataDir, summary.StateDir} {
		if DirExists(dir) {
			_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if info, err := d.Info(); err == nil {
						summary.TotalBytes += info.Size()
					}
				}
				return nil
			})
		}
	}

	return summary
}

// showDirStatus prints the status of a directory.
func showDirStatus(dir, label string) {
	if DirExists(dir) {
		ui.Deletion("%s: %s", label, AbbreviatePath(dir))
	} else {
		ui.Info("%s: %s (not found)", label, AbbreviatePath(dir))
	}
}
