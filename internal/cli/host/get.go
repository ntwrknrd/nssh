package host

import (
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewGetCmd creates the host get command.
func NewGetCmd() *cobra.Command {
	var showSecret bool

	cmd := &cobra.Command{
		Use:   "get HOST",
		Short: "Show host details",
		Long:  "Display detailed information about an SSH host configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(args[0], showSecret)
		},
	}

	cmd.Flags().BoolVarP(&showSecret, "show-secret", "s", false, "reveal password in plain text (prints to terminal)")

	return cmd
}

func runGet(query string, showSecret bool) error {
	parser := getParser()

	ui.CommandStart("HOST DETAILS")

	// Find the host using fuzzy matching
	result, err := parser.MatchHost(query)
	if err != nil {
		ui.Error("Failed to search for host: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	var resolvedName string
	switch {
	case result.Host != nil:
		resolvedName = result.Host.Host
	case len(result.Suggestions) > 0:
		// Multiple matches - let user select
		selected, err := ui.FuzzySelectString("Multiple matches for '"+query+"'", result.Suggestions, query)
		if err != nil || selected == "" {
			ui.Abort("Selection canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		resolvedName = selected
	default:
		ui.Error("Host not found: %s", query)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 4}
	}

	host, cfg, err := parser.FindHostWithLocation(resolvedName)
	if err != nil || host == nil {
		ui.Error("Host not found: %s", resolvedName)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 4}
	}

	// Initialize vault and unlock if needed
	vm, _ := clisession.NewManager(vault.Auto())
	if vm != nil {
		_ = clisession.TryUnlockIfTTY(vm)
	}

	// Display host details
	contextName := displayHostDetails(host.Host, host.Lines, cfg.Path, vm, showSecret)

	// Audit when secrets are intentionally revealed
	if showSecret && vm != nil {
		if audit := vm.AuditLogger(); audit != nil {
			audit.Info("vault_reveal_secret", "host", host.Host, "context", contextName)
		}
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
