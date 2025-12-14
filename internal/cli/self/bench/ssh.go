package bench

import (
	"fmt"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewSSHCmd creates the ssh benchmark subcommand.
func NewSSHCmd() *cobra.Command {
	var (
		warmups    int
		samples    int
		simpleOnly bool
	)

	cmd := &cobra.Command{
		Use:   "ssh <host>",
		Short: "Benchmark SSH connections",
		Long: `Measure SSH connection timing overhead.

This command connects to the specified host multiple times and measures:
- PTY allocation time
- Connection establishment time
- Password prompt detection time
- Password injection time
- Total connection time

Use --simple for wall-clock only measurements (faster).

Examples:
  nssh self bench ssh router1
  nssh self bench ssh router1 --samples 10 --warmups 2
  nssh self bench ssh router1 --simple`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSSHBenchmark(args[0], warmups, samples, simpleOnly)
		},
	}

	cmd.Flags().IntVar(&warmups, "warmups", 1, "number of warmup runs (not measured)")
	cmd.Flags().IntVar(&samples, "samples", 3, "number of measured runs")
	cmd.Flags().BoolVar(&simpleOnly, "simple", false, "wall-clock timing only (no stage breakdown)")

	return cmd
}

func runSSHBenchmark(host string, warmups, samples int, simpleOnly bool) error {
	if samples < 1 {
		return fmt.Errorf("--samples must be at least 1")
	}
	if warmups < 0 {
		return fmt.Errorf("--warmups must be >= 0")
	}

	// Unlock vault before running benchmark (subprocess won't have TTY)
	if mgr, err := clisession.NewManager(vault.Auto()); err == nil {
		_ = clisession.TryUnlockIfTTY(mgr)
	}

	ui.CommandStart("SSH BENCHMARK")

	fmt.Printf("  %s: %s\n", ui.Gray("Host"), ui.Cyan(host))
	fmt.Printf("  %s: %d samples, %d warmup\n", ui.Gray("Config"), samples, warmups)
	fmt.Println()

	// Build command args: nssh <host> -- echo nssh-benchmark-test
	// The echo command runs on the remote and exits, giving us a quick roundtrip test
	cmdArgs := []string{host, "--", "echo", "nssh-benchmark-test"}

	result, err := run(cmdArgs, warmups, samples, simpleOnly)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("benchmark failed: %w", err)
	}

	renderResults(result, simpleOnly)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
