package host

import (
	"path/filepath"
	"regexp"
	"sort"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewListCmd creates the host list command.
func NewListCmd() *cobra.Command {
	var selectPattern string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all hosts",
		Long:  "List all SSH hosts from config files with their connection details.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(selectPattern)
		},
	}

	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "filter by regex pattern")

	return cmd
}

func runList(selectPattern string) error {
	parser := getParser()

	hosts, err := parser.GetAllHosts()
	if err != nil {
		ui.CommandStart("SSH HOSTS")
		ui.Error("Failed to get hosts: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	ui.CommandStart("SSH HOSTS")

	if len(hosts) == 0 {
		ui.WarningCentered("No hosts configured")
		ui.Info("Add one with: nssh host add HOSTNAME")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	// Sort hosts alphabetically
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Host < hosts[j].Host
	})

	// Build context map and get hosts with custom credentials
	contextMap := make(map[string]string)
	hostsWithCreds := make(map[string]bool)
	if mgr, err := clisession.NewManager(vault.Auto()); err == nil {
		// Unlock vault if needed and TTY is available
		_ = clisession.TryUnlockIfTTY(mgr)
		if contexts, err := mgr.ListContexts(); err == nil {
			for _, ctx := range contexts {
				base := filepath.Base(ctx.GitIncludeFile)
				contextMap[base] = ctx.Name
			}
		}
		if hostCreds, err := mgr.ListHostsWithCredentials(); err == nil {
			hostsWithCreds = hostCreds
		}
	}

	// Apply regex filter if specified
	if selectPattern != "" {
		pattern, err := regexp.Compile("(?i)" + selectPattern)
		if err != nil {
			ui.Error("Invalid regex pattern: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		var filtered []*sshconfig.HostEntry
		for _, host := range hosts {
			ctxName := contextMap[filepath.Base(host.SourceFile)]
			credSource := "[C]"
			if hostsWithCreds[host.Host] {
				credSource = "[H]"
			}
			hostID := sshconfig.DeriveHostID(host.Host)
			if pattern.MatchString(host.Host) ||
				pattern.MatchString(host.HostName) ||
				pattern.MatchString(hostID) ||
				pattern.MatchString(host.User()) ||
				pattern.MatchString(ctxName) ||
				pattern.MatchString(credSource) {
				filtered = append(filtered, host)
			}
		}
		hosts = filtered

		if len(hosts) == 0 {
			ui.WarningCentered("No hosts matching pattern: %s", selectPattern)
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}
	}

	table := ui.NewTable("Host", "HostName", "User", "Port", "Auth", "Cred", "Context")

	for _, host := range hosts {
		user := host.User()
		if user == "" {
			user = "-"
		}

		ctxName := contextMap[filepath.Base(host.SourceFile)]
		if ctxName == "" {
			ctxName = "-"
		}

		// Credential source: [H] = host-specific, [C] = context
		credSource := "[C]"
		if hostsWithCreds[host.Host] {
			credSource = "[H]"
		}

		// HostName column: show "-" if no explicit HostName was set (Host == HostName)
		hostName := host.HostName
		if host.Host == host.HostName {
			hostName = "-"
		}

		table.AddRow(
			host.Host,
			hostName,
			user,
			host.Port(),
			authTypeString(host),
			credSource,
			ctxName,
		)
	}

	margin := table.LeftMargin()
	if selectPattern != "" {
		ui.InfoWithMargin(margin, "Filter: %s", selectPattern)
	}
	table.Render()
	ui.InfoWithMargin(margin, "Total: %d hosts", len(hosts))

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
