package sync

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	intsync "github.com/ntwrknrd/nssh/internal/sync"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewGetCmd creates the sync get command.
func NewGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <source>",
		Short: "Show sync source details",
		Long:  "Show configuration, state, and managed host count for a sync source.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(args[0])
		},
	}
}

func runGet(sourceName string) error {
	ui.CommandStart("SYNC SOURCE: " + sourceName)

	cfg, err := config.LoadDefault()
	if err != nil {
		ui.Error("Failed to load config: %s", err)
		ui.CommandEnd(ui.StatusError)
		return err
	}

	// Find the source config
	var src *config.SyncSourceConfig
	for i := range cfg.Sync.Sources {
		if cfg.Sync.Sources[i].Name == sourceName {
			src = &cfg.Sync.Sources[i]
			break
		}
	}
	if src == nil {
		ui.Error("Source %q not found in config", sourceName)
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("source %q not found", sourceName)
	}

	panel := ui.NewPanel("Configuration")
	panel.Row("Provider", src.Provider)
	panel.Row("Routes", fmt.Sprintf("%d", len(src.Routes)))

	switch src.Provider {
	case "containerlab":
		if src.Containerlab != nil {
			panel.Row("Jump Host", src.Containerlab.JumpHost)
			panel.Row("Sudo", fmt.Sprintf("%v", src.Containerlab.Sudo))
		}
	case "netbox":
		if src.NetBox != nil {
			panel.Row("Base URL", src.NetBox.BaseURL)
			panel.Row("Token Env", src.NetBox.TokenEnv)
		}
	}
	panel.Print()

	// Show routes
	fmt.Println()
	ui.SubSection("Routes")
	for _, r := range src.Routes {
		name := r.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("    %s -> context=%s", name, r.Context)
		if r.IncludeFile != "" {
			fmt.Printf(" include=%s", r.IncludeFile)
		}
		fmt.Println()
		if len(r.Match) > 0 {
			for field, values := range r.Match {
				fmt.Printf("      %s: %v\n", field, values)
			}
		}
	}

	// Show state
	state, err := intsync.LoadSourceState(sourceName)
	if err != nil {
		ui.Warning("Failed to load state: %s", err)
	} else if state != nil {
		fmt.Println()
		ui.SubSection("State")
		statePanel := ui.NewPanel("")
		statePanel.Row("Last Sync", state.LastSync.Format("2006-01-02 15:04:05"))
		statePanel.Row("Managed Hosts", fmt.Sprintf("%d", len(state.Objects)))
		statePanel.Print()
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
