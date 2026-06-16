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
		edit      bool
		pathsOnly bool
		source    bool
	)

	cmd := &cobra.Command{
		Use:   "cfg",
		Short: "Manage configuration",
		Long: `View or edit the nssh configuration file.

By default, prints the effective configuration (merged from file,
environment variables, and defaults) in YAML format.

Use --source to print the resolved source files with comments preserved.
Use --edit to open the config file in your editor ($VISUAL or $EDITOR).
Use --paths to print config file paths, including resolved includes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCfg(edit, pathsOnly, source)
		},
	}

	cmd.Flags().BoolVar(&edit, "edit", false, "open config in editor")
	cmd.Flags().BoolVar(&pathsOnly, "paths", false, "print config file paths only")
	cmd.Flags().BoolVar(&source, "source", false, "print source config files with comments")

	return cmd
}

func runCfg(edit, pathsOnly, source bool) error {
	paths := config.DefaultPaths()

	if pathsOnly {
		return printConfigPaths(paths)
	}
	if source {
		return printSourceConfig(paths)
	}

	// --edit: open in editor
	if edit {
		return openInEditor(paths.ConfigFile)
	}

	// Default: print effective config
	return printEffectiveConfig(paths)
}

func printSourceConfig(paths *config.Paths) error {
	files, err := configFiles(paths)
	if err != nil {
		return err
	}
	color := term.IsTerminal(int(os.Stdout.Fd()))
	for i, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read config %s: %w", file, err)
		}
		if len(files) > 1 {
			if i > 0 {
				fmt.Println()
			}
			header := "# " + file + "\n"
			fmt.Print(renderConfigText(header, color))
		}
		text := string(data)
		fmt.Print(renderConfigText(text, color))
		if text == "" || text[len(text)-1] != '\n' {
			fmt.Println()
		}
	}
	return nil
}

func printConfigPaths(paths *config.Paths) error {
	files, err := configFiles(paths)
	if err != nil {
		return err
	}
	for _, file := range files {
		fmt.Println(file)
	}
	return nil
}

func configFiles(paths *config.Paths) ([]string, error) {
	if paths == nil {
		paths = config.DefaultPaths()
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	files := cfg.ConfigFiles()
	if len(files) == 0 {
		files = []string{paths.ConfigFile}
	}
	return files, nil
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

	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}

	text, err := config.MarshalSparse(cfg)
	if err != nil {
		return err
	}
	fmt.Print(renderConfigText(text, term.IsTerminal(int(os.Stdout.Fd()))))
	return nil
}

func renderConfigText(text string, color bool) string {
	if !color {
		return text
	}
	return ui.HighlightYAML(text)
}
