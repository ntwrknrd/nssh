package host

import (
	"fmt"
	"os"
	"path/filepath"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewRemoveCmd creates the host rm command.
func NewRemoveCmd() *cobra.Command {
	var (
		yes    bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:     "rm [FQDN|FILE]",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove hosts from config",
		Long: `Remove one or more SSH hosts from the configuration.

Single-host mode:
  nssh host rm server.example.com

Batch mode (from file):
  nssh host rm ./hosts.txt    # one FQDN per line
  nssh host rm ./hosts.csv    # CSV with headers (uses hostname)
  nssh host rm ./hosts.json   # JSON array of objects

Interactive mode if no arguments provided.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(args, yes, dryRun)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only")

	return cmd
}

func runRemove(args []string, yes, dryRun bool) error {
	parser := getParser()

	ui.CommandStart("REMOVE HOST")

	// Determine targets (FQDNs)
	var hostnames []string
	switch {
	case len(args) == 0:
		// Interactive mode - select from list
		hosts, err := parser.GetAllHosts()
		if err != nil {
			ui.Error("Failed to get hosts: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		if len(hosts) == 0 {
			ui.Warning("No hosts configured")
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}

		// Build options for fzf or numbered selection
		sshconfig.SortHosts(hosts)
		options := make([]ui.FuzzySelectOption, len(hosts))
		for i, h := range hosts {
			options[i] = ui.FuzzySelectOption{
				Label:       h.Host,
				Description: h.User(),
				Value:       h.Host,
			}
		}

		idx, err := ui.FuzzySelect("Select host to remove", options)
		if err != nil || idx < 0 {
			ui.Abort("Selection canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		hostnames = []string{hosts[idx].Host}
	case len(args) == 1:
		// Check if it's a batch file
		if IsBatchFile(args[0]) {
			entries, err := ParseBatchFile(args[0])
			if err != nil {
				ui.Error("%s", err)
				ui.CommandEnd(ui.StatusError)
				return &exit.ExitError{Code: 1}
			}
			for _, e := range entries {
				hostnames = append(hostnames, e.Host)
			}
		} else {
			hostnames = []string{args[0]}
		}
	default:
		hostnames = args
	}

	if len(hostnames) == 0 {
		ui.Warning("No hosts specified")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Group by config file for efficient updates
	type removal struct {
		host *sshconfig.HostEntry
		cfg  *sshconfig.ParsedConfig
	}
	var removals []removal

	for _, hostname := range hostnames {
		// Use fuzzy matching to find the host
		result, err := parser.MatchHost(hostname)
		if err != nil {
			ui.Error("Failed to search for host %s: %s", hostname, err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		var resolvedName string
		switch {
		case result.Host != nil:
			resolvedName = result.Host.Host
		case len(result.Suggestions) > 0:
			// Multiple matches - let user select
			selected, err := ui.FuzzySelectString("Multiple matches for '"+hostname+"'", result.Suggestions, hostname)
			if err != nil || selected == "" {
				ui.Info("Skipping %s", hostname)
				continue
			}
			resolvedName = selected
		default:
			ui.Info("Host not found: %s", hostname)
			continue
		}

		// Find the host with its config location
		host, cfg, err := parser.FindHostWithLocation(resolvedName)
		if err != nil || host == nil {
			ui.Info("Host not found: %s", resolvedName)
			continue
		}

		removals = append(removals, removal{host, cfg})
	}

	if len(removals) == 0 {
		ui.Info("No changes required")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Preview what will be removed - group by config file
	fmt.Println()

	// Build file-to-context mapping
	fileToContext := make(map[string]string)
	if mgr, err := clisession.NewManager(vault.Auto()); err == nil {
		// Unlock vault if needed and TTY is available
		_ = clisession.TryUnlockIfTTY(mgr)
		if contexts, err := mgr.ListContexts(); err == nil {
			for _, ctx := range contexts {
				fileToContext[ctx.GitIncludeFile] = ctx.Name
			}
		}
	}

	// Group by config file for pretty display
	byFile := make(map[string][]removal)
	for _, r := range removals {
		byFile[r.cfg.Path] = append(byFile[r.cfg.Path], r)
	}

	var groups []ui.BatchGroup
	for path, fileRemovals := range byFile {
		var items []ui.BatchItem
		for _, r := range fileRemovals {
			items = append(items, ui.BatchItem{Name: r.host.Host})
		}

		// Use context name if available, otherwise use filename
		groupName := filepath.Base(path)
		if ctxName, ok := fileToContext[groupName]; ok {
			groupName = ctxName
		}

		groups = append(groups, ui.BatchGroup{
			Name:  groupName,
			Items: items,
		})
	}

	ui.BatchPreview(groups, "-")
	if len(groups) > 1 {
		ui.BatchSummaryLine(len(removals), len(groups), "host")
	}

	if dryRun {
		ui.Warning("Dry run - no changes made")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Confirm
	if !yes {
		fmt.Println()
		confirm, _ := ui.Confirm(fmt.Sprintf("Remove %d host(s)?", len(removals)), false)
		if !confirm {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	// Process each file
	ui.SubSection("Removing Hosts")
	progress := ui.NewBatchProgress(func() []string {
		names := make([]string, len(removals))
		for i, r := range removals {
			names[i] = r.host.Host
		}
		return names
	}())

	backedUp := make(map[string]bool)
	for path, fileRemovals := range byFile {
		// Parse fresh (in case file changed)
		cfg, err := parser.ParseFile(path)
		if err != nil {
			for _, r := range fileRemovals {
				progress.MarkError(r.host.Host, "parse failed")
			}
			continue
		}

		// Create backup (once per file)
		if !backedUp[path] {
			if err := createBackup(path, getBackupDir(), getMaxBackups()); err != nil {
				ui.Warning("Backup failed for %s: %v", abbreviateHome(path), err)
			}
			backedUp[path] = true
		}

		// Remove hosts
		for _, r := range fileRemovals {
			cfg.Hosts = sshconfig.RemoveHost(cfg.Hosts, r.host.Host)
			progress.MarkSuccess(r.host.Host)
		}

		// Write updated config
		if err := parser.WriteFile(cfg); err != nil {
			ui.Error("Failed to write %s: %s", abbreviateHome(path), err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		// If config file is now empty, clean up context
		if len(cfg.Hosts) == 0 {
			cleanupEmptyContext(path, fileToContext)
		}
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// cleanupEmptyContext removes an empty config file and its associated context.
func cleanupEmptyContext(configPath string, fileToContext map[string]string) {
	filename := filepath.Base(configPath)
	ctxName, hasContext := fileToContext[filename]

	// Delete the empty config file
	if err := os.Remove(configPath); err != nil {
		ui.Warning("Failed to remove empty config: %v", err)
		return
	}

	// Delete the context if one exists for this file
	if hasContext {
		mgr, err := clisession.NewManager(vault.Auto())
		if err != nil {
			return
		}
		if _, err := mgr.DeleteContext(ctxName); err != nil {
			ui.Warning("Failed to remove context '%s': %v", ctxName, err)
			return
		}
		ui.Info("Removed empty context '%s'", ctxName)
	} else {
		ui.Info("Removed empty config file")
	}
}
