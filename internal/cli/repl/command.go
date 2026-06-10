package repl

import (
	"fmt"
	"os"

	replcore "github.com/ntwrknrd/nssh/internal/repl"
	replbroker "github.com/ntwrknrd/nssh/internal/repl/broker"
	repltui "github.com/ntwrknrd/nssh/internal/repl/tui"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type replOptions struct {
	concurrency int
	plain       bool
	brokerJSON  bool
	diff        bool
	cursor      string
}

func NewCmd() *cobra.Command {
	opts := replOptions{
		concurrency: replcore.DefaultConcurrency,
		cursor:      string(replcore.PromptCursorPipe),
	}
	cmd := &cobra.Command{
		Use:   "repl",
		Short: "Run commands across SSH targets",
		Long: `Run network commands from an interactive prompt.

Input uses a quoted target group followed by a quoted command group:
  [ 'target' ] ( 'command' )

Targets may be a host, an IPv6 literal, a compact host expansion, or an explicit
inventory selector. Multiple targets or commands are comma-separated inside
their groups.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cursor, err := parsePromptCursor(opts.cursor)
			if err != nil {
				return err
			}
			coreOpts := replcore.Options{Concurrency: opts.concurrency, In: os.Stdin, Out: os.Stdout, Diff: opts.diff, PromptCursor: cursor}
			if opts.plain || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return replcore.RunPlain(cmd.Context(), coreOpts)
			}
			return repltui.Run(cmd.Context(), coreOpts)
		},
	}
	cmd.Flags().IntVar(&opts.concurrency, "concurrency", replcore.DefaultConcurrency, "maximum concurrent SSH commands")
	cmd.Flags().BoolVar(&opts.plain, "plain", false, "use plain line-oriented REPL")
	cmd.Flags().BoolVar(&opts.diff, "diff", false, "highlight split output differences")
	cmd.Flags().StringVar(&opts.cursor, "cursor", opts.cursor, "prompt cursor style: pipe or underscore")
	cmd.AddCommand(newBrokerCmd(&opts))
	ui.ApplyStyledHelp(cmd)
	return cmd
}

func parsePromptCursor(value string) (replcore.PromptCursor, error) {
	switch replcore.PromptCursor(value) {
	case "", replcore.PromptCursorPipe:
		return replcore.PromptCursorPipe, nil
	case replcore.PromptCursorUnderscore:
		return replcore.PromptCursorUnderscore, nil
	default:
		return "", fmt.Errorf("invalid cursor %q: expected pipe or underscore", value)
	}
}

func newBrokerCmd(opts *replOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "broker",
		Short:  "Run the REPL adapter broker",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.brokerJSON {
				return fmt.Errorf("broker requires --json")
			}
			return replbroker.Run(cmd.Context(), replcore.Options{
				Concurrency: opts.concurrency,
				In:          os.Stdin,
				Out:         os.Stdout,
			})
		},
	}
	cmd.Flags().BoolVar(&opts.brokerJSON, "json", false, "use newline-delimited JSON protocol")
	return cmd
}
