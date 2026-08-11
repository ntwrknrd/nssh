package log

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewPlayCmd creates the 'log play' command.
func NewPlayCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "play",
		Short: "Play recording",
		Long:  "Play back a recorded SSH session in the terminal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlay(dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview actions without executing")

	return cmd
}

func runPlay(dryRun bool) error {
	settings := recording.LoadRecordingSettings()
	sessions := recording.IterSessionRecords(settings)

	if len(sessions) == 0 {
		ui.Warning("No recordings found in %s", settings.Directory)
		return nil
	}

	session, err := SelectSession(sessions, "Select recording:")
	if err != nil {
		ui.Abort("%s", err)
		return nil
	}

	asciinemaPath, err := RequireBinary("asciinema")
	if err != nil {
		ui.Error("%s", err)
		return &exit.ExitError{Code: 1}
	}

	// Build command args
	cmdArgs := []string{"play"}

	// Add idle time limit if configured for playback
	mode := strings.ToLower(settings.IdleTimeLimitMode)
	if settings.IdleTimeLimit > 0 && (mode == "play" || mode == "both") {
		cmdArgs = append(cmdArgs, "--idle-time-limit", fmt.Sprintf("%.1f", settings.IdleTimeLimit))
	}

	cmdArgs = append(cmdArgs, session.CastPath)

	if dryRun {
		ui.Info("[dry-run] %s %s", asciinemaPath, strings.Join(cmdArgs, " "))
		ui.Warning("Run without --dry-run to actually play")
		return nil
	}

	// Run asciinema and capture output to detect interruption
	cmd := exec.Command(asciinemaPath, cmdArgs...)
	cmd.Stdin = os.Stdin

	// Capture stdout/stderr while also displaying them
	var outputBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &outputBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &outputBuf)

	err = cmd.Run()

	// Check if playback was interrupted
	if strings.Contains(outputBuf.String(), "interrupted") {
		return nil
	}

	if err != nil {
		ui.Error("%s", err)
		return &exit.ExitError{Code: 1}
	}

	return nil
}
