package log

import (
	"fmt"
	"time"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the 'log delete' command.
func NewDeleteCmd() *cobra.Command {
	var (
		selectPattern string
		olderThan     int
		yes           bool
		dryRun        bool
	)

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete recordings",
		Long: `Delete recorded sessions.

Three mutually exclusive modes:
  1. --select PATTERN     : Delete recordings matching regex pattern
  2. --older-than DAYS    : Delete recordings older than N days
  3. Interactive (default): Select recordings via fzf (no mode flags)

Cannot combine modes - use only one mode flag at a time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(selectPattern, olderThan, yes, dryRun)
		},
	}

	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "Filter by pattern (today, yesterday, this-week, this-month, or regex)")
	cmd.Flags().IntVar(&olderThan, "older-than", 0, "Delete recordings older than N days")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview actions without executing")

	return cmd
}

func runDelete(selectPattern string, olderThan int, yes, dryRun bool) error {
	settings := recording.LoadRecordingSettings()

	ui.CommandStart("DELETE RECORDINGS")

	// Validate mutual exclusion
	modesSpecified := 0
	if olderThan > 0 {
		modesSpecified++
	}
	if selectPattern != "" {
		modesSpecified++
	}
	if modesSpecified > 1 {
		ui.Error("Cannot combine modes. Use one of: --select, --older-than, or interactive (no flags)")
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	// Mode 1: --older-than
	if olderThan > 0 {
		return deleteOlderThan(settings, olderThan, dryRun)
	}

	// Mode 2: --select pattern
	if selectPattern != "" {
		return deleteByPattern(settings, selectPattern, yes, dryRun)
	}

	// Mode 3: Interactive
	return deleteInteractive(settings, yes, dryRun)
}

func deleteOlderThan(settings recording.RecordingSettings, days int, dryRun bool) error {
	cutoff := time.Now().AddDate(0, 0, -days)

	sessions := LoadSessions(settings)
	var toDelete []recording.SessionRecord

	for _, session := range sessions {
		mtime := sessionUpdatedTimestamp(session)
		if mtime.Before(cutoff) {
			toDelete = append(toDelete, session)
		}
	}

	if len(toDelete) == 0 {
		ui.Success("No recordings to clean up")
		ui.CommandEnd(ui.StatusSuccess)
		return nil
	}

	ui.Info("Cutoff: %s", cutoff.Format("2006-01-02 15:04:05"))

	for _, session := range toDelete {
		if err := DeleteRecording(session.CastPath, settings.Directory, dryRun); err != nil {
			ui.Error("%s", err)
		}
	}

	ui.Info("Files removed: %d", len(toDelete))

	if dryRun {
		ui.Warning("Run without --dry-run to actually delete files")
		ui.CommandEnd(ui.StatusWarning)
	} else {
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}

func deleteByPattern(settings recording.RecordingSettings, pattern string, yes, dryRun bool) error {
	sessions := LoadSessions(settings)

	pattern = ExpandDateShortcut(pattern)
	filtered, err := FilterSessionsByPattern(sessions, pattern)
	if err != nil {
		ui.Error("Invalid pattern: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	if len(filtered) == 0 {
		ui.Warning("No recordings match '%s'", pattern)
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	ui.Info("Pattern: %s", pattern)
	ui.Info("Found %d recording(s)", len(filtered))

	if !yes && !dryRun {
		result, _ := ui.Confirm(fmt.Sprintf("Delete all %d?", len(filtered)), false)
		if !result {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	for _, session := range filtered {
		if err := DeleteRecording(session.CastPath, settings.Directory, dryRun); err != nil {
			ui.Error("%s", err)
		}
	}

	if dryRun {
		ui.Warning("Run without --dry-run to actually delete files")
		ui.CommandEnd(ui.StatusWarning)
	} else {
		ui.Success("Deleted %d recording(s)", len(filtered))
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}

func deleteInteractive(settings recording.RecordingSettings, yes, dryRun bool) error {
	sessions := LoadSessions(settings)

	if len(sessions) == 0 {
		ui.Warning("No recordings found in %s", settings.Directory)
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	selected, err := SelectSessionsMulti(sessions, "Select recording(s) [Tab=multi-select]:")
	if err != nil {
		ui.Abort("%s", err)
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	if !yes && !dryRun {
		var prompt string
		if len(selected) == 1 {
			prompt = fmt.Sprintf("Delete %s?", homeReplace(selected[0].CastPath))
		} else {
			prompt = fmt.Sprintf("Delete %d recording(s)?", len(selected))
		}

		result, _ := ui.Confirm(prompt, len(selected) == 1)
		if !result {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	for _, session := range selected {
		if err := DeleteRecording(session.CastPath, settings.Directory, dryRun); err != nil {
			ui.Error("%s", err)
		}
	}

	if dryRun {
		ui.Warning("Run without --dry-run to actually delete files")
		ui.CommandEnd(ui.StatusWarning)
	} else {
		ui.Success("Deleted %d recording(s)", len(selected))
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}
