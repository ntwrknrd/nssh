package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
)

func RunPlain(ctx context.Context, opts Options) error {
	opts = normalizeOptions(opts)
	scanner := bufio.NewScanner(opts.In)
	for {
		if _, err := fmt.Fprint(opts.Out, "nssh> "); err != nil {
			return err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(opts.Out)
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case ":quit", ":exit":
			return nil
		case ":help":
			PrintHelp(opts.Out)
			continue
		}
		req, err := ParseLine(line)
		if err != nil {
			PrintError(opts.Out, err)
			continue
		}
		targets, err := ResolveTargets(req, opts.Resolver)
		if err != nil {
			PrintError(opts.Out, err)
			continue
		}
		var results []CommandResult
		for _, command := range req.Commands {
			results = append(results, ExecuteFanout(ctx, targets, command, opts.Concurrency, opts.Runner)...)
		}
		RenderResults(opts.Out, results)
	}
}

func normalizeOptions(opts Options) Options {
	if opts.Concurrency <= 0 {
		opts.Concurrency = DefaultConcurrency
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Resolver == nil {
		opts.Resolver = DefaultTargetResolver{}
	}
	if opts.Runner == nil {
		opts.Runner = SSHCommandRunner{}
	}
	return opts
}

func PrintHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage:")
	_, _ = fmt.Fprintln(out, "  [ 'host' ] ( 'command...' )")
	_, _ = fmt.Fprintln(out, "  [ '2001:db8::10' ] ( 'command...' )")
	_, _ = fmt.Fprintln(out, "  [ 'prefix(1,2,3)' ] ( 'command...' )")
	_, _ = fmt.Fprintln(out, "  [ 'select:group:local/lab' ] ( 'command...' )")
	_, _ = fmt.Fprintln(out, "  [ 'host1', 'host2' ] ( 'show version', 'show clock' )")
	_, _ = fmt.Fprintln(out, "  :quit | :exit")
}

func PrintError(out io.Writer, err error) {
	_, _ = fmt.Fprintf(out, "error: %v\n", err)
}

func RenderResults(out io.Writer, results []CommandResult) {
	for i, result := range results {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintln(out, RenderOutputBanner(result.Host, result.Command))
		if len(result.Output) > 0 {
			_, _ = out.Write(result.Output)
			if result.Output[len(result.Output)-1] != '\n' {
				_, _ = fmt.Fprintln(out)
			}
		}
		if result.Err != nil {
			var exitErr *exit.ExitError
			if errors.As(result.Err, &exitErr) {
				_, _ = fmt.Fprintf(out, "error: %s (exit %d)\n", exitErr.Message, exitErr.Code)
			} else {
				_, _ = fmt.Fprintf(out, "error: %v\n", result.Err)
			}
		}
	}
}
