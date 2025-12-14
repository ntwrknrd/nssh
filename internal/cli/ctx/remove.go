package ctx

import (
	"fmt"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewRemoveCmd creates the ctx remove command.
func NewRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "remove [name]",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove context",
		Long:    "Remove a credential context. Shows dependent hosts and warns before removal.",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return runRemove(name, force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation")

	return cmd
}

func runRemove(name string, force bool) error {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return err
	}

	// Unlock vault if needed and TTY is available
	_ = clisession.TryUnlockIfTTY(mgr)

	ui.CommandStart("REMOVE CONTEXT")

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

		finalName, err = ui.Select("Select context to delete", options)
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	// Check if context exists
	ctx, err := mgr.GetContext(finalName)
	if err != nil {
		ui.Error("Failed to get context: %s", err)
		ui.CommandEnd(ui.StatusError)
		return nil
	}
	if ctx == nil {
		ui.Error("Context '%s' not found", finalName)
		ui.CommandEnd(ui.StatusError)
		return nil
	}

	// Show context info
	hostCount := countHostsInFile(ctx.GitIncludeFile)
	hostCountStr := ui.Gray("(none)")
	if hostCount > 0 {
		hostCountStr = fmt.Sprintf("%d", hostCount)
	}
	domain := "-"
	if ctx.Domain != "" {
		domain = ctx.Domain
	}
	credential := "-"
	if ctx.Credential != nil {
		credential = ctx.Credential.Username
	}

	ui.PrintKeyValue("Context", finalName)
	ui.PrintKeyValue("SSH Config File", displayIncludePath(ctx.GitIncludeFile))
	ui.PrintKeyValue("Hosts", hostCountStr)
	ui.PrintKeyValue("Domain", domain)
	ui.PrintKeyValue("Credential", credential)

	// Confirm deletion
	if !force {
		confirmed, err := ui.Confirm(fmt.Sprintf("Delete context '%s'?", finalName), false)
		if err != nil || !confirmed {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}
	}

	sshConfigFile := ctx.GitIncludeFile

	success, err := mgr.DeleteContext(finalName)
	if err != nil {
		ui.Error("Failed to delete context: %s", err)
		ui.CommandEnd(ui.StatusError)
		return nil
	}

	if !success {
		ui.Error("Context '%s' not found", finalName)
		ui.CommandEnd(ui.StatusError)
		return nil
	}

	ui.Success("Context '%s' deleted", finalName)

	// Offer to delete the SSH config file
	if !force {
		deleteFilePrompt := fmt.Sprintf("Delete SSH config file '%s'?", sshConfigFile)
		if hostCount > 0 {
			deleteFilePrompt = fmt.Sprintf("Delete SSH config file '%s'? (%d hosts will be removed)", sshConfigFile, hostCount)
		}

		deleteFile, err := ui.Confirm(deleteFilePrompt, false)
		if err == nil && deleteFile {
			if err := deleteSSHConfigFile(sshConfigFile); err != nil {
				ui.Warning("Failed to delete SSH config file: %s", err)
			} else {
				ui.Success("SSH config file deleted")
			}
		} else {
			ui.Info("SSH config file retained: %s", sshConfigPath(sshConfigFile))
		}
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
