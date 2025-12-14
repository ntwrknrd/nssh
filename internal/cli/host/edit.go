package host

import (
	"fmt"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewEditCmd creates the host edit command.
func NewEditCmd() *cobra.Command {
	var (
		yes      bool
		dryRun   bool
		authType string
	)

	cmd := &cobra.Command{
		Use:   "edit [HOST]",
		Short: "Edit host settings",
		Long: `Edit an SSH host's configuration or authentication settings.

If HOST is not provided, interactive selection is used.
Supports partial matching (e.g., "switch" matches "switch.example.com").`,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runEdit(query, yes, dryRun, authType)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only")
	cmd.Flags().StringVar(&authType, "auth", "", "change auth type: password or key")

	return cmd
}

func runEdit(query string, yes, dryRun bool, authType string) error {
	parser := getParser()

	ui.CommandStart("EDIT HOST")

	// Initialize vault and unlock if needed
	vm, _ := clisession.NewManager(vault.Auto())
	if vm != nil {
		if err := clisession.TryUnlockIfTTY(vm); err != nil {
			ui.Error("Failed to unlock vault: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}
	}

	// If no query provided, list hosts for selection
	if query == "" {
		hosts, err := parser.GetAllHosts()
		if err != nil {
			ui.Error("Failed to get hosts: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		if len(hosts) == 0 {
			ui.Warning("No hosts configured")
			ui.Info("Add one with: nssh host add HOSTNAME")
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

		idx, err := ui.FuzzySelect("Select host to edit", options)
		if err != nil || idx < 0 {
			ui.Abort("Selection canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		query = hosts[idx].Host
	}

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

	// Show current config (same format as host get)
	displayHostDetails(host.Host, host.Lines, cfg.Path, vm, false)

	modified := false

	// Handle auth type change
	if authType != "" {
		if authType != "password" && authType != "key" {
			ui.Error("Invalid auth type: %s (use 'password' or 'key')", authType)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		newUsesPassword := authType == "password"
		if newUsesPassword != host.UsesPassword() {
			if dryRun {
				ui.Warning("Would change auth from %s to %s", authTypeString(host), authType)
			} else {
				updateHostAuth(host, newUsesPassword)
				modified = true
				ui.Success("Changed auth to %s", authType)

				// After auth change, test connection and apply compat fixes
				if !yes {
					runCompatFix, _ := ui.Confirm("Test connection and apply compatibility fixes?", true)
					if runCompatFix {
						result := IterativeCompatFix(parser, cfg, host, 5, true)
						DisplayCompatResult(result, host.Host)
						if !result.Success && result.TestResult != nil {
							debugFile := WriteDebugFile(host.Host, result.TestResult,
								fmt.Sprintf("Auth changed to: %s\nStopped reason: %s", authType, result.StoppedReason))
							if debugFile != "" {
								ui.Info("Debug info: %s", debugFile)
							}
						}
					}
				}
			}
		} else {
			ui.Noop("Auth type already set to %s", authType)
		}
	}

	// Interactive editing if no specific changes requested
	if authType == "" && !yes {
		ui.SubSection("Edit Options")
		menuOptions := []ui.SelectOption{
			{Label: "Manage SSH config", Value: "ssh_config"},
			{Label: "Manage credentials", Value: "credentials"},
			{Label: "Cancel", Value: "cancel"},
		}

		choice, err := ui.Select("Action", menuOptions)
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}

		switch choice {
		case "ssh_config":
			sshConfigOptions := []ui.SelectOption{
				{Label: "Host", Value: "host"},
				{Label: "HostName", Value: "address"},
				{Label: "Port", Value: "port"},
				{Label: "User", Value: "user"},
				{Label: "Authentication", Value: "auth"},
				{Label: "Back", Value: "back"},
			}

			sshChoice, err := ui.Select("SSH config option", sshConfigOptions)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}

			switch sshChoice {
			case "host":
				input, err := ui.InputWithDefault("Host", host.Host)
				if err != nil {
					ui.Abort("Canceled")
					ui.CommandEnd(ui.StatusAbort)
					return nil
				}
				if input != "" && input != host.Host {
					if dryRun {
						ui.Warning("Would change Host to %s", input)
					} else {
						// Update the Host line
						for i, line := range host.Lines {
							trimmed := strings.TrimSpace(line)
							if strings.HasPrefix(strings.ToLower(trimmed), "host ") {
								host.Lines[i] = fmt.Sprintf("Host %s\n", input)
								break
							}
						}
						oldHost := host.Host
						host.Host = input
						modified = true
						ui.Success("Changed Host from %s to %s", oldHost, input)
					}
				}

			case "address":
				currentAddr := host.HostName
				if currentAddr == host.Host {
					currentAddr = "" // No override set
				}
				input, err := ui.InputWithDefault("HostName (leave empty to use Host)", currentAddr)
				if err != nil {
					ui.Abort("Canceled")
					ui.CommandEnd(ui.StatusAbort)
					return nil
				}
				if input == "" && currentAddr != "" {
					if dryRun {
						ui.Warning("Would remove HostName")
					} else {
						removeHostProperty(host, "hostname")
						modified = true
						ui.Success("Removed HostName")
					}
				} else if input != "" {
					if dryRun {
						if input != currentAddr {
							ui.Warning("Would set HostName to %s", input)
						}
					} else if updateHostProperty(host, "hostname", input) {
						modified = true
						ui.Success("Set HostName to %s", input)
					}
				}

			case "user":
				input, err := ui.InputWithDefault("User", host.User())
				if err != nil {
					ui.Abort("Canceled")
					ui.CommandEnd(ui.StatusAbort)
					return nil
				}
				if input != "" {
					if dryRun {
						if input != host.User() {
							ui.Warning("Would change User to %s", input)
						}
					} else if updateHostProperty(host, "user", input) {
						modified = true
						ui.Success("Changed User to %s", input)
					}
				}

			case "port":
				input, err := ui.InputWithDefault("Port", host.Port())
				if err != nil {
					ui.Abort("Canceled")
					ui.CommandEnd(ui.StatusAbort)
					return nil
				}
				if input != "" {
					if dryRun {
						if input != host.Port() {
							ui.Warning("Would change Port to %s", input)
						}
					} else if updateHostProperty(host, "port", input) {
						modified = true
						ui.Success("Changed Port to %s", input)
					}
				}

			case "auth":
				newAuth := "key"
				if !host.UsesPassword() {
					newAuth = "password"
				}
				confirm, _ := ui.Confirm(fmt.Sprintf("Change auth to %s?", newAuth), true)
				if confirm {
					if dryRun {
						ui.Warning("Would change auth to %s", newAuth)
					} else {
						updateHostAuth(host, newAuth == "password")
						modified = true
						ui.Success("Changed auth to %s", newAuth)
					}
				}

			case "back":
				// Return to main menu - but we're not in a loop, so just fall through
			}

		case "credentials":
			if err := manageHostCredentials(host, cfg, parser, dryRun); err != nil {
				ui.Error("Credential management failed: %s", err)
				ui.CommandEnd(ui.StatusError)
				return &exit.ExitError{Code: 1}
			}
			ui.CommandEnd(ui.StatusSuccess)
			return nil

		case "cancel":
			ui.Noop("No changes made")
			ui.CommandEnd(ui.StatusNoop)
			return nil
		}
	}

	if !modified {
		ui.Noop("No changes made")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	if dryRun {
		ui.Warning("Run without --dry-run to apply changes")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Create backup
	if err := createBackup(cfg.Path, getBackupDir(), getMaxBackups()); err != nil {
		ui.Warning("Backup failed: %v", err)
	}

	// Write config
	if err := parser.WriteFile(cfg); err != nil {
		ui.Error("Failed to write config: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	ui.Info("Updated %s in %s", host.Host, abbreviateHome(cfg.Path))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// updateHostAuth updates the authentication lines in a host entry.
func updateHostAuth(host *sshconfig.HostEntry, usesPassword bool) {
	// Remove existing auth-related lines
	var newLines []string
	for _, line := range host.Lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "pubkeyauthentication") ||
			strings.HasPrefix(lower, "passwordauthentication") ||
			strings.HasPrefix(lower, "preferredauthentications") {
			continue
		}
		newLines = append(newLines, line)
	}

	// Find insertion point (after Host line)
	insertIdx := 1
	for i, line := range newLines {
		if i == 0 {
			continue
		}
		if strings.TrimSpace(line) == "" {
			insertIdx = i
			break
		}
		insertIdx = i + 1
	}

	// Add new auth lines
	var authLines []string
	if usesPassword {
		authLines = []string{
			"  PubkeyAuthentication no\n",
			"  PreferredAuthentications keyboard-interactive,password\n",
		}
		host.Properties["pubkeyauthentication"] = "no"
		host.Properties["preferredauthentications"] = "keyboard-interactive,password"
		delete(host.Properties, "passwordauthentication")
	} else {
		authLines = []string{
			"  PubkeyAuthentication yes\n",
			"  PasswordAuthentication no\n",
		}
		host.Properties["pubkeyauthentication"] = "yes"
		host.Properties["passwordauthentication"] = "no"
		delete(host.Properties, "preferredauthentications")
	}

	// Insert auth lines
	result := make([]string, 0, len(newLines)+len(authLines))
	result = append(result, newLines[:insertIdx]...)
	result = append(result, authLines...)
	result = append(result, newLines[insertIdx:]...)
	host.Lines = result
}

// updateHostProperty updates or adds a property in a host entry.
// Returns true if the line was actually modified (value or formatting changed).
func updateHostProperty(host *sshconfig.HostEntry, key, value string) bool {
	lowerKey := strings.ToLower(key)
	found := false
	changed := false
	newLine := fmt.Sprintf("  %s %s\n", capitalize(key), value)

	// Update existing line
	for i, line := range host.Lines {
		if i == 0 {
			continue // Skip Host line
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), lowerKey+" ") ||
			strings.HasPrefix(strings.ToLower(trimmed), lowerKey+"\t") {
			if host.Lines[i] != newLine {
				host.Lines[i] = newLine
				changed = true
			}
			found = true
			break
		}
	}

	// Add new line if not found
	if !found {
		// Insert after Host line
		host.Lines = append(host.Lines[:1], append([]string{newLine}, host.Lines[1:]...)...)
		changed = true
	}

	host.Properties[lowerKey] = value
	return changed
}

// removeHostProperty removes a property from a host entry.
func removeHostProperty(host *sshconfig.HostEntry, key string) {
	lowerKey := strings.ToLower(key)

	// Remove matching line
	var newLines []string
	for i, line := range host.Lines {
		if i == 0 {
			newLines = append(newLines, line) // Keep Host line
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), lowerKey+" ") ||
			strings.HasPrefix(strings.ToLower(trimmed), lowerKey+"\t") {
			continue // Skip this line (remove it)
		}
		newLines = append(newLines, line)
	}
	host.Lines = newLines

	delete(host.Properties, lowerKey)
}

// capitalize returns the key with first letter uppercase.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// manageHostCredentials provides a submenu for credential CRUD operations.
func manageHostCredentials(host *sshconfig.HostEntry, cfg *sshconfig.ParsedConfig, parser *sshconfig.Parser, dryRun bool) error {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fmt.Errorf("vault init: %w", err)
	}

	// Unlock vault if needed and TTY is available
	if err := clisession.TryUnlockIfTTY(mgr); err != nil {
		return fmt.Errorf("vault unlock: %w", err)
	}

	for {
		// Get current credentials
		creds, err := mgr.GetHostCredentials(host.Host)
		if err != nil {
			return fmt.Errorf("get credentials: %w", err)
		}

		// Show submenu options
		menuOptions := []ui.SelectOption{
			{Label: "Add credential", Value: "add"},
			{Label: "Update credential", Value: "update"},
			{Label: "Remove credential", Value: "remove"},
			{Label: "Back", Value: "back"},
		}

		choice, err := ui.Select("Action", menuOptions)
		if err != nil {
			return nil // User canceled
		}

		switch choice {
		case "add": // Add credential
			if dryRun {
				ui.Warning("Would prompt for new credential (dry-run)")
				continue
			}

			username, err := ui.Input("Username", "")
			if err != nil {
				ui.Abort("Canceled")
				continue
			}
			if username == "" {
				ui.Warning("Username cannot be empty")
				continue
			}

			// Check for duplicate username
			duplicate := false
			for _, cred := range creds {
				if cred.Username == username {
					ui.Warning("Credential for '%s' already exists", username)
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}

			password, err := ui.PasswordWithConfirm("Password")
			if err != nil {
				ui.Error("Password error: %s", err)
				continue
			}

			sec := newSecretFromString(password)
			if err := mgr.AddHostCredential(host.Host, username, sec); err != nil {
				ui.Error("Failed to add credential: %s", err)
				continue
			}

			ui.Success("Credential added for '%s'", username)

			// Auto-set auth type to password if not already
			if !host.UsesPassword() {
				confirm, _ := ui.Confirm("Set authentication type to 'password'?", true)
				if confirm {
					updateHostAuth(host, true)
					// Write the SSH config change
					if err := createBackup(cfg.Path, getBackupDir(), getMaxBackups()); err != nil {
						ui.Warning("Backup failed: %v", err)
					}
					if err := parser.WriteFile(cfg); err != nil {
						ui.Warning("Failed to save auth change: %s", err)
					} else {
						ui.Success("Auth type set to 'password'")
					}
				}
			}
			return nil

		case "update": // Update credential
			if len(creds) == 0 {
				ui.Warning("No credentials to update")
				continue
			}

			if dryRun {
				ui.Warning("Would prompt to update credential (dry-run)")
				continue
			}

			// Build selection options
			options := make([]ui.SelectOption, len(creds))
			for i, cred := range creds {
				label := cred.Username
				if cred.Default {
					label += " [default]"
				}
				options[i] = ui.SelectOption{
					Label: label,
					Value: cred.Username,
				}
			}

			username, err := ui.Select("Select credential to update", options)
			if err != nil {
				ui.Abort("Canceled")
				continue
			}

			// Find the selected credential
			var selectedCred *vault.Credential
			for i := range creds {
				if creds[i].Username == username {
					selectedCred = &creds[i]
					break
				}
			}
			if selectedCred == nil {
				ui.Warning("Credential not found")
				continue
			}

			// Show update submenu
			updateOptions := []ui.SelectOption{
				{Label: "Update username", Value: "username"},
				{Label: "Update password", Value: "password"},
				{Label: "Set as default", Value: "default"},
				{Label: "Back", Value: "back"},
			}

			updateChoice, err := ui.Select("What to update", updateOptions)
			if err != nil {
				ui.Abort("Canceled")
				continue
			}

			switch updateChoice {
			case "username":
				newUsername, err := ui.Input("New username", username)
				if err != nil {
					ui.Abort("Canceled")
					continue
				}
				if newUsername == "" {
					ui.Warning("Username cannot be empty")
					continue
				}
				if newUsername == username {
					ui.Warning("Username unchanged")
					continue
				}

				// Check for duplicate
				for _, cred := range creds {
					if cred.Username == newUsername {
						ui.Warning("Credential for '%s' already exists", newUsername)
						continue
					}
				}

				// Get current password and recreate with new username
				sec := newSecretFromString(selectedCred.Password)
				wasDefault := selectedCred.Default

				if _, err := mgr.RemoveHostCredential(host.Host, username); err != nil {
					ui.Error("Failed to update username: %s", err)
					continue
				}
				if err := mgr.AddHostCredential(host.Host, newUsername, sec); err != nil {
					ui.Error("Failed to update username: %s", err)
					continue
				}
				// Restore default status if it was default
				if wasDefault {
					_ = mgr.SetHostDefaultCredential(host.Host, newUsername)
				}
				ui.Success("Username updated to '%s'", newUsername)

			case "password":
				password, err := ui.PasswordWithConfirm("New password")
				if err != nil {
					ui.Error("Password error: %s", err)
					continue
				}

				sec := newSecretFromString(password)
				updated, err := mgr.UpdateHostCredential(host.Host, username, sec)
				if err != nil {
					ui.Error("Failed to update password: %s", err)
					continue
				}
				if updated {
					ui.Success("Password updated for '%s'", username)
				} else {
					ui.Warning("Credential not found")
				}

			case "default":
				if err := mgr.SetHostDefaultCredential(host.Host, username); err != nil {
					ui.Error("Failed to set default: %s", err)
					continue
				}
				ui.Success("Default credential set to '%s'", username)

			case "back":
				continue
			}

		case "remove": // Remove credential
			if len(creds) == 0 {
				ui.Warning("No credentials to remove")
				continue
			}

			if dryRun {
				ui.Warning("Would prompt to remove credential (dry-run)")
				continue
			}

			// Build selection options
			options := make([]ui.SelectOption, len(creds))
			for i, cred := range creds {
				options[i] = ui.SelectOption{
					Label: cred.Username,
					Value: cred.Username,
				}
			}

			username, err := ui.Select("Select credential to remove", options)
			if err != nil {
				ui.Abort("Canceled")
				continue
			}

			confirm, _ := ui.Confirm(fmt.Sprintf("Remove credential for '%s'?", username), false)
			if !confirm {
				ui.Abort("Canceled")
				continue
			}

			removed, err := mgr.RemoveHostCredential(host.Host, username)
			if err != nil {
				ui.Error("Failed to remove credential: %s", err)
				continue
			}
			if removed {
				ui.Success("Credential removed for '%s'", username)
				return nil
			} else {
				ui.Warning("Credential not found")
			}

		case "back":
			return nil // Back to main menu
		}
	}
}
