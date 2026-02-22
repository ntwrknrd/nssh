package host

import (
	"fmt"
	"path/filepath"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewRenameCmd creates the host rename command.
func NewRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename",
		Short: "Rename hosts with CNAME changes",
		Long: `Detect hosts whose DNS names have changed via CNAME records and
update the SSH config to match.

For each renamed host, the old Host alias is kept so existing scripts
and muscle memory continue to work. When the target host already exists,
the old entry is merged as an alias on the existing entry.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename()
		},
	}

	return cmd
}

func runRename() error {
	parser := getParser()

	ui.CommandStart("RENAME HOSTS")

	// Build context mapping upfront (may trigger vault unlock prompt)
	_ = buildFileToContext()

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

	hosts = deduplicateHosts(hosts)

	fmt.Printf("  Checking %d hosts...\n", len(hosts))
	fmt.Println()

	results := resolveAllHosts(hosts)
	summary := summarizeDNS(results)

	// Show summary counts
	printDNSSummary(summary)

	// Show error details if any
	printDNSErrors(results)

	// Filter to CNAME results
	var cnameResults []dnsResult
	for _, r := range results {
		if r.status == "cname" {
			cnameResults = append(cnameResults, r)
		}
	}

	if len(cnameResults) == 0 {
		fmt.Println()
		ui.Info("No renamed hosts detected")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Display table
	fmt.Println()
	tbl := ui.NewTable("Host", "Current HostName", "New HostName")
	for _, r := range cnameResults {
		tbl.AddRow(r.host.Host, r.target, r.detail)
	}
	tbl.Render()
	fmt.Println()

	// Multi-select
	options := make([]ui.FuzzySelectOption, len(cnameResults))
	for i, r := range cnameResults {
		options[i] = ui.FuzzySelectOption{
			Label: fmt.Sprintf("%s -> %s", r.host.Host, sshconfig.DeriveHostID(r.detail)),
			Value: r.host.Host,
		}
	}

	indices, err := ui.FuzzySelectMulti("Select hosts to rename", options)
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

	// Build host index for collision detection and merge targets
	allHosts, _ := parser.GetAllHosts()
	hostByID := make(map[string]*sshconfig.HostEntry)
	for _, h := range allHosts {
		hostByID[strings.ToLower(h.Host)] = h
	}

	// Classify operations: rename (no collision) or merge (target exists)
	type renameOp struct {
		host        *sshconfig.HostEntry
		newID       string
		cnameTarget string
	}
	type mergeOp struct {
		old    *sshconfig.HostEntry // entry to remove
		target *sshconfig.HostEntry // existing entry to add alias to
		alias  string               // old Host ID to add as alias
	}

	byFile := make(map[string][]renameOp)
	var merges []mergeOp

	for _, idx := range indices {
		r := cnameResults[idx]
		newID := sshconfig.DeriveHostID(r.detail)

		if strings.EqualFold(newID, r.host.Host) {
			// Same ID -- just update HostName, no collision
			byFile[r.host.SourceFile] = append(byFile[r.host.SourceFile], renameOp{
				host:        r.host,
				newID:       newID,
				cnameTarget: r.detail,
			})
			continue
		}

		existing, hasCollision := hostByID[strings.ToLower(newID)]
		if !hasCollision {
			byFile[r.host.SourceFile] = append(byFile[r.host.SourceFile], renameOp{
				host:        r.host,
				newID:       newID,
				cnameTarget: r.detail,
			})
		} else {
			merges = append(merges, mergeOp{
				old:    r.host,
				target: existing,
				alias:  r.host.Host,
			})
		}
	}

	if len(byFile) == 0 && len(merges) == 0 {
		ui.Info("No changes to apply")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Process renames
	ui.SubSection("Updating")

	backedUp := make(map[string]bool)
	backupFile := func(path string) {
		if !backedUp[path] {
			if err := createBackup(path, getBackupDir(), getMaxBackups()); err != nil {
				ui.Warning("Backup failed for %s: %v", abbreviateHome(path), err)
			}
			backedUp[path] = true
		}
	}

	for path, ops := range byFile {
		cfg, err := parser.ParseFile(path)
		if err != nil {
			for _, op := range ops {
				ui.Error("%s: parse failed: %s", op.host.Host, err)
			}
			continue
		}

		backupFile(path)

		for _, op := range ops {
			host := sshconfig.FindHostByPattern(cfg.Hosts, op.host.Host)
			if host == nil {
				ui.Error("%s: not found in %s", op.host.Host, abbreviateHome(path))
				continue
			}

			oldID := host.Host

			// Update Host line: "Host <new-id> <old-id>"
			for i, line := range host.Lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(strings.ToLower(trimmed), "host ") {
					host.Lines[i] = fmt.Sprintf("Host %s %s\n", op.newID, oldID)
					break
				}
			}

			// Update HostName
			updateHostProperty(host, "hostname", op.cnameTarget)

			// Update in-memory fields
			host.Host = op.newID
			host.Patterns = []string{op.newID, oldID}
			host.HostName = op.cnameTarget

			ui.Success("%s -> %s (alias: %s)", oldID, op.newID, oldID)
		}

		if err := parser.WriteFile(cfg); err != nil {
			ui.Error("Failed to write %s: %s", abbreviateHome(path), err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}
	}

	// Process merges: add alias to existing entry, remove old entry
	if len(merges) > 0 {
		// Collect all files we need to touch
		// For each merge: target file (add alias) + old file (remove entry)
		type fileEdit struct {
			addAliases     map[string]string // host pattern -> alias to add
			removePatterns []string
		}
		edits := make(map[string]*fileEdit)

		getEdit := func(path string) *fileEdit {
			if e, ok := edits[path]; ok {
				return e
			}
			e := &fileEdit{addAliases: make(map[string]string)}
			edits[path] = e
			return e
		}

		for _, m := range merges {
			// Add alias to target's file
			targetEdit := getEdit(m.target.SourceFile)
			targetEdit.addAliases[strings.ToLower(m.target.Host)] = m.alias

			// Remove old entry from its file
			oldEdit := getEdit(m.old.SourceFile)
			oldEdit.removePatterns = append(oldEdit.removePatterns, m.old.Host)
		}

		for path, edit := range edits {
			cfg, err := parser.ParseFile(path)
			if err != nil {
				ui.Error("parse %s: %s", abbreviateHome(path), err)
				continue
			}

			backupFile(path)

			// Add aliases
			for hostPattern, alias := range edit.addAliases {
				host := sshconfig.FindHostByPattern(cfg.Hosts, hostPattern)
				if host == nil {
					continue
				}
				// Append alias to Host line
				for i, line := range host.Lines {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(strings.ToLower(trimmed), "host ") {
						// Add alias at the end of the Host line
						host.Lines[i] = strings.TrimRight(line, "\n") + " " + alias + "\n"
						break
					}
				}
				host.Patterns = append(host.Patterns, alias)
			}

			// Remove old entries
			for _, pattern := range edit.removePatterns {
				cfg.Hosts = sshconfig.RemoveHost(cfg.Hosts, pattern)
			}

			if err := parser.WriteFile(cfg); err != nil {
				ui.Error("Failed to write %s: %s", abbreviateHome(path), err)
				ui.CommandEnd(ui.StatusError)
				return &exit.ExitError{Code: 1}
			}
		}

		for _, m := range merges {
			ui.Success("%s merged into %s (alias: %s)", m.old.Host, m.target.Host, m.alias)
		}
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// printDNSSummary prints a one-line summary of DNS resolution results.
func printDNSSummary(s dnsSummary) {
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
	if s.skip > 0 {
		parts = append(parts, fmt.Sprintf("[*] %d skipped", s.skip))
	}
	if s.errCount > 0 {
		parts = append(parts, fmt.Sprintf("[!] %d errors", s.errCount))
	}
	fmt.Printf("  %s\n", strings.Join(parts, "   "))
}

// printDNSErrors lists hosts that had DNS resolution errors.
func printDNSErrors(results []dnsResult) {
	var errResults []dnsResult
	for _, r := range results {
		if r.status == "error" {
			errResults = append(errResults, r)
		}
	}
	if len(errResults) == 0 {
		return
	}

	fmt.Println()
	ui.SubSection("DNS Errors", true)
	for _, r := range errResults {
		ui.Warning("%s: %s", r.host.Host, r.detail)
	}
}

// buildFileToContext creates a mapping from include filename to context name.
func buildFileToContext() map[string]string {
	fileToContext := make(map[string]string)
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fileToContext
	}
	_ = clisession.TryUnlockIfTTY(mgr)
	contexts, err := mgr.ListContexts()
	if err != nil {
		return fileToContext
	}
	for _, ctx := range contexts {
		fileToContext[ctx.GitIncludeFile] = ctx.Name
	}
	return fileToContext
}

// contextForHost returns the context name for a host entry, or the filename.
func contextForHost(host *sshconfig.HostEntry, fileToContext map[string]string) string {
	base := filepath.Base(host.SourceFile)
	if name, ok := fileToContext[base]; ok {
		return name
	}
	return base
}
