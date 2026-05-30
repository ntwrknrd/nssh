package self

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewCfgCmd creates the cfg subcommand.
func NewCfgCmd() *cobra.Command {
	var (
		edit     bool
		pathOnly bool
	)

	cmd := &cobra.Command{
		Use:   "cfg",
		Short: "Manage configuration",
		Long: `View or edit the nssh configuration file.

By default, prints the effective configuration (merged from file,
environment variables, and defaults) in TOML format.

Use --edit to open the config file in your editor ($VISUAL or $EDITOR).
Use --path to print just the config file path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCfg(edit, pathOnly)
		},
	}

	cmd.Flags().BoolVar(&edit, "edit", false, "open config in editor")
	cmd.Flags().BoolVar(&pathOnly, "path", false, "print config file path only")

	return cmd
}

func runCfg(edit, pathOnly bool) error {
	paths := config.DefaultPaths()

	// --path: just print path and exit
	if pathOnly {
		fmt.Println(paths.ConfigFile)
		return nil
	}

	// --edit: open in editor
	if edit {
		return openInEditor(paths.ConfigFile)
	}

	// Default: print effective config
	return printEffectiveConfig(paths)
}

func openInEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func printEffectiveConfig(paths *config.Paths) error {
	ui.CommandStart(paths.ConfigFile)

	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	text, err := config.MarshalSparse(cfg)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	fmt.Print(renderConfigText(text, term.IsTerminal(int(os.Stdout.Fd()))))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func renderConfigText(text string, color bool) string {
	if !color {
		return text
	}
	return ui.HighlightTOML(text)
}
