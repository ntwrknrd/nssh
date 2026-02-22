package host

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewPruneCmd creates the host prune command.
func NewPruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove stale and duplicate hosts",
		Long: `Detect hosts whose DNS names no longer resolve (NXDOMAIN) or that
appear as duplicates within the same config file, and remove them.

Runs DNS lookups against all configured hosts, then presents an
interactive selection of stale and duplicate entries to remove.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune()
		},
	}

	return cmd
}

// pruneCandidate is a host flagged for potential removal.
type pruneCandidate struct {
	host   *sshconfig.HostEntry
	reason string // "NXDOMAIN" or "duplicate"
}

func runPrune() error {
	parser := getParser()

	ui.CommandStart("PRUNE HOSTS")

	// Build context mapping upfront (may trigger vault unlock prompt)
	fileToContext := buildFileToContext()

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

	// Find duplicates within the same file before deduplicating for DNS
	dupes := findDuplicatesInFile(hosts)
	unique := deduplicateHosts(hosts)

	fmt.Printf("  Checking %d hosts...\n", len(unique))
	fmt.Println()

	results := resolveAllHosts(unique)
	summary := summarizeDNS(results)

	// Show summary counts (include duplicate count)
	printPruneSummary(summary, len(dupes))

	// Show error details if any
	printDNSErrors(results)

	// Build candidate list: NXDOMAIN + timeout + duplicates
	var candidates []pruneCandidate
	for _, r := range results {
		switch r.status {
		case "nxdomain":
			candidates = append(candidates, pruneCandidate{host: r.host, reason: "NXDOMAIN"})
		case "timeout":
			candidates = append(candidates, pruneCandidate{host: r.host, reason: "timeout"})
		}
	}
	for _, d := range dupes {
		candidates = append(candidates, pruneCandidate{host: d, reason: "duplicate"})
	}

	if len(candidates) == 0 {
		fmt.Println()
		ui.Info("No stale or duplicate hosts detected")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Display table
	fmt.Println()
	tbl := ui.NewTable("Host", "HostName", "Context", "Reason")
	for _, c := range candidates {
		ctx := contextForHost(c.host, fileToContext)
		hostname := c.host.HostName
		if hostname == "" || hostname == c.host.Host {
			hostname = "-"
		}
		tbl.AddRow(c.host.Host, hostname, ctx, c.reason)
	}
	tbl.Render()
	fmt.Println()

	// Multi-select
	options := make([]ui.FuzzySelectOption, len(candidates))
	for i, c := range candidates {
		options[i] = ui.FuzzySelectOption{
			Label: fmt.Sprintf("%s (%s)", c.host.Host, c.reason),
			Value: c.host.Host,
		}
	}

	indices, err := ui.FuzzySelectMulti("Select hosts to remove", options)
	if err != nil {
		ui.Abort("Selection failed: %s", err)
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}
	if len(indices) == 0 {
		ui.Abort("No hosts selected")
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	// Group selected hosts by source file, tracking reason
	type removal struct {
		host   *sshconfig.HostEntry
		reason string
	}
	byFile := make(map[string][]removal)
	for _, idx := range indices {
		c := candidates[idx]
		byFile[c.host.SourceFile] = append(byFile[c.host.SourceFile], removal{host: c.host, reason: c.reason})
	}

	// Process each file
	ui.SubSection("Removing")

	backedUp := make(map[string]bool)
	for path, removals := range byFile {
		cfg, err := parser.ParseFile(path)
		if err != nil {
			for _, rm := range removals {
				ui.Error("%s: parse failed: %s", rm.host.Host, err)
			}
			continue
		}

		// Backup once per file
		if !backedUp[path] {
			if err := createBackup(path, getBackupDir(), getMaxBackups()); err != nil {
				ui.Warning("Backup failed for %s: %v", abbreviateHome(path), err)
			}
			backedUp[path] = true
		}

		// Remove hosts
		for _, rm := range removals {
			if rm.reason == "duplicate" {
				// Remove only the duplicate entry, not all copies.
				// Match by line number of the Host directive.
				cfg.Hosts = removeHostByLines(cfg.Hosts, rm.host.Lines)
			} else {
				cfg.Hosts = sshconfig.RemoveHost(cfg.Hosts, rm.host.Host)
			}
			ui.Success("%s", rm.host.Host)
		}

		// Write updated config
		if err := parser.WriteFile(cfg); err != nil {
			ui.Error("Failed to write %s: %s", abbreviateHome(path), err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		// Clean up empty context
		if len(cfg.Hosts) == 0 {
			cleanupEmptyContext(path, map[string]string{
				filepath.Base(path): fileToContext[filepath.Base(path)],
			})
		}
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// printPruneSummary prints the DNS summary plus duplicate count.
func printPruneSummary(s dnsSummary, dupeCount int) {
	var parts []string
	if s.ok > 0 {
		parts = append(parts, fmt.Sprintf("[*] %d resolved", s.ok))
	}
	if s.nxdomain > 0 {
		parts = append(parts, fmt.Sprintf("[!] %d NXDOMAIN", s.nxdomain))
	}
	if s.cname > 0 {
		parts = append(parts, fmt.Sprintf("[*] %d renamed (CNAME)", s.cname))
	}
	if s.timeout > 0 {
		parts = append(parts, fmt.Sprintf("[!] %d timeout", s.timeout))
	}
	if dupeCount > 0 {
		parts = append(parts, fmt.Sprintf("[!] %d duplicates", dupeCount))
	}
	if s.skip > 0 {
		parts = append(parts, fmt.Sprintf("[*] %d skipped", s.skip))
	}
	if s.errCount > 0 {
		parts = append(parts, fmt.Sprintf("[!] %d errors", s.errCount))
	}
	fmt.Printf("  %s\n", strings.Join(parts, "   "))
}
