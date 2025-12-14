package log

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

const defaultAsciinemaServer = "https://asciinema.org"

// NewUploadCmd creates the 'log upload' command.
func NewUploadCmd() *cobra.Command {
	var (
		yes    bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload to asciinema",
		Long:  "Upload a recording to asciinema.org for sharing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpload(yes, dryRun)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview actions without executing")

	return cmd
}

func runUpload(yes, dryRun bool) error {
	settings := recording.LoadRecordingSettings()
	sessions := LoadSessions(settings)

	ui.CommandStart("UPLOAD RECORDING")

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

	// Determine server URL
	serverURL := os.Getenv("NSSH_ASCIINEMA_SERVER_URL")
	if serverURL == "" {
		serverURL = settings.AsciinemaServer
	}
	if serverURL == "" {
		serverURL = defaultAsciinemaServer
	}

	if !yes {
		result, err := ui.InputWithDefault("Server URL", serverURL)
		if err != nil {
			ui.Abort("%s", err)
			ui.CommandEnd(ui.StatusAbort)
			return err
		}
		serverURL = result
	}

	ui.Info("Server: %s", serverURL)

	asciinemaPath, err := RequireBinary("asciinema")
	if err != nil {
		ui.Error("%s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	if dryRun {
		ui.Info("[dry-run] %s upload --server-url %s %s", asciinemaPath, serverURL, session.CastPath)
		ui.Warning("Run without --dry-run to actually upload")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Run asciinema and capture output
	cmd := exec.Command(asciinemaPath, "upload", "--server-url", serverURL, session.CastPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Show stderr if available
		if stderr.Len() > 0 {
			ui.Error("%s", strings.TrimSpace(stderr.String()))
		} else {
			ui.Error("%s", err)
		}
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	// Extract URL from output
	output := stdout.String()
	url := extractURL(output)
	if url != "" {
		ui.Success("URL: %s", url)
	} else {
		// Fallback: show raw output if we can't parse it
		ui.Info("%s", strings.TrimSpace(output))
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// extractURL finds the first URL in the output
func extractURL(output string) string {
	re := regexp.MustCompile(`https?://[^\s]+`)
	match := re.FindString(output)
	return match
}
