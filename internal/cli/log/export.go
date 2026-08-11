package log

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/recording"
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
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.Error("%s", err)
		return &exit.ExitError{Code: 1}
	}
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

	// Generate default destination
	defaultDest := DefaultExportDestination(session.CastPath, "txt")

	var destination string
	if yes {
		destination = defaultDest
	} else {
		result, err := ui.InputWithDefault("Output path (.txt or .gif)", defaultDest)
		if err != nil {
			ui.Abort("%s", err)
			return err
		}
		destination = result
	}

	// Resolve format from extension
	format, err := resolveExportFormat(destination)
	if err != nil {
		ui.Error("%s", err)
		return &exit.ExitError{Code: 1}
	}

	ui.Info("Format: %s", format)

	var cmd []string
	var useProgress bool
	if format == "gif" {
		toolPath, err := findGifConverter()
		if err != nil {
			ui.Error("%s", err)
			return &exit.ExitError{Code: 1}
		}
		cmd, err = buildGIFExportCommand(toolPath, session.CastPath, destination, cfg.Logging.Export.GIF)
		if err != nil {
			ui.Error("%s", err)
			return &exit.ExitError{Code: 1}
		}
		useProgress = true
	} else {
		asciinemaPath, err := RequireBinary("asciinema")
		if err != nil {
			ui.Error("%s", err)
			return &exit.ExitError{Code: 1}
		}
		cmd = buildTextExportCommand(asciinemaPath, session.CastPath, destination)
	}

	var runErr error
	if useProgress {
		runErr = RunCommandWithProgress(cmd, "Exporting", dryRun)
	} else {
		runErr = RunCommand(cmd, dryRun)
	}
	if runErr != nil {
		ui.Error("%s", runErr)
		return &exit.ExitError{Code: 1}
	}

	if dryRun {
		ui.Warning("Run without --dry-run to actually export")
	} else {
		ui.Success("Exported to %s", destination)
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

// findGifConverter looks for agg in PATH.
func findGifConverter() (string, error) {
	if path, err := exec.LookPath("agg"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("GIF converter not found. Install 'agg' (brew install agg)")
}

func buildGIFExportCommand(toolPath, castPath, destination string, settings config.GIFExportConfig) ([]string, error) {
	cmd := []string{toolPath}
	if settings.WindowSize != "" {
		cols, rows, err := parseGIFWindowSize(settings.WindowSize)
		if err != nil {
			return nil, err
		}
		cmd = append(cmd, "--cols", strconv.Itoa(cols), "--rows", strconv.Itoa(rows))
	}
	if settings.FontSize > 0 {
		cmd = append(cmd, "--font-size", strconv.Itoa(settings.FontSize))
	}
	cmd = append(cmd, castPath, destination)
	return cmd, nil
}

func buildTextExportCommand(asciinemaPath, castPath, destination string) []string {
	return []string{asciinemaPath, "convert", "--overwrite", castPath, destination}
}

func parseGIFWindowSize(value string) (int, int, error) {
	colsText, rowsText, ok := strings.Cut(strings.TrimSpace(value), "x")
	if !ok || strings.TrimSpace(colsText) == "" || strings.TrimSpace(rowsText) == "" {
		return 0, 0, fmt.Errorf("logging.export.gif.window_size must be COLSxROWS, got %q", value)
	}
	cols, err := strconv.Atoi(strings.TrimSpace(colsText))
	if err != nil || cols <= 0 {
		return 0, 0, fmt.Errorf("logging.export.gif.window_size has invalid columns %q", colsText)
	}
	rows, err := strconv.Atoi(strings.TrimSpace(rowsText))
	if err != nil || rows <= 0 {
		return 0, 0, fmt.Errorf("logging.export.gif.window_size has invalid rows %q", rowsText)
	}
	return cols, rows, nil
}
