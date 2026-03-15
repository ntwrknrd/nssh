package sync

import (
	"fmt"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	intsync "github.com/ntwrknrd/nssh/internal/sync"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewListCmd creates the sync list command.
func NewListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured sync sources",
		Long:  "List all configured sync sources with their provider and last sync time.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList()
		},
	}
}

func runList() error {
	ui.CommandStart("SYNC SOURCES")

	cfg, err := config.LoadDefault()
	if err != nil {
		ui.Error("Failed to load config: %s", err)
		ui.CommandEnd(ui.StatusError)
		return err
	}

	if len(cfg.Sync.Sources) == 0 {
		ui.Noop("No sync sources configured")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	table := ui.NewTable("Source", "Provider", "Routes", "Last Sync", "Hosts")

	for _, src := range cfg.Sync.Sources {
		lastSync := "-"
		hostCount := "-"

		state, err := intsync.LoadSourceState(src.Name)
		if err == nil && state != nil {
			if !state.LastSync.IsZero() {
				lastSync = state.LastSync.Format(time.RFC3339)
			}
			hostCount = fmt.Sprintf("%d", len(state.Objects))
		}

		table.AddRow(src.Name, src.Provider, fmt.Sprintf("%d", len(src.Routes)), lastSync, hostCount)
	}

	table.Render()

	margin := table.LeftMargin()
	ui.InfoWithMargin(margin, "%d source(s) configured", len(cfg.Sync.Sources))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
