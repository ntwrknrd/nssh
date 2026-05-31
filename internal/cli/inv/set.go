package inv

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
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
	var passwordRef string
	var credentialUsername string
	var credentialUsernameRef string
	var credentialClear bool
	cmd := &cobra.Command{
		Use:   "set HOST",
		Short: "Create or update inventory",
		Long:  "Create or update a managed host, group, or host auth override.",
		Args:  cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh inv set HOST\nnssh inv set -g local/GROUP\nnssh inv set HOST -g local/GROUP",
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
					CredentialProvider: credentialProvider,
					PasswordRef:        passwordRef,
					Username:           credentialUsername,
					UsernameRef:        credentialUsernameRef,
				},
			}
			return runSetHost(args[0], group, hostname, user, port, cmd.Flags().Changed("port"), authPatch)
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "target provider-qualified group")
	cmd.Flags().StringVar(&hostname, "hostname", "", "SSH HostName")
	cmd.Flags().StringVar(&user, "user", "", "SSH User")
	cmd.Flags().IntVar(&port, "port", 0, "SSH Port")
	cmd.Flags().StringVar(&credentialProvider, "credential-provider", "", "credential provider instance for host auth override")
	cmd.Flags().StringVar(&passwordRef, "password-ref", "", "credential provider item or secret reference for SSH password")
	cmd.Flags().StringVar(&credentialUsername, "credential-username", "", "literal SSH username for credential lookup")
	cmd.Flags().StringVar(&credentialUsernameRef, "credential-username-ref", "", "provider secret reference for SSH username")
	cmd.Flags().BoolVar(&credentialClear, "credential-clear", false, "clear host auth override")
	return cmd
}

func runSetGroup(group string) error {
	if err := validateLocalGroupID(group); err != nil {
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
	_, groupName, _ := config.ParseInventoryGroupID(group)
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	localProvider := cfg.Inventory.Provider[config.ProviderLocal]
	localProvider.Type = config.ProviderLocal
	if localProvider.Group == nil {
		localProvider.Group = make(map[string]config.GroupConfig)
	}
	if _, ok := localProvider.Group[groupName]; ok {
		return false
	}
	localProvider.Group[groupName] = config.GroupConfig{}
	cfg.Inventory.Provider[config.ProviderLocal] = localProvider
	return true
}

func validateLocalGroupID(group string) error {
	provider, groupName, err := config.ParseInventoryGroupID(group)
	if err != nil {
		return err
	}
	if strings.HasPrefix(groupName, "-") || !groupNamePattern.MatchString(groupName) {
		return fmt.Errorf("group name %q must use only letters, numbers, underscores, and dashes", groupName)
	}
	if provider != config.ProviderLocal {
		return fmt.Errorf("local inventory group must use local/<group>")
	}
	return nil
}

func runSetHost(host, group, hostname, user string, port int, portSet bool, authPatch hostAuthPatch) error {
	if group != "" {
		if err := validateLocalGroupID(group); err != nil {
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
			return fmt.Errorf("host %q is provider-owned; change provider group selector config instead", host)
		}
		interactiveAdd := shouldPromptLocalHostAddDetails(existing, group, hostname, user, portSet, authPatch)
		ui.Info(`Inventory Provider: "local"`)
		ui.Info("%s", localProviderOwnerLabel(paths))
		groupPrompt := promptInventoryGroup
		if summaries, err := loadInventoryGroupSummaries(cfg, parser, paths); err == nil {
			previewHostName := host
			if strings.TrimSpace(hostname) != "" {
				previewHostName = hostname
			}
			options := inventoryGroupSelectOptions(summaries, previewHostName)
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
		patch := hostPatch{
			Host:     host,
			Group:    resolvedGroup,
			HostName: hostname,
			User:     user,
			Port:     port,
			PortSet:  portSet,
		}
		hostAuthChanged := false
		if interactiveAdd {
			patch, err = promptLocalHostAddDetails(cfg, patch, nil)
			if err != nil {
				ui.CommandEnd(ui.StatusError)
				return err
			}
			host = patch.Host
			hostAuthChanged = applyInteractiveHostAuthSelection(cfg, patch)
			credentialRecord, err := resolveLocalHostCredentialRecord(cfg, patch)
			if err != nil {
				ui.CommandEnd(ui.StatusError)
				return err
			}
			var credentialSecret *secret.Secret
			if credentialRecord != nil {
				credentialSecret = credentialRecord.Secret
			}
			if credentialSecret != nil {
				defer credentialSecret.Destroy()
			}
			draft := localHostEntryFromPatch(paths, patch)
			if user := localHostProbeUser(cfg, patch, credentialRecord); user != "" {
				upsertDirective(draft, "User", user)
				draft.Properties["user"] = user
			}
			result, err := runLocalHostCompatCheck(context.Background(), cfg, draft, 5, credentialSecret)
			if err != nil {
				ui.CommandEnd(ui.StatusError)
				return err
			}
			if len(result.FixesApplied) > 0 {
				patch.CompatFixes = result.FixesApplied
				ui.Success("Compatibility fixes validated for %s", patch.Host)
			} else if result.Success {
				ui.Success("Connection test passed for %s", patch.Host)
			} else {
				ui.Warning("Connection test did not pass: %s", result.StoppedReason)
				keep, err := ui.Confirm("Add host entry anyway?", false)
				if err != nil || !keep {
					ui.CommandEnd(ui.StatusAbort)
					return nil
				}
			}
		} else if existing == nil && strings.TrimSpace(patch.HostName) == "" {
			patch.HostName = defaultHostNameForGroup(cfg, host, resolvedGroup)
		}
		if err := upsertLocalHost(parser, cfg, paths, patch); err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
		if interactiveAdd {
			if hostAuthChanged {
				if err := config.SaveInventoryHostAuth(config.DefaultPaths().ConfigFile, cfg, patch.Host); err != nil {
					ui.CommandEnd(ui.StatusError)
					return err
				}
				stopAgentAfterInventoryAuthMutation()
			}
			printLocalWrittenHostConfig(parser, cfg, paths, patch.Host)
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
