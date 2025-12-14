package log

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewExportCmd creates the 'log export' command.
func NewExportCmd() *cobra.Command {
	var (
		yes    bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export recording",
		Long: `Export recording to text or GIF format.

Format is automatically inferred from the output file extension:
- .txt for text export
- .gif for animated GIF`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(yes, dryRun)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview actions without executing")

	return cmd
}

func runExport(yes, dryRun bool) error {
	settings := recording.LoadRecordingSettings()
	sessions := LoadSessions(settings)

	ui.CommandStart("EXPORT RECORDING")

	if len(sessions) == 0 {
		ui.Warning("No recordings found in %s", settings.Directory)
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	session, err := SelectSession(sessions, "Select recording:")
	if err != nil {
		ui.Abort("%s", err)
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	// Generate default destination
	defaultDest := DefaultExportDestination(session.CastPath, "txt")

	var destination string
	if yes {
		destination = defaultDest
	} else {
		result, err := ui.InputWithDefault("Output path (.txt or .gif)", defaultDest)
		if err != nil {
			ui.Abort("%s", err)
			ui.CommandEnd(ui.StatusAbort)
			return err
		}
		destination = result
	}

	// Resolve format from extension
	format, err := resolveExportFormat(destination)
	if err != nil {
		ui.Error("%s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	ui.Info("Format: %s", format)

	var cmd []string
	var useProgress bool
	if format == "gif" {
		toolPath, err := findGifConverter()
		if err != nil {
			ui.Error("%s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}
		cmd = []string{toolPath, session.CastPath, destination}
		useProgress = true
	} else {
		asciinemaPath, err := RequireBinary("asciinema")
		if err != nil {
			ui.Error("%s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}
		cmd = []string{asciinemaPath, "convert", "--overwrite", session.CastPath, destination}
	}

	var runErr error
	if useProgress {
		runErr = RunCommandWithProgress(cmd, "Exporting", dryRun)
	} else {
		runErr = RunCommand(cmd, dryRun)
	}
	if runErr != nil {
		ui.Error("%s", runErr)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	if dryRun {
		ui.Warning("Run without --dry-run to actually export")
		ui.CommandEnd(ui.StatusWarning)
	} else {
		ui.Success("Exported to %s", destination)
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}

func resolveExportFormat(destination string) (string, error) {
	ext := strings.ToLower(filepath.Ext(destination))

	switch ext {
	case ".gif":
		return "gif", nil
	case ".txt", "":
		return "txt", nil
	default:
		return "", fmt.Errorf("unsupported extension '%s'. Use .txt or .gif", ext)
	}
}

// findGifConverter looks for agg (brew) or asciicast2gif (npm) in PATH.
func findGifConverter() (string, error) {
	// Try agg first (modern Rust-based converter, available via brew)
	if path, err := exec.LookPath("agg"); err == nil {
		return path, nil
	}
	// Fall back to asciicast2gif (original, available via npm)
	if path, err := exec.LookPath("asciicast2gif"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("GIF converter not found. Install 'agg' (brew install agg) or 'asciicast2gif' (npm install -g asciicast2gif)")
}
