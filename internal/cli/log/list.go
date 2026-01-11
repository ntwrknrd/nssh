package log

import (
	"regexp"
	"time"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewListCmd creates the 'log list' command.
func NewListCmd() *cobra.Command {
	var selectPattern string
	var lastN int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List recordings",
		Long:    "List recorded SSH sessions with timestamps and host information.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(selectPattern, lastN)
		},
	}

	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "Filter by pattern (today, yesterday, this-week, this-month, or regex)")
	cmd.Flags().IntVarP(&lastN, "last", "l", 0, "Filter on the last N entries")

	return cmd
}

func runList(selectPattern string, lastN int) error {
	settings := recording.LoadRecordingSettings()
	localTZ := time.Now().Location()

	ui.CommandStart("SESSION RECORDINGS")

	// Use lazy loading optimization when --last is specified without filter
	// This avoids loading all session metadata when only a few are needed
	var sessions []recording.SessionRecord
	if lastN > 0 && selectPattern == "" {
		sessions = LoadSessionsLimit(settings, lastN)
	} else {
		sessions = LoadSessions(settings)
	}

	if selectPattern != "" {
		selectPattern = ExpandDateShortcut(selectPattern)
		pattern, err := regexp.Compile("(?i)" + selectPattern)
		if err != nil {
			ui.Error("Invalid regex pattern: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		var filtered []recording.SessionRecord
		for _, s := range sessions {
			startDate := s.StartedAt.In(localTZ).Format("2006-01-02")
			mtimeDate := sessionUpdatedTimestamp(s).In(localTZ).Format("2006-01-02")
			if MatchesPattern(pattern, s.Host, s.SessionLabel, startDate, mtimeDate) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered

		if len(sessions) == 0 {
			ui.WarningCentered("No sessions matching pattern: %s", selectPattern)
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}

		// Apply --last limit after filtering (sessions are already sorted newest-first)
		if lastN > 0 && lastN < len(sessions) {
			sessions = sessions[:lastN]
		}
	}

	PrintSessions(sessions, selectPattern)

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
