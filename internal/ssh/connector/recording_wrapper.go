//go:build unix

package connector

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// MaybeWrapWithRecording checks if recording is enabled and wraps the connection.
// Returns true if recording was started (caller should exit), false to continue normally.
func MaybeWrapWithRecording(hostname string, args []string) (bool, error) {
	// Check if we're already inside a recording
	if os.Getenv("NSSH_RECORDING_INNER") == "1" {
		return false, nil
	}

	settings := recording.LoadRecordingSettings()
	plan, err := recording.BuildRecordingPlan(hostname, settings)
	if err != nil {
		return false, err
	}

	if !plan.Enabled {
		if plan.Warn {
			ui.Warning("Recording skipped: %s", plan.Reason)
		} else {
			slog.Debug("recording disabled", "reason", plan.Reason)
		}
		return false, nil
	}

	// Acquire session lock
	lock, err := recording.AcquireSessionLock(plan.LockDirectory)
	if err != nil {
		return false, fmt.Errorf("failed to acquire session lock: %w", err)
	}
	defer lock.Release()

	// Get current executable
	exe, err := os.Executable()
	if err != nil {
		exe = "nssh"
	}

	// Build command: nssh <hostname> [args...]
	innerCmd := []string{exe, hostname}
	innerCmd = append(innerCmd, args...)

	// Build asciinema command
	asciinemaCmd := recording.BuildAsciinemaCommand(plan, innerCmd)

	// Execute with NSSH_RECORDING_INNER set
	cmd := exec.Command(asciinemaCmd[0], asciinemaCmd[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "NSSH_RECORDING_INNER=1")

	err = cmd.Run()

	// Export to text if enabled (do this before handling exit errors)
	if settings.AutoExportTxt && plan.CastPath != "" {
		if exportErr := recording.ExportToText(plan.CastPath); exportErr != nil {
			slog.Debug("failed to export recording to text", "err", exportErr)
		}
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			lock.Release()
			//nolint:gocritic // os.Exit is intentional here; caller expects wrapper to handle exit code.
			os.Exit(exitErr.ExitCode())
		}
		return true, err
	}

	return true, nil
}
