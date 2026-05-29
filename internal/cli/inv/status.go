package inv

import (
	"context"
	"fmt"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	invproviders "github.com/ntwrknrd/nssh/internal/inventory/providers"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var liveCheck bool
	cmd := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show inventory provider status",
		Long:  "Show read-only inventory provider config, freshness, and last-error status.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := ""
			if len(args) > 0 {
				provider = args[0]
			}
			return runStatus(provider, liveCheck)
		},
	}
	cmd.Flags().BoolVar(&liveCheck, "check", false, "perform a live health check without writing state")
	return cmd
}

func runStatus(providerName string, liveCheck bool) error {
	ui.CommandStart("INVENTORY PROVIDERS")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	table := ui.NewTable("Provider", "Type", "Refresh", "Routes", "Health")
	count := 0
	for name, provider := range cfg.Inventory.Provider {
		if providerName != "" && name != providerName {
			continue
		}
		health := "not checked"
		if liveCheck {
			health = liveProviderCheck(name, provider)
		}
		refresh := "-"
		if d := provider.RefreshInterval.Duration(); d > 0 {
			refresh = d.Round(time.Second).String()
		}
		table.AddRow(name, provider.Type, refresh, fmt.Sprintf("%d", len(provider.Route)), health)
		count++
	}
	if providerName != "" && count == 0 {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("provider %q not found", providerName)
	}
	table.Render()
	ui.InfoWithMargin(table.LeftMargin(), "Total: %d providers", count)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func liveProviderCheck(name string, providerCfg config.InventoryProviderConfig) string {
	provider, err := invproviders.New(providerCfg.Type)
	if err != nil {
		return "error: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	objects, err := provider.Discover(ctx, name, providerCfg, newConfigOnlyRunner(sshconfig.NewParser()))
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("ok (%d objects)", len(objects))
}
