package ctx

import (
	"fmt"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewEditCmd creates the ctx edit command.
func NewEditCmd() *cobra.Command {
	var (
		domain    string
		sshConfig string
		username  string
	)

	cmd := &cobra.Command{
		Use:   "edit [name]",
		Short: "Edit context settings",
		Long: `Edit context credentials and settings.

Sets or replaces the fallback username, domain, and SSH config file.
Use flags for non-interactive updates (empty string clears domain).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}

			// Determine if any flags were explicitly set
			domainSet := cmd.Flags().Changed("domain")
			sshConfigSet := cmd.Flags().Changed("ssh-config")
			usernameSet := cmd.Flags().Changed("username")

			return runEdit(name, domain, sshConfig, username,
				domainSet, sshConfigSet, usernameSet)
		},
	}

	cmd.Flags().StringVarP(&domain, "domain", "d", "", "set domain suffix for auto-selection")
	cmd.Flags().StringVarP(&sshConfig, "ssh-config", "s", "", "set SSH config include file")
	cmd.Flags().StringVarP(&username, "username", "u", "", "set fallback username (password prompted securely)")

	return cmd
}

func runEdit(name, domain, sshConfig, username string,
	domainSet, sshConfigSet, usernameSet bool) error {

	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return err
	}

	// Unlock vault if needed and TTY is available
	_ = clisession.TryUnlockIfTTY(mgr)

	// Interactive mode if no flags provided
	nonInteractive := domainSet || sshConfigSet || usernameSet

	// Show banner first
	ui.CommandStart("EDIT CONTEXT")

	// Get context name
	var finalName string
	if name != "" {
		finalName = name
	} else {
		// List available contexts for selection
		contexts, err := mgr.ListContexts()
		if err != nil {
			ui.Error("Failed to list contexts: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		if len(contexts) == 0 {
			ui.Warning("No contexts configured")
			ui.Info("Create one with: nssh ctx add")
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}

		// Build options for select
		options := make([]ui.SelectOption, len(contexts))
		for i, ctx := range contexts {
			label := ctx.Name
			if ctx.Domain != "" {
				label = fmt.Sprintf("%s (%s)", ctx.Name, ctx.Domain)
			}
			options[i] = ui.SelectOption{Label: label, Value: ctx.Name}
		}

		finalName, err = ui.Select("Select context", options)
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	// Get context
	ctx, err := mgr.GetContext(finalName)
	if err != nil {
		ui.Error("Failed to get context: %s", err)
		ui.CommandEnd(ui.StatusError)
		return nil
	}
	if ctx == nil {
		ui.Error("Context '%s' not found", finalName)
		ui.Info("Create it with: nssh ctx add %s", finalName)
		ui.CommandEnd(ui.StatusError)
		return nil
	}

	if nonInteractive {
		return runEditNonInteractive(mgr, finalName, ctx, domain, sshConfig, username,
			domainSet, sshConfigSet, usernameSet)
	}

	return runEditInteractive(mgr, finalName, ctx)
}

func runEditNonInteractive(mgr *vault.Manager, name string, ctx *vault.ContextEntry,
	domain, sshConfig, username string,
	domainSet, sshConfigSet, usernameSet bool) error {

	// Update domain if specified
	if domainSet && domain != ctx.Domain {
		if err := mgr.UpdateContextDomain(name, domain); err != nil {
			ui.Error("Failed to update domain: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		if domain != "" {
			ui.Success("Domain set to '%s'", domain)
		} else {
			ui.Warning("Domain cleared")
		}
	}

	// Update SSH config file if specified
	if sshConfigSet && sshConfig != ctx.GitIncludeFile {
		if sshConfig == "" {
			ui.Error("SSH config file cannot be empty")
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		if err := mgr.UpdateContextIncludeFile(name, sshConfig); err != nil {
			ui.Error("Failed to update SSH config: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		ui.Success("SSH config set to '%s'", sshConfig)
	}

	// Update credential if username specified
	if usernameSet {
		if username == "" {
			ui.Error("Username cannot be empty")
			ui.CommandEnd(ui.StatusError)
			return nil
		}

		// Prompt for password securely
		password, err := ui.PasswordWithConfirm("Password")
		if err != nil {
			ui.Error("Password error: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}

		sec := newSecretFromString(password)
		if err := mgr.AddContextCredential(name, username, sec, true); err != nil {
			ui.Error("Failed to update credential: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		ui.Success("Credential set for '%s'", username)
	}

	ui.Success("Context '%s' updated", name)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func runEditInteractive(mgr *vault.Manager, name string, ctx *vault.ContextEntry) error {
	// Show current configuration (skip newline - CommandStart already added one)
	ui.SubSection("Current Configuration", true)
	showContextConfig(ctx)

	// Main menu
	menuOptions := []ui.SelectOption{
		{Label: "Edit SSH config path", Value: "ssh_config"},
		{Label: "Edit domain", Value: "domain"},
		{Label: "Edit credential", Value: "credential"},
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
		// Get files used by other contexts (exclude current)
		contexts, _ := mgr.ListContexts()
		usedFiles := make([]string, 0, len(contexts))
		for _, c := range contexts {
			if c.Name != name {
				usedFiles = append(usedFiles, c.GitIncludeFile)
			}
		}

		newConfig, err := selectSSHConfigFile(ctx.GitIncludeFile, usedFiles...)
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
		if newConfig != ctx.GitIncludeFile {
			if err := mgr.UpdateContextIncludeFile(name, newConfig); err != nil {
				ui.Error("Failed to update SSH config: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			ui.Success("SSH config set to '%s'", newConfig)
			ui.CommandEnd(ui.StatusSuccess)
		} else {
			ui.Noop("No changes made")
			ui.CommandEnd(ui.StatusNoop)
		}

	case "domain":
		editOptions := []ui.SelectOption{
			{Label: "Set domain", Value: "set"},
		}
		if ctx.Domain != "" {
			editOptions = append(editOptions, ui.SelectOption{Label: "Clear domain", Value: "clear"})
		}
		editOptions = append(editOptions, ui.SelectOption{Label: "Back", Value: "back"})

		editChoice, err := ui.Select("Domain option", editOptions)
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}

		switch editChoice {
		case "set":
			defaultVal := ""
			if ctx.Domain != "" {
				defaultVal = ctx.Domain
			}
			newDomain, err := ui.InputWithDefault("Domain", defaultVal)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if newDomain != "" && newDomain != ctx.Domain {
				if err := mgr.UpdateContextDomain(name, newDomain); err != nil {
					ui.Error("Failed to update domain: %s", err)
					ui.CommandEnd(ui.StatusError)
					return nil
				}
				ui.Success("Domain set to '%s'", newDomain)
				ui.CommandEnd(ui.StatusSuccess)
			} else {
				ui.Noop("No changes made")
				ui.CommandEnd(ui.StatusNoop)
			}
		case "clear":
			if err := mgr.UpdateContextDomain(name, ""); err != nil {
				ui.Error("Failed to clear domain: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			ui.Success("Domain cleared")
			ui.CommandEnd(ui.StatusSuccess)
		case "back":
			ui.Noop("No changes made")
			ui.CommandEnd(ui.StatusNoop)
		}

	case "credential":
		editOptions := []ui.SelectOption{}
		if ctx.Credential != nil {
			editOptions = append(editOptions,
				ui.SelectOption{Label: "Edit username", Value: "username"},
				ui.SelectOption{Label: "Edit password", Value: "password"},
				ui.SelectOption{Label: "Remove credential", Value: "remove"},
			)
		} else {
			editOptions = append(editOptions,
				ui.SelectOption{Label: "Add credential", Value: "add"},
			)
		}
		editOptions = append(editOptions, ui.SelectOption{Label: "Back", Value: "back"})

		editChoice, err := ui.Select("Credential option", editOptions)
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}

		switch editChoice {
		case "add":
			username, err := ui.Input("Username", "")
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if username == "" {
				ui.Warning("Username cannot be empty")
				ui.CommandEnd(ui.StatusWarning)
				return nil
			}
			password, err := ui.PasswordWithConfirm("Password")
			if err != nil {
				ui.Error("Password error: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			sec := newSecretFromString(password)
			if err := mgr.AddContextCredential(name, username, sec, true); err != nil {
				ui.Error("Failed to add credential: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			ui.Success("Credential added for '%s'", username)
			ui.CommandEnd(ui.StatusSuccess)

		case "username":
			newUsername, err := ui.InputWithDefault("Username", ctx.Credential.Username)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if newUsername == "" {
				ui.Warning("Username cannot be empty")
				ui.CommandEnd(ui.StatusWarning)
				return nil
			}
			if newUsername != ctx.Credential.Username {
				// Keep existing password, just update username
				sec := newSecretFromString(ctx.Credential.Password)
				if err := mgr.AddContextCredential(name, newUsername, sec, true); err != nil {
					ui.Error("Failed to update username: %s", err)
					ui.CommandEnd(ui.StatusError)
					return nil
				}
				ui.Success("Username updated to '%s'", newUsername)
				ui.CommandEnd(ui.StatusSuccess)
			} else {
				ui.Noop("No changes made")
				ui.CommandEnd(ui.StatusNoop)
			}

		case "password":
			password, err := ui.PasswordWithConfirm("New password")
			if err != nil {
				ui.Error("Password error: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			sec := newSecretFromString(password)
			if err := mgr.AddContextCredential(name, ctx.Credential.Username, sec, true); err != nil {
				ui.Error("Failed to update password: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			ui.Success("Password updated for '%s'", ctx.Credential.Username)
			ui.CommandEnd(ui.StatusSuccess)

		case "remove":
			confirm, _ := ui.Confirm("Remove credential?", false)
			if !confirm {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if err := mgr.RemoveContextCredential(name); err != nil {
				ui.Error("Failed to remove credential: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
			ui.Success("Credential removed")
			ui.CommandEnd(ui.StatusSuccess)

		case "back":
			ui.Noop("No changes made")
			ui.CommandEnd(ui.StatusNoop)
		}

	case "cancel":
		ui.Noop("No changes made")
		ui.CommandEnd(ui.StatusNoop)
	}

	return nil
}

func showContextConfig(ctx *vault.ContextEntry) {
	var configLines []string
	configLines = append(configLines, fmt.Sprintf("SSH Config:  %s", ctx.GitIncludeFile))
	hostCount := countHostsInFile(ctx.GitIncludeFile)
	if hostCount > 0 {
		configLines = append(configLines, fmt.Sprintf("Hosts:       %d", hostCount))
	} else {
		configLines = append(configLines, fmt.Sprintf("Hosts:       %s", ui.Gray("(none)")))
	}
	if ctx.Domain != "" {
		configLines = append(configLines, fmt.Sprintf("Domain:      %s", ctx.Domain))
	} else {
		configLines = append(configLines, fmt.Sprintf("Domain:      %s", ui.Gray("(not set)")))
	}
	if ctx.Credential != nil {
		configLines = append(configLines, fmt.Sprintf("Credential:  %s", ctx.Credential.Username))
	} else {
		configLines = append(configLines, fmt.Sprintf("Credential:  %s", ui.Gray("(not set)")))
	}
	ui.Box("", strings.Join(configLines, "\n"))
}
