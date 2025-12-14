package host

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewAddCmd creates the host add command.
func NewAddCmd() *cobra.Command {
	var (
		yes      bool
		dryRun   bool
		force    bool
		authType string
		hostname string
	)

	cmd := &cobra.Command{
		Use:   "add [FQDN|FILE]",
		Short: "Add hosts to config",
		Long: `Add one or more SSH hosts to the configuration.

The FQDN becomes the HostName (resolved address) and a Host identifier is derived from it.
Example: server.example.com -> Host: server, HostName: server.example.com

Single-host mode (interactive):
  nssh host add server.example.com              # Host=server, HostName=server.example.com
  nssh host add server.example.com -y           # use defaults
  nssh host add switch --hostname 192.168.1.10  # Host=switch, HostName=192.168.1.10

Batch mode (from file):
  nssh host add ./hosts.txt    # one FQDN per line
  nssh host add ./hosts.csv    # CSV with headers (host,hostname,user,port,context)
  nssh host add ./hosts.json   # JSON array of objects

Interactive mode if no arguments provided.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(args, yes, dryRun, force, authType, hostname)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept defaults")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "preview only")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip connection test")
	cmd.Flags().StringVarP(&authType, "auth", "A", "", "auth type: password or key")
	cmd.Flags().StringVarP(&hostname, "hostname", "H", "", "resolved address (HostName)")

	return cmd
}

func runAdd(args []string, yes, dryRun, force bool, authType, hostname string) error {
	// Check if this is a batch file operation
	if len(args) == 1 && IsBatchFile(args[0]) {
		return runBatchAdd(args[0], yes, dryRun, force, authType)
	}

	ui.CommandStart("ADD HOST")

	// Check if any contexts are configured
	if err := requireContexts(); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	// Determine input source
	var hostnames []string
	switch {
	case len(args) == 0:
		// Interactive mode - prompt for hostname
		result, err := ui.InputHostname("Hostname", "")
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		hostnames = []string{result}
	case len(args) >= 1:
		// Use provided hostnames directly
		hostnames = args
	}

	if len(hostnames) == 0 {
		ui.Error("No hostnames provided")
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	parser := getParser()

	// Get context for target config file (pass first hostname for domain matching)
	targetFile, context, err := selectTargetFile(parser, hostnames[0])
	if err != nil {
		ui.Abort("%s", err)
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	// Get default values - prefer context credential username over config default
	defaultUser := getDefaultUser()
	if context != nil && context.Credential != nil && context.Credential.Username != "" {
		defaultUser = context.Credential.Username
	}

	// Determine default auth type (flag > context > key)
	usesPassword := authType == "password"
	if authType == "" && context != nil && context.Credential != nil {
		usesPassword = true
	}

	if dryRun {
		ui.Info("Target: %s", abbreviateHome(targetFile))
		ui.Info("User: %s", defaultUser)
		ui.Info("Auth: %s", map[bool]string{true: "password", false: "key"}[usesPassword])
		for _, fqdn := range hostnames {
			hostID := sshconfig.DeriveHostID(fqdn)
			target := fqdn
			if hostname != "" {
				target = hostname
			}
			ui.Warning("Would add: %s -> %s@%s", hostID, defaultUser, target)
		}
		ui.Warning("Run without --dry-run to actually add hosts")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Parse target file
	cfg, err := parser.ParseFile(targetFile)
	if err != nil {
		ui.Error("Failed to parse config: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	ui.Info("Target: %s", abbreviateHome(targetFile))

	// Process each host
	var hostsToAdd []*sshconfig.HostEntry
	var hostUsers []string

	for _, target := range hostnames {
		// Derive Host identifier from FQDN (e.g., "test-host.example.com" -> "test-host")
		hostID := sshconfig.DeriveHostID(target)

		// In interactive mode, allow user to override Host and HostName
		if !yes {
			// Prompt for Host (identifier)
			result, err := ui.InputWithDefault("Host", hostID)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			hostID = result

			// Prompt for target (HostName) - use flag value if provided, else default to original input
			// If context has a domain and target is a simple hostname (no dots, not an IP), append domain
			targetDefault := target
			if hostname != "" {
				targetDefault = hostname
			} else if context != nil && context.Domain != "" && !strings.Contains(target, ".") && net.ParseIP(target) == nil {
				targetDefault = target + "." + context.Domain
			}
			result, err = ui.InputWithDefault("HostName", targetDefault)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			target = result
		} else if hostname != "" {
			// --yes mode with --hostname flag: use flag value as target
			target = hostname
		}

		// Get port - default 22
		port := 22
		if !yes {
			portStr, err := ui.InputWithDefault("Port", "22")
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
				port = p
			}
		}

		// Check if Host identifier already exists
		if sshconfig.FindHostByPattern(cfg.Hosts, hostID) != nil {
			ui.Warning("Host %s already exists, skipping", hostID)
			continue
		}

		// Get user - prompt with context credential username as default
		user := defaultUser
		if !yes {
			result, err := ui.InputWithDefault("User", user)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			user = result
		}

		// Prompt for authentication type if not specified via flag
		if authType == "" && !yes {
			var authOptions []ui.SelectOption
			if usesPassword {
				// Default to password when context has credentials
				authOptions = []ui.SelectOption{
					{Label: "Password", Value: "password"},
					{Label: "Key", Value: "key"},
				}
			} else {
				// Default to key
				authOptions = []ui.SelectOption{
					{Label: "Key", Value: "key"},
					{Label: "Password", Value: "password"},
				}
			}
			selected, err := ui.Select("Authentication", authOptions)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			usesPassword = (selected == "password")
		}

		// Create host entry: hostID as Host, target as HostName
		host := sshconfig.CreateHostEntry(hostID, target, user, port, usesPassword, targetFile)
		hostsToAdd = append(hostsToAdd, host)
		hostUsers = append(hostUsers, user)
	}

	if len(hostsToAdd) == 0 {
		ui.Warning("No hosts to add")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Prompt for password if password auth selected
	var enteredPassword *secret.Secret
	if usesPassword && !yes {
		pw, err := ui.Password("Password (optional)")
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		if pw != "" {
			enteredPassword = secret.NewFromString(pw)
		}
	}

	// Test connections first (unless --force)
	// This uses temp config files so we don't modify the real config until tests pass
	var testedHosts []*sshconfig.HostEntry
	if !force {
		for i, host := range hostsToAdd {
			result, finalHost, err := TestHostWithTempConfig(host, 5, true, enteredPassword)
			if err != nil {
				ui.Error("Failed to test %s: %s", host.Host, err)
				ui.CommandEnd(ui.StatusError)
				return &exit.ExitError{Code: 1}
			}

			if !result.Success {
				// Connection failed - display result and offer options
				DisplayCompatResult(result, host.Host)

				// Write debug file
				if result.TestResult != nil {
					debugFile := WriteDebugFile(host.Host, result.TestResult,
						fmt.Sprintf("Stopped reason: %s\nIterations: %d", result.StoppedReason, result.Iterations))
					if debugFile != "" {
						ui.Info("Debug info: %s", debugFile)
					}
				}

				// If not auto-mode, ask if user wants to keep the host anyway
				if !yes {
					keep, _ := ui.Confirm("Connection test failed. Add host entry anyway?", false)
					if keep {
						ui.Warning("Host will be added despite failed connection test")
						// Use finalHost if available (has partial compat fixes), otherwise use original host
						if finalHost != nil {
							testedHosts = append(testedHosts, finalHost)
						} else {
							testedHosts = append(testedHosts, host)
						}
						continue
					}
				}

				ui.Error("Host %s not added (connection test failed)", host.Host)
				ui.Info("Use --force to skip connection testing")
				ui.CommandEnd(ui.StatusError)
				return nil
			}

			// Use the final host with any compat fixes applied
			testedHosts = append(testedHosts, finalHost)
			hostUsers[i] = finalHost.User() // Update user in case it changed

			if result.Success && len(result.FixesApplied) == 0 {
				ui.Success("Connection test passed for %s", host.Host)
			}
		}
		hostsToAdd = testedHosts
	}

	// Show insertion preview for each host (unless --yes)
	// This is shown AFTER testing, so it includes any compat fixes that were applied
	if !yes {
		fmt.Println() // Blank line after testing output
		for _, host := range hostsToAdd {
			idx := sshconfig.FindInsertionIndex(cfg.Hosts, host.Host)

			// Get before/after context
			var beforeHost, afterHost []string
			if idx > 0 {
				beforeHost = cfg.Hosts[idx-1].Lines
			}
			if idx < len(cfg.Hosts) {
				afterHost = cfg.Hosts[idx].Lines
			}

			ui.InsertionPreview(host.Lines, beforeHost, afterHost, targetFile, 4)
		}

		// Confirm
		confirmed, err := ui.Confirm(fmt.Sprintf("Add %d host(s)?", len(hostsToAdd)), true)
		if err != nil || !confirmed {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	// Insert all hosts
	for _, host := range hostsToAdd {
		idx := sshconfig.FindInsertionIndex(cfg.Hosts, host.Host)
		cfg.Hosts = append(cfg.Hosts[:idx], append([]*sshconfig.HostEntry{host}, cfg.Hosts[idx:]...)...)
	}

	// Create backup
	if err := createBackup(targetFile, getBackupDir(), getMaxBackups()); err != nil {
		ui.Warning("Backup failed: %v", err)
	}

	// Write config
	if err := parser.WriteFile(cfg); err != nil {
		ui.Error("Failed to write config: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	// Store password in vault if provided
	if enteredPassword != nil {
		mgr, err := clisession.NewManager(vault.Auto())
		if err == nil {
			_ = clisession.TryUnlockIfTTY(mgr)
			for _, host := range hostsToAdd {
				if err := mgr.AddHostCredential(host.Host, host.User(), enteredPassword); err != nil {
					ui.Warning("Failed to store password for %s: %v", host.Host, err)
				}
			}
		}
	}

	for i, host := range hostsToAdd {
		ui.Success("Added %s -> %s@%s", host.Host, hostUsers[i], host.HostName)
	}

	ui.Info("Total: %d host(s) added to %s", len(hostsToAdd), abbreviateHome(targetFile))

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// runBatchAdd handles batch add mode from a file (.csv, .json).
func runBatchAdd(path string, yes, dryRun, force bool, authType string) error {
	ui.CommandStart("BATCH ADD HOSTS")

	// Check if any contexts are configured
	if err := requireContexts(); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	// Parse batch file
	entries, err := ParseBatchFile(path)
	if err != nil {
		ui.Error("%s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	if len(entries) == 0 {
		ui.Warning("No entries found in %s", path)
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	ui.Success("Loaded %d entries from %s", len(entries), path)

	parser := getParser()
	mgr, _ := clisession.NewManager(vault.Auto())
	// Unlock vault if needed (spawner handles TTY/NSSH_PASSPHRASE)
	if mgr != nil {
		_ = clisession.TryUnlockIfTTY(mgr)
	}
	result := &BatchResult{}

	// Auto-create missing contexts
	contextsToCreate := findMissingContexts(entries, mgr)
	if len(contextsToCreate) > 0 {
		ui.SubSection("Prerequisites")
		for _, ctxName := range contextsToCreate {
			if dryRun {
				ui.Info("Would create context '%s'", ctxName)
			} else {
				if err := autoCreateContext(mgr, ctxName); err != nil {
					ui.Warning("Failed to create context '%s': %v", ctxName, err)
				} else {
					ui.Success("Created context '%s'", ctxName)
				}
			}
		}
		// Refresh parser's include cache after creating new files
		parser = getParser()
	}

	// Validate entries and check for duplicates
	var validEntries []HostEntry
	var skippedMessages []string

	for i, entry := range entries {
		// Validate host
		if errMsg := ValidateHostname(entry.Host); errMsg != "" {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: %s", i+1, errMsg))
			continue
		}

		// Check for duplicate across all config files (using derived Host identifier)
		hostID := sshconfig.DeriveHostID(entry.Host)
		if existingHost := findHostAnyConfig(parser, hostID); existingHost != "" {
			result.Skipped++
			skippedMessages = append(skippedMessages, fmt.Sprintf("%s (exists in %s)", hostID, abbreviateHome(existingHost)))
			continue
		}

		// Check context exists (after auto-creation)
		if entry.Context != "" && mgr != nil {
			ctx, _ := mgr.GetContext(entry.Context)
			if ctx == nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: context '%s' not found", entry.Host, entry.Context))
				continue
			}
		}

		validEntries = append(validEntries, entry)
	}

	// Display validation issues (only if there are any)
	if len(skippedMessages) > 0 || len(result.Errors) > 0 {
		ui.SubSection("Validation")
		if len(skippedMessages) > 0 {
			ui.Info("Skipping %d (already exist)", len(skippedMessages))
			for _, msg := range skippedMessages[:min(5, len(skippedMessages))] {
				fmt.Printf("    %s %s\n", ui.Gray("-"), ui.Gray(msg))
			}
			if len(skippedMessages) > 5 {
				fmt.Printf("    %s\n", ui.Gray(fmt.Sprintf("... and %d more", len(skippedMessages)-5)))
			}
		}
		if len(result.Errors) > 0 {
			ui.Error("%d validation error(s)", len(result.Errors))
			for _, msg := range result.Errors[:min(5, len(result.Errors))] {
				fmt.Printf("    %s %s\n", ui.Red("x"), ui.Gray(msg))
			}
			if len(result.Errors) > 5 {
				fmt.Printf("    %s\n", ui.Gray(fmt.Sprintf("... and %d more", len(result.Errors)-5)))
			}
		}
	}

	if len(validEntries) == 0 {
		if result.Skipped > 0 && result.Failed == 0 {
			ui.Info("No changes required")
			ui.CommandEnd(ui.StatusNoop)
			return nil
		}
		ui.Error("No valid entries to process")
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	// Group entries by context for preview
	byContext := groupEntriesByContext(validEntries)
	fmt.Println()

	// Build batch groups for pretty display
	var groups []ui.BatchGroup
	for ctx, ctxEntries := range byContext {
		ctxName := ctx
		if ctxName == "" {
			ctxName = "default"
		}

		var items []ui.BatchItem
		for _, e := range ctxEntries {
			item := ui.BatchItem{Name: e.Host}
			// Show HostName if different from Host (IP override)
			if e.HostName != "" && e.HostName != e.Host {
				item.Detail = fmt.Sprintf("-> %s", e.HostName)
			}
			items = append(items, item)
		}
		groups = append(groups, ui.BatchGroup{Name: ctxName, Items: items})
	}

	ui.BatchPreview(groups, "+")
	if len(groups) > 1 {
		ui.BatchSummaryLine(len(validEntries), len(groups), "host")
	}

	// Dry run - stop here
	if dryRun {
		ui.Warning("Dry run - no changes made")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Confirm unless --yes
	if !yes {
		fmt.Println()
		confirmed, err := ui.Confirm(fmt.Sprintf("Add %d host(s)?", len(validEntries)), true)
		if err != nil || !confirmed {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	// Build host entries and test connections (unless --force)
	type testedEntry struct {
		entry      HostEntry
		host       *sshconfig.HostEntry
		targetFile string
		context    *vault.ContextEntry
		passed     bool
		error      string
	}
	var testedEntries []testedEntry

	// Test connections first if not --force
	if !force {
		ui.SubSection("Testing Connections")
		for _, entry := range validEntries {
			// Build host entry
			targetFile, context, err := selectTargetFileForContext(parser, entry.Context, entry.Host)
			if err != nil {
				testedEntries = append(testedEntries, testedEntry{
					entry:  entry,
					passed: false,
					error:  err.Error(),
				})
				continue
			}

			host := buildHostEntry(entry, targetFile, context, authType)

			// Get password for testing (from entry or context)
			var batchPassword *secret.Secret
			if entry.Password != "" {
				batchPassword = secret.NewFromString(entry.Password)
			}

			// Test using temp config
			testResult, finalHost, err := TestHostWithTempConfig(host, 5, true, batchPassword)
			if err != nil {
				testedEntries = append(testedEntries, testedEntry{
					entry:      entry,
					host:       host,
					targetFile: targetFile,
					context:    context,
					passed:     false,
					error:      fmt.Sprintf("test setup failed: %s", err),
				})
				continue
			}

			if testResult.Success {
				testedEntries = append(testedEntries, testedEntry{
					entry:      entry,
					host:       finalHost,
					targetFile: targetFile,
					context:    context,
					passed:     true,
				})
			} else {
				testedEntries = append(testedEntries, testedEntry{
					entry:      entry,
					host:       host,
					targetFile: targetFile,
					context:    context,
					passed:     false,
					error:      testResult.StoppedReason,
				})
			}
		}

		// Show test results summary
		fmt.Println()
		var passed, failed int
		for i := range testedEntries {
			if testedEntries[i].passed {
				passed++
			} else {
				failed++
			}
		}
		if passed > 0 {
			ui.Success("%d host(s) passed connection tests", passed)
		}
		if failed > 0 {
			ui.Warning("%d host(s) failed connection tests", failed)
			for i := range testedEntries {
				if !testedEntries[i].passed {
					fmt.Printf("    %s %s: %s\n", ui.Red("x"), testedEntries[i].entry.Host, ui.Gray(testedEntries[i].error))
				}
			}
		}

		// If all failed, abort
		if passed == 0 {
			ui.Error("No hosts passed connection tests")
			ui.Info("Use --force to skip connection testing")
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}
	} else {
		// --force: build entries without testing
		for _, entry := range validEntries {
			targetFile, context, err := selectTargetFileForContext(parser, entry.Context, entry.Host)
			if err != nil {
				testedEntries = append(testedEntries, testedEntry{
					entry:  entry,
					passed: false,
					error:  err.Error(),
				})
				continue
			}

			host := buildHostEntry(entry, targetFile, context, authType)

			testedEntries = append(testedEntries, testedEntry{
				entry:      entry,
				host:       host,
				targetFile: targetFile,
				context:    context,
				passed:     true,
			})
		}
	}

	// Create backups for each unique target file (once per file)
	backedUp := make(map[string]bool)

	// Write only passed entries
	ui.SubSection("Adding Hosts")
	progress := ui.NewBatchProgress(func() []string {
		var names []string
		for i := range testedEntries {
			if testedEntries[i].passed {
				names = append(names, testedEntries[i].entry.Host)
			}
		}
		return names
	}())

	for i := range testedEntries {
		te := &testedEntries[i]
		if !te.passed {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", te.entry.Host, te.error))
			continue
		}

		if err := writeBatchEntry(parser, mgr, te.entry, te.host, te.targetFile, backedUp); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", te.entry.Host, err))
			progress.MarkError(te.entry.Host, err.Error())
		} else {
			result.Added++
			progress.MarkSuccess(te.entry.Host)
		}
	}

	// Final status
	if result.Failed > 0 && result.Added == 0 {
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	if result.Failed > 0 {
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// findMissingContexts returns a list of context names that don't exist.
func findMissingContexts(entries []HostEntry, mgr *vault.Manager) []string {
	if mgr == nil {
		return nil
	}

	seen := make(map[string]bool)
	var missing []string

	for _, e := range entries {
		if e.Context == "" || seen[e.Context] {
			continue
		}
		seen[e.Context] = true

		ctx, _ := mgr.GetContext(e.Context)
		if ctx == nil {
			missing = append(missing, e.Context)
		}
	}

	return missing
}

// autoCreateContext creates a context with its SSH config file.
func autoCreateContext(mgr *vault.Manager, name string) error {
	paths := config.DefaultPaths()
	confD := filepath.Join(paths.SSHConfigDir, "conf.d")
	gitIncludeFile := fmt.Sprintf("%s_hosts", name)
	configFilePath := filepath.Join(confD, gitIncludeFile)

	// Create conf.d directory if needed
	if err := os.MkdirAll(confD, 0700); err != nil {
		return fmt.Errorf("create conf.d: %w", err)
	}

	// Create SSH config file if it doesn't exist
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		f, err := os.Create(configFilePath)
		if err != nil {
			return fmt.Errorf("create config file: %w", err)
		}
		_ = f.Close()
	}

	// Create context in vault
	return mgr.CreateContext(name, gitIncludeFile, "", nil)
}

// buildHostEntry creates a host entry from a batch entry without writing to config.
func buildHostEntry(entry HostEntry, targetFile string, context *vault.ContextEntry, authType string) *sshconfig.HostEntry {
	// Determine user
	user := entry.User
	if user == "" {
		if context != nil && context.Credential != nil && context.Credential.Username != "" {
			user = context.Credential.Username
		} else {
			user = getDefaultUser()
		}
	}

	// Determine port
	port := entry.Port
	if port == 0 {
		port = 22
	}

	// Determine auth type
	usesPassword := authType == "password"
	if authType == "" {
		if entry.Password != "" || (context != nil && context.Credential != nil) {
			usesPassword = true
		}
	}

	// Derive Host identifier from entry
	hostID := sshconfig.DeriveHostID(entry.Host)

	// Target is HostName if set, otherwise the Host itself
	target := entry.HostName
	if target == "" {
		target = entry.Host
	}

	// Create host entry: hostID as Host, target as HostName
	host := sshconfig.CreateHostEntry(hostID, target, user, port, usesPassword, targetFile)
	return host
}

// writeBatchEntry writes a host entry to config, backing up only once per file.
func writeBatchEntry(parser *sshconfig.Parser, mgr *vault.Manager, entry HostEntry, host *sshconfig.HostEntry, targetFile string, backedUp map[string]bool) error {
	// Backup only once per file
	if !backedUp[targetFile] {
		if err := createBackup(targetFile, getBackupDir(), getMaxBackups()); err != nil {
			// Log but don't fail
			ui.Warning("Backup failed for %s: %v", abbreviateHome(targetFile), err)
		}
		backedUp[targetFile] = true
	}

	// Parse target file
	cfg, err := parser.ParseFile(targetFile)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Insert host entry
	idx := sshconfig.FindInsertionIndex(cfg.Hosts, host.Host)
	cfg.Hosts = append(cfg.Hosts[:idx], append([]*sshconfig.HostEntry{host}, cfg.Hosts[idx:]...)...)

	if err := parser.WriteFile(cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Store password if provided
	if entry.Password != "" && mgr != nil {
		pwd := secret.NewFromString(entry.Password)
		if err := mgr.AddHostCredential(host.Host, host.User(), pwd); err != nil {
			ui.Warning("Failed to store password for %s: %v", host.Host, err)
		}
	}

	return nil
}

// findHostAnyConfig checks if a Host identifier exists in any SSH config file.
// Returns the file path if found, empty string otherwise.
func findHostAnyConfig(parser *sshconfig.Parser, hostID string) string {
	includes, _ := parser.FindIncludeFiles()
	for _, inc := range includes {
		cfg, err := parser.ParseFile(inc)
		if err != nil {
			continue
		}
		if sshconfig.FindHostByPattern(cfg.Hosts, hostID) != nil {
			return inc
		}
	}

	// Also check main config
	mainCfg, err := parser.ParseFile(parser.ConfigFile())
	if err == nil && sshconfig.FindHostByPattern(mainCfg.Hosts, hostID) != nil {
		return parser.ConfigFile()
	}

	return ""
}

// selectTargetFileForContext finds the target SSH config file for a context.
func selectTargetFileForContext(parser *sshconfig.Parser, contextName, hostname string) (string, *vault.ContextEntry, error) {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		// No vault - use first include or main config
		includes, _ := parser.FindIncludeFiles()
		if len(includes) > 0 {
			return includes[0], nil, nil
		}
		return parser.ConfigFile(), nil, nil
	}

	// Unlock vault if needed (spawner handles TTY/NSSH_PASSPHRASE)
	_ = clisession.TryUnlockIfTTY(mgr)

	contexts, _ := mgr.ListContexts()

	// If context specified, find its file
	if contextName != "" {
		for _, ctx := range contexts {
			if ctx.Name == contextName {
				includes, _ := parser.FindIncludeFiles()
				for _, inc := range includes {
					if filepath.Base(inc) == ctx.GitIncludeFile {
						return inc, &ctx, nil
					}
				}
				return "", nil, fmt.Errorf("context '%s' has no config file", contextName)
			}
		}
		return "", nil, fmt.Errorf("context '%s' not found", contextName)
	}

	// Try domain matching
	if hostname != "" {
		for _, ctx := range contexts {
			if ctx.Domain != "" && strings.HasSuffix(strings.ToLower(hostname), strings.ToLower(ctx.Domain)) {
				includes, _ := parser.FindIncludeFiles()
				for _, inc := range includes {
					if filepath.Base(inc) == ctx.GitIncludeFile {
						return inc, &ctx, nil
					}
				}
			}
		}
	}

	// Fall back to default context or first include
	defaultCtx := getDefaultContext()
	if defaultCtx != "" {
		for _, ctx := range contexts {
			if ctx.Name == defaultCtx {
				includes, _ := parser.FindIncludeFiles()
				for _, inc := range includes {
					if filepath.Base(inc) == ctx.GitIncludeFile {
						return inc, &ctx, nil
					}
				}
			}
		}
	}

	includes, _ := parser.FindIncludeFiles()
	if len(includes) > 0 {
		return includes[0], nil, nil
	}
	return parser.ConfigFile(), nil, nil
}

// groupEntriesByContext groups batch entries by their context field.
func groupEntriesByContext(entries []HostEntry) map[string][]HostEntry {
	result := make(map[string][]HostEntry)
	for _, e := range entries {
		result[e.Context] = append(result[e.Context], e)
	}
	return result
}

// selectTargetFile prompts user to select a config file for the new host.
// If hostname is provided, it will try to auto-select based on domain matching.
func selectTargetFile(parser *sshconfig.Parser, hostname string) (string, *vault.ContextEntry, error) {
	includes, err := parser.FindIncludeFiles()
	if err != nil {
		return "", nil, fmt.Errorf("find includes: %w", err)
	}

	if len(includes) == 0 {
		// No include files, use main config or create conf.d
		sshDir := filepath.Dir(parser.ConfigFile())
		defaultFile := filepath.Join(sshDir, "hosts")
		ui.Warning("No SSH config include files found")
		result, _ := ui.Confirm(fmt.Sprintf("Create new file at %s?", abbreviateHome(defaultFile)), true)
		if !result {
			return parser.ConfigFile(), nil, nil
		}
		return defaultFile, nil, nil
	}

	// Try to match with vault contexts
	mgr, err := clisession.NewManager(vault.Auto())
	var contexts []vault.ContextEntry
	if err == nil {
		// Unlock vault if needed (spawner handles TTY/NSSH_PASSPHRASE)
		_ = clisession.TryUnlockIfTTY(mgr)
		contexts, _ = mgr.ListContexts()
	}

	// Try to auto-select based on domain matching
	if hostname != "" && len(contexts) > 0 {
		for _, ctx := range contexts {
			if ctx.Domain != "" && strings.HasSuffix(strings.ToLower(hostname), strings.ToLower(ctx.Domain)) {
				// Find the include file for this context
				for _, inc := range includes {
					if filepath.Base(inc) == ctx.GitIncludeFile {
						ui.Info("Auto-selected context '%s' (domain: %s)", ctx.Name, ctx.Domain)
						return inc, &ctx, nil
					}
				}
			}
		}
	}

	// Build context name to file mapping
	contextToFile := make(map[string]string)
	fileToContext := make(map[string]*vault.ContextEntry)
	var contextNames []string

	for _, ctx := range contexts {
		for _, inc := range includes {
			if filepath.Base(inc) == ctx.GitIncludeFile {
				contextToFile[ctx.Name] = inc
				ctxCopy := ctx
				fileToContext[inc] = &ctxCopy
				contextNames = append(contextNames, ctx.Name)
				break
			}
		}
	}

	// Add non-context files as options too
	var fileLabels []string
	for _, inc := range includes {
		label := abbreviateHome(inc)
		if ctx, ok := fileToContext[inc]; ok {
			label = fmt.Sprintf("%s (context: %s)", ctx.Name, abbreviateHome(inc))
		}
		fileLabels = append(fileLabels, label)
	}

	var selectedIdx int

	// Use select menu if contexts available
	if len(contextNames) > 0 {
		// Build select options - put default context first if configured
		defaultCtx := getDefaultContext()
		options := make([]ui.SelectOption, 0, len(fileLabels))

		// Find default context index
		defaultIdx := -1
		for i, label := range fileLabels {
			if defaultCtx != "" && strings.HasPrefix(label, defaultCtx+" ") {
				defaultIdx = i
				break
			}
		}

		// Build options with default first
		if defaultIdx >= 0 {
			options = append(options, ui.SelectOption{Label: fileLabels[defaultIdx], Value: fileLabels[defaultIdx]})
		}
		for i, label := range fileLabels {
			if i != defaultIdx {
				options = append(options, ui.SelectOption{Label: label, Value: label})
			}
		}

		result, err := ui.Select("Context", options)
		if err != nil {
			return "", nil, fmt.Errorf("selection canceled")
		}

		// Find selected index in original fileLabels
		for i, label := range fileLabels {
			if result == label {
				selectedIdx = i
				break
			}
		}
	} else {
		// Build display options with context info
		options := make([]ui.FuzzySelectOption, len(includes))
		for i, inc := range includes {
			options[i] = ui.FuzzySelectOption{
				Label: abbreviateHome(inc),
				Value: inc,
			}
		}

		idx, err := ui.FuzzySelect("Select target SSH config file", options)
		if err != nil {
			return "", nil, err
		}
		if idx < 0 {
			return "", nil, fmt.Errorf("selection canceled")
		}
		selectedIdx = idx
	}

	selectedFile := includes[selectedIdx]

	// Find matching context
	var selectedContext *vault.ContextEntry
	for _, ctx := range contexts {
		if filepath.Base(selectedFile) == ctx.GitIncludeFile {
			selectedContext = &ctx
			break
		}
	}

	return selectedFile, selectedContext, nil
}

// requireContexts checks if any contexts are configured and returns an error with
// guidance if not. This ensures users set up contexts before adding hosts.
func requireContexts() error {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		// Can't check contexts, allow to proceed (might be first-time setup)
		return nil
	}

	// Unlock vault if needed
	_ = clisession.TryUnlockIfTTY(mgr)

	contexts, err := mgr.ListContexts()
	if err != nil {
		// Can't list contexts, allow to proceed
		return nil
	}

	if len(contexts) == 0 {
		ui.Error("No contexts configured")
		fmt.Println()
		ui.SubSection("Next Steps")
		steps := []string{
			"Create a context: nssh ctx add <name>",
			"Then add hosts: nssh host add <hostname>",
		}
		ui.NumberedList(steps)
		fmt.Println()
		return &exit.ExitError{Code: 1}
	}

	return nil
}
