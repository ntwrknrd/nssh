package host

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewSortCmd creates the host sort command.
func NewSortCmd() *cobra.Command {
	var selectPattern string

	cmd := &cobra.Command{
		Use:   "sort",
		Short: "Sort hosts in config",
		Long: `Sort SSH hosts alphabetically within their config files.

This operation sorts hosts in each Include file while preserving
the header comments and global settings.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSort(selectPattern)
		},
	}

	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "filter files by regex pattern")

	return cmd
}

func runSort(selectPattern string) error {
	parser := getParser()

	ui.CommandStart("SORT HOSTS")

	includes, err := parser.FindIncludeFiles()
	if err != nil {
		ui.Error("Failed to find include files: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	if len(includes) == 0 {
		ui.Warning("No SSH config include files found")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Apply pattern filter
	if selectPattern != "" {
		pattern, err := regexp.Compile("(?i)" + selectPattern)
		if err != nil {
			ui.Error("Invalid regex pattern: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		var filtered []string
		for _, inc := range includes {
			if pattern.MatchString(filepath.Base(inc)) {
				filtered = append(filtered, inc)
			}
		}
		includes = filtered

		if len(includes) == 0 {
			ui.Warning("No files matching pattern: %s", selectPattern)
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}

		ui.Info("Filter: %s", selectPattern)
	}

	sorted := 0
	for _, path := range includes {
		cfg, err := parser.ParseFile(path)
		if err != nil {
			ui.Warning("Failed to parse %s: %v", abbreviateHome(path), err)
			continue
		}

		if len(cfg.Hosts) <= 1 {
			ui.Noop("Skipping %s (%d hosts)", abbreviateHome(path), len(cfg.Hosts))
			continue
		}

		// Check if already sorted
		if isAlreadySorted(cfg.Hosts) {
			ui.Noop("Already sorted: %s", abbreviateHome(path))
			continue
		}

		// Create backup
		if err := createBackup(path, getBackupDir(), getMaxBackups()); err != nil {
			ui.Warning("Backup failed for %s: %v", abbreviateHome(path), err)
		}

		// Sort hosts
		sshconfig.SortHosts(cfg.Hosts)

		// Write updated config
		if err := parser.WriteFile(cfg); err != nil {
			ui.Error("Failed to write %s: %s", abbreviateHome(path), err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		sorted++
		ui.Success("Sorted %d hosts in %s", len(cfg.Hosts), abbreviateHome(path))
	}

	if sorted == 0 {
		ui.Noop("No files needed sorting")
		ui.CommandEnd(ui.StatusNoop)
	} else {
		ui.Info("Total: %d file(s) sorted", sorted)
		ui.CommandEnd(ui.StatusSuccess)
	}

	return nil
}

// isAlreadySorted checks if hosts are already in alphabetical order.
func isAlreadySorted(hosts []*sshconfig.HostEntry) bool {
	for i := 1; i < len(hosts); i++ {
		prev := strings.ToLower(hosts[i-1].Host)
		curr := strings.ToLower(hosts[i].Host)
		if prev > curr {
			return false
		}
	}
	return true
}
