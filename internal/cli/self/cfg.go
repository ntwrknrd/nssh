package self

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/BurntSushi/toml"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/spf13/cobra"
)

// NewCfgCmd creates the cfg subcommand.
func NewCfgCmd() *cobra.Command {
	var (
		edit     bool
		pathOnly bool
	)

	cmd := &cobra.Command{
		Use:   "cfg",
		Short: "View or edit nssh configuration",
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
	// Print header with metadata
	exists := "exists"
	if _, err := os.Stat(paths.ConfigFile); os.IsNotExist(err) {
		exists = "not found, using defaults"
	}
	fmt.Printf("# Config: %s (%s)\n", paths.ConfigFile, exists)
	fmt.Printf("# Effective configuration\n\n")

	// Load and marshal config
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}

	// Encode as TOML to stdout
	encoder := toml.NewEncoder(os.Stdout)
	return encoder.Encode(cfg)
}
