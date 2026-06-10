package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ntwrknrd/nssh/internal/repl"
)

func Run(ctx context.Context, opts repl.Options) error {
	opts = normalizeOptions(opts)
	programOptions := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	if opts.In != nil {
		programOptions = append(programOptions, tea.WithInput(opts.In))
	}
	if opts.Out != nil {
		programOptions = append(programOptions, tea.WithOutput(opts.Out))
	}
	p := tea.NewProgram(newModel(ctx, opts), programOptions...)
	_, err := p.Run()
	return err
}
