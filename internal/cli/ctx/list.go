// Package ctx provides CLI commands for credential context management.
package ctx

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewListCmd creates the ctx list command.
func NewListCmd() *cobra.Command {
	var selectPattern string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all contexts",
		Long:  "List all credential contexts with their SSH config files and credentials.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(selectPattern)
		},
	}

	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "filter by regex pattern")

	return cmd
}

func runList(selectPattern string) error {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		ui.CommandStart("CONTEXTS")
		ui.Error("Failed to initialize vault: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	// Unlock vault if needed and TTY is available
	_ = clisession.TryUnlockIfTTY(mgr)

	contexts, err := mgr.ListContexts()
	if err != nil {
		ui.CommandStart("CONTEXTS")
		ui.Error("Failed to list contexts: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	ui.CommandStart("CONTEXTS")

	if len(contexts) == 0 {
		ui.WarningCentered("No contexts configured")
		ui.Info("Create one with: nssh ctx add NAME")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Apply regex filter if specified
	if selectPattern != "" {
		pattern, err := regexp.Compile("(?i)" + selectPattern)
		if err != nil {
			ui.Error("Invalid regex pattern: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		var filtered []vault.ContextEntry
		for _, ctx := range contexts {
			username := ""
			if ctx.Credential != nil {
				username = ctx.Credential.Username
			}
			if pattern.MatchString(ctx.Name) ||
				pattern.MatchString(ctx.GitIncludeFile) ||
				pattern.MatchString(username) {
				filtered = append(filtered, ctx)
			}
		}
		contexts = filtered

		if len(contexts) == 0 {
			ui.WarningCentered("No contexts matching pattern: %s", selectPattern)
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}
	}

	table := ui.NewTable("Context", "SSH Config File", "Hosts", "Domain", "Credential")

	for _, ctx := range contexts {
		includePath := displayIncludePath(ctx.GitIncludeFile)
		hostCount := countHostsInFile(ctx.GitIncludeFile)
		hostCountStr := "-"
		if hostCount > 0 {
			hostCountStr = fmt.Sprintf("%d", hostCount)
		}
		domain := ctx.Domain
		if domain == "" {
			domain = "-"
		}
		username := "-"
		if ctx.Credential != nil {
			username = ctx.Credential.Username
		}

		table.AddRow(ctx.Name, includePath, hostCountStr, domain, username)
	}

	margin := table.LeftMargin()
	if selectPattern != "" {
		ui.InfoWithMargin(margin, "Filter: %s", selectPattern)
	}
	table.Render()
	ui.InfoWithMargin(margin, "Total: %d contexts", len(contexts))

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// displayIncludePath formats an include file for display.
func displayIncludePath(includeFile string) string {
	if includeFile == "" {
		return "-"
	}
	return abbreviateHome(sshConfigPath(includeFile))
}

// abbreviateHome replaces home directory with ~
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}

	return path
}
