package log

import (
	"context"
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewArchiveCmd creates the 'log archive' command.
func NewArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive",
		Short: "Archive old recordings",
		Long:  "Run one recording archive maintenance pass. Use cron, launchd, or systemd timers for automation.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArchive(cmd.Context())
		},
	}
}

func runArchive(ctx context.Context) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.Error("%s", err)
		return &exit.ExitError{Code: 1}
	}
	paths := config.DefaultPaths()
	settings := recording.LoadRecordingSettings()
	summary, err := recording.RunArchive(ctx, recording.ArchiveMaintenanceConfig{
		Archive:      cfg.Logging.Session.Archive,
		RecordingDir: settings.Directory,
		StateDir:     paths.StateDir,
	}, nil)
	if err != nil {
		ui.Error("%s", err)
		return &exit.ExitError{Code: 1}
	}

	if summary.SkippedReason != "" {
		fmt.Printf("archived files=0 bytes=0 pruned=0 skipped=%q\n", summary.SkippedReason)
		return nil
	}
	fmt.Printf("archived files=%d bytes=%d pruned=%d skipped=\"\"\n",
		summary.FilesArchived,
		summary.BytesArchived,
		summary.BundlesPruned)
	return nil
}
