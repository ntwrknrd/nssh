package log

import (
	"os"
	"os/exec"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewAuthCmd creates the 'log auth' command.
func NewAuthCmd() *cobra.Command {
	var (
		serverURL string
		quiet     bool
	)

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with asciinema",
		Long:  "Link this CLI with an asciinema server account for uploading recordings.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuth(serverURL, quiet)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server-url", "", "asciinema server URL (uses config default if not specified)")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress diagnostic messages")

	return cmd
}

func runAuth(serverURL string, quiet bool) error {
	settings := recording.LoadRecordingSettings()

	if !quiet {
		ui.CommandStart("AUTHENTICATE WITH ASCIINEMA")
	}

	// Determine server URL (flag > env > config > default)
	if serverURL == "" {
		serverURL = os.Getenv("NSSH_ASCIINEMA_SERVER_URL")
	}
	if serverURL == "" {
		serverURL = settings.AsciinemaServer
	}
	if serverURL == "" {
		serverURL = defaultAsciinemaServer
	}

	asciinemaPath, err := RequireBinary("asciinema")
	if err != nil {
		if !quiet {
			ui.Error("%s", err)
			ui.CommandEnd(ui.StatusError)
		}
		return &exit.ExitError{Code: 1}
	}

	if !quiet {
		ui.Info("Server: %s", serverURL)
	}

	// Build command args
	cmdArgs := []string{"auth", "--server-url", serverURL}
	if quiet {
		cmdArgs = append(cmdArgs, "--quiet")
	}

	// Run asciinema auth interactively
	cmd := exec.Command(asciinemaPath, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if !quiet {
			ui.Error("Authentication failed: %s", err)
			ui.CommandEnd(ui.StatusError)
		}
		return &exit.ExitError{Code: 1}
	}

	if !quiet {
		ui.CommandEnd(ui.StatusSuccess)
	}
	return nil
}
