package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// Options configures the root application runtime.
type Options struct {
	Version string
	Commit  string
	Date    string
	Args    []string
}

// Run executes nssh and returns the process exit code.
func Run(opts Options) int {
	args := opts.Args
	if args == nil {
		args = os.Args[1:]
	}

	if len(args) >= 1 && args[0] == "__agent" {
		runAgentDaemon()
		return exit.ExitSuccess
	}

	if err := execute(opts, args); err != nil {
		if ui.IsExplainShown(err) {
			return exit.ExitSuccess
		}

		var notFound *connect.HostNotFoundError
		if errors.As(err, &notFound) {
			if spawnErr := spawnHostAdd(notFound.Hostname); spawnErr != nil {
				fmt.Fprintln(os.Stderr, "nssh:", spawnErr)
				return exit.ExitGeneralError
			}
			return exit.ExitSuccess
		}

		var exitErr *exit.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, "nssh:", exitErr.Message)
			}
			return exitErr.Code
		}
		fmt.Fprintln(os.Stderr, "nssh:", err)
		return exit.ExitGeneralError
	}
	return exit.ExitSuccess
}

func execute(opts Options, args []string) error {
	if len(args) > 0 && completionEntrypoints[args[0]] {
		return fmt.Errorf("shell completion is not supported")
	}

	rootCmd := NewRootCmd(opts)
	rootCmd.SetArgs(PreprocessArgs(args))
	return rootCmd.Execute()
}

func spawnHostAdd(hostname string) error {
	fmt.Printf("Host '%s' not found. Adding new host...\n", hostname)

	cmd := exec.Command(os.Args[0], "inv", "set", hostname)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func initLogging(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}
