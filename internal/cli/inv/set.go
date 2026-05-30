package inv

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var groupNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

func newSetCmd() *cobra.Command {
	var group string
	var hostname string
	var user string
	var port int
	var credentialProvider string
	var credentialRef string
	var credentialUsername string
	var credentialUsernameRef string
	var credentialClear bool
	cmd := &cobra.Command{
		Use:   "set HOST",
		Short: "Create or update inventory",
		Long:  "Create or update a managed host, group, or host auth override.",
		Args:  cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh inv set HOST\nnssh inv set -g GROUP\nnssh inv set HOST -g GROUP",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("group") && len(args) == 0 {
				return runSetGroup(group)
			}
			if len(args) != 1 {
				return fmt.Errorf("host is required")
			}
			authPatch := hostAuthPatch{
				Clear: credentialClear,
				Auth: config.InventoryAuthConfig{
					Provider:    credentialProvider,
					Ref:         credentialRef,
					Username:    credentialUsername,
					UsernameRef: credentialUsernameRef,
				},
			}
			return runSetHost(args[0], group, hostname, user, port, cmd.Flags().Changed("port"), authPatch)
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "target group")
	cmd.Flags().StringVar(&hostname, "hostname", "", "SSH HostName")
	cmd.Flags().StringVar(&user, "user", "", "SSH User")
	cmd.Flags().IntVar(&port, "port", 0, "SSH Port")
	cmd.Flags().StringVar(&credentialProvider, "credential-provider", "", "credential provider instance for host auth override")
	cmd.Flags().StringVar(&credentialRef, "credential-ref", "", "credential provider item or secret reference")
	cmd.Flags().StringVar(&credentialUsername, "credential-username", "", "literal SSH username for credential lookup")
	cmd.Flags().StringVar(&credentialUsernameRef, "credential-username-ref", "", "provider secret reference for SSH username")
	cmd.Flags().BoolVar(&credentialClear, "credential-clear", false, "clear host auth override")
	return cmd
}

func runSetGroup(group string) error {
	if err := validateGroupName(group); err != nil {
		return err
	}
	ui.CommandStart("SET INVENTORY GROUP")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	created := ensureGroup(cfg, group)
	if err := cfg.Inventory.Validate(); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if !created {
		ui.Noop("Group %q already exists", group)
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}
	if err := config.SaveInventoryGroup(config.DefaultPaths().ConfigFile, cfg, group); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	ui.Success("Group %q created", group)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func ensureGroup(cfg *config.Config, group string) bool {
	if cfg.Inventory.Group == nil {
		cfg.Inventory.Group = make(map[string]config.GroupConfig)
	}
	if _, ok := cfg.Inventory.Group[group]; ok {
		return false
	}
	cfg.Inventory.Group[group] = config.GroupConfig{}
	return true
}

func validateGroupName(group string) error {
	if strings.TrimSpace(group) == "" {
		return fmt.Errorf("group name is required")
	}
	if strings.HasPrefix(group, "-") {
		return fmt.Errorf("group name %q cannot start with '-'", group)
	}
	if !groupNamePattern.MatchString(group) {
		return fmt.Errorf("group name %q must use only letters, numbers, underscores, and dashes", group)
	}
	return nil
}

func runSetHost(host, group, hostname, user string, port int, portSet bool, authPatch hostAuthPatch) error {
	if group != "" {
		if err := validateGroupName(group); err != nil {
			return err
		}
	}
	ui.CommandStart("SET INVENTORY HOST")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if err := authPatch.Validate(cfg); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	parser := sshconfig.NewParser()
	paths := config.DefaultPaths()
	if group != "" || hostname != "" || user != "" || portSet || !authPatch.HasChange() {
		existing, _, err := findInventoryHostWithLocation(parser, cfg, paths, host)
		if err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
		if existing != nil && metadataForHost(existing, cfg, paths, nil).Owner != "local" {
			ui.CommandEnd(ui.StatusError)
			return fmt.Errorf("host %q is provider-owned; change provider route config instead", host)
		}
		ui.Info("Owner: local inventory provider")
		ui.Info("Output: %s", localFilePath(paths, inventory.LocalProviderIncludeFile()))
		groupPrompt := promptInventoryGroup
		if summaries, err := loadInventoryGroupSummaries(cfg, parser, paths); err == nil {
			options := inventoryGroupSelectOptions(summaries)
			groupPrompt = func(groups []string) (string, error) {
				return promptInventoryGroupOptions(inventoryGroupSelectOptionsForNames(groups, options))
			}
		}
		resolvedGroup, err := resolveLocalHostGroup(cfg, hostPatch{
			Host:     host,
			Group:    group,
			HostName: hostname,
		}, existing, groupPrompt)
		if err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
		resolvedHostName := hostname
		if existing == nil && strings.TrimSpace(resolvedHostName) == "" {
			resolvedHostName = defaultHostNameForGroup(cfg, host, resolvedGroup)
		}
		if err := upsertLocalHost(parser, cfg, paths, hostPatch{
			Host:     host,
			Group:    resolvedGroup,
			HostName: resolvedHostName,
			User:     user,
			Port:     port,
			PortSet:  portSet,
		}); err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
	}
	if authPatch.HasChange() {
		if err := applyHostAuthPatch(parser, cfg, config.DefaultPaths(), host, authPatch); err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
		if err := config.SaveInventoryHostAuth(config.DefaultPaths().ConfigFile, cfg, host); err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
		stopAgentAfterInventoryAuthMutation()
	}
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
