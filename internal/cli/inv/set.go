package inv

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var groupNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

func newSetCmd() *cobra.Command {
	var group string
	var hostname string
	var aliases []string
	var user string
	var port int
	var authMode string
	var credential string
	cmd := &cobra.Command{
		Use:   "set TARGET",
		Short: "Create or update inventory",
		Long:  "Create or update a managed host, group, or host auth override.",
		Args:  cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh inv set HOST\nnssh inv set local/GROUP\nnssh inv set HOST -g local/GROUP",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := resolveSetTarget(args, group, cmd.Flags().Changed("group"), hasHostSetFlags(cmd))
			if err != nil {
				return err
			}
			if target.Kind == setTargetGroup {
				authPatch, err := buildSetAuthPatch(authMode, credential, target.Value, true)
				if err != nil {
					return err
				}
				return runSetGroup(target.Value, authPatch)
			}
			authPatch, err := buildSetAuthPatch(authMode, credential, target.Value, false)
			if err != nil {
				return err
			}
			return runSetHost(target.Value, group, hostname, aliases, user, port, cmd.Flags().Changed("port"), authPatch)
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "target provider-qualified group")
	cmd.Flags().StringVar(&hostname, "hostname", "", "SSH connection target override")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, "add host alias")
	cmd.Flags().StringVar(&user, "user", "", "SSH User")
	cmd.Flags().IntVar(&port, "port", 0, "SSH Port")
	cmd.Flags().StringVar(&authMode, "auth", "", "auth mode: password or key")
	cmd.Flags().StringVar(&credential, "cred", "", "credential provider or provider:ref; use none to clear")
	return cmd
}

type setTargetKind string

const (
	setTargetHost  setTargetKind = "host"
	setTargetGroup setTargetKind = "group"
)

type setTarget struct {
	Kind  setTargetKind
	Value string
}

func hasHostSetFlags(cmd *cobra.Command) bool {
	for _, name := range []string{"hostname", "alias", "user", "port", "group"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func resolveSetTarget(args []string, group string, groupFlagChanged bool, hostFlagChanged bool) (setTarget, error) {
	if groupFlagChanged && len(args) == 0 {
		return setTarget{Kind: setTargetGroup, Value: group}, nil
	}
	if len(args) != 1 {
		return setTarget{}, fmt.Errorf("host is required")
	}
	if isInventoryGroupTarget(args[0]) && !hostFlagChanged {
		return setTarget{Kind: setTargetGroup, Value: args[0]}, nil
	}
	return setTarget{Kind: setTargetHost, Value: args[0]}, nil
}

func isInventoryGroupTarget(target string) bool {
	if !strings.Contains(target, "/") {
		return false
	}
	_, _, err := config.ParseInventoryGroupID(target)
	return err == nil
}

func buildSetAuthPatch(authMode, credentialValue, target string, groupTarget bool) (inventoryAuthPatch, error) {
	authMode = strings.ToLower(strings.TrimSpace(authMode))
	credentialValue = strings.TrimSpace(credentialValue)
	if authMode != "" && authMode != config.AuthModePassword && authMode != config.AuthModeKey {
		return inventoryAuthPatch{}, fmt.Errorf("--auth must be password or key")
	}
	if authMode == "" && credentialValue == "" {
		return inventoryAuthPatch{}, nil
	}
	if credentialValue == "none" {
		if authMode == "" {
			return inventoryAuthPatch{Clear: true}, nil
		}
		return inventoryAuthPatch{Auth: config.InventoryAuthConfig{Mode: authMode}}, nil
	}
	if authMode == config.AuthModeKey && credentialValue != "" {
		return inventoryAuthPatch{}, fmt.Errorf("--auth key conflicts with --cred")
	}

	auth := config.InventoryAuthConfig{Mode: authMode}
	if credentialValue != "" {
		provider, ref := splitCredentialValue(credentialValue)
		if provider == "" {
			return inventoryAuthPatch{}, fmt.Errorf("--cred must be <provider> or <provider>:<ref>")
		}
		if ref == "" {
			ref = config.DefaultCredentialRef(provider, target, groupTarget)
		}
		auth.Mode = config.AuthModePassword
		auth.CredentialProvider = provider
		auth.PasswordRef = ref
	}
	return inventoryAuthPatch{Auth: auth}, nil
}

func splitCredentialValue(value string) (string, string) {
	provider, ref, ok := strings.Cut(value, ":")
	if !ok {
		return strings.TrimSpace(value), ""
	}
	return strings.TrimSpace(provider), strings.TrimSpace(ref)
}

func runSetGroup(group string, authPatch inventoryAuthPatch) error {
	if _, _, err := config.ParseInventoryGroupID(group); err != nil {
		return err
	}
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	if err := authPatch.Validate(cfg); err != nil {
		return err
	}
	created, err := ensureInventoryGroup(cfg, group)
	if err != nil {
		return err
	}
	if !authPatch.HasChange() {
		authPatch, err = promptGroupAuthPatch(cfg, group)
		if err != nil {
			return err
		}
		if err := authPatch.Validate(cfg); err != nil {
			return err
		}
	}
	if authPatch.HasChange() {
		if err := applyGroupAuthPatch(cfg, group, authPatch); err != nil {
			return err
		}
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return err
	}
	if !created && !authPatch.HasChange() {
		ui.Noop("Group %q already exists", group)
		return nil
	}
	if err := config.SaveInventoryGroup(config.DefaultPaths().ConfigFile, cfg, group); err != nil {
		return err
	}
	if created {
		ui.Success("Group %q created", group)
	} else {
		ui.Success("Group %q updated", group)
	}
	if authPatch.HasChange() {
		stopAgentAfterInventoryAuthMutation()
	}
	return nil
}

func promptGroupAuthPatch(cfg *config.Config, group string) (inventoryAuthPatch, error) {
	return promptGroupAuthPatchWithPrompter(cfg, group, uiLocalHostAddPrompter{})
}

func promptGroupAuthPatchWithPrompter(cfg *config.Config, group string, prompter localHostAddPrompter) (inventoryAuthPatch, error) {
	selected, err := promptSelectWithBack(prompter, "Authentication", []ui.SelectOption{
		{Label: "Password", Value: config.AuthModePassword},
		{Label: "SSH key", Value: config.AuthModeKey},
		{Label: "No stored credential", Value: "none"},
	}, true)
	if err != nil {
		return inventoryAuthPatch{}, err
	}
	switch selected {
	case "", "none":
		return inventoryAuthPatch{Clear: true}, nil
	case config.AuthModeKey:
		return inventoryAuthPatch{Auth: config.InventoryAuthConfig{Mode: config.AuthModeKey}}, nil
	case config.AuthModePassword:
		auth, err := promptStoredCredentialAuth(cfg, group, true, prompter)
		if err != nil {
			return inventoryAuthPatch{}, err
		}
		auth.Mode = config.AuthModePassword
		return inventoryAuthPatch{Auth: auth}, nil
	default:
		return inventoryAuthPatch{}, fmt.Errorf("unknown auth mode %q", selected)
	}
}

func ensureGroup(cfg *config.Config, group string) bool {
	return ensureGroupWithConfig(cfg, group, config.GroupConfig{})
}

func ensureInventoryGroup(cfg *config.Config, groupID string) (bool, error) {
	provider, groupName, err := config.ParseInventoryGroupID(groupID)
	if err != nil {
		return false, err
	}
	if provider == config.ProviderLocal {
		return ensureGroup(cfg, groupID), nil
	}
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	providerCfg, ok := cfg.Inventory.Provider[provider]
	if !ok {
		return false, fmt.Errorf("inventory provider %q is not configured", provider)
	}
	if providerCfg.Group == nil {
		providerCfg.Group = make(map[string]config.GroupConfig)
	}
	if _, ok := providerCfg.Group[groupName]; ok {
		return false, nil
	}
	providerCfg.Group[groupName] = config.GroupConfig{}
	cfg.Inventory.Provider[provider] = providerCfg
	return true, nil
}

func applyGroupAuthPatch(cfg *config.Config, groupID string, patch inventoryAuthPatch) error {
	if !patch.HasChange() {
		return nil
	}
	provider, groupName, err := config.ParseInventoryGroupID(groupID)
	if err != nil {
		return err
	}
	providerCfg := cfg.Inventory.Provider[provider]
	groupCfg := providerCfg.Group[groupName]
	if patch.Clear {
		groupCfg.Auth = config.InventoryAuthConfig{}
	} else {
		groupCfg.Auth = mergeInventoryAuthPatch(groupCfg.Auth, patch.Auth)
	}
	providerCfg.Group[groupName] = groupCfg
	cfg.Inventory.Provider[provider] = providerCfg
	return cfg.Validate()
}

func ensureGroupWithConfig(cfg *config.Config, group string, groupCfg config.GroupConfig) bool {
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
	localProvider.Group[groupName] = groupCfg
	cfg.Inventory.Provider[config.ProviderLocal] = localProvider
	return true
}

func ensureLocalGroup(cfg *config.Config, group string, patch hostPatch) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("config is required")
	}
	if err := validateLocalGroupID(group); err != nil {
		return false, err
	}
	if _, ok, err := localProviderGroup(cfg, group); err != nil {
		return false, err
	} else if ok {
		return false, nil
	}
	created := ensureGroupWithConfig(cfg, group, localGroupConfigFromPatch(patch))
	if err := cfg.Inventory.Validate(); err != nil {
		return false, err
	}
	return created, nil
}

func localGroupConfigFromPatch(patch hostPatch) config.GroupConfig {
	target := strings.TrimSpace(patch.HostName)
	if target == "" {
		target = strings.TrimSpace(patch.Host)
	}
	suffix := domainSuffixFromFQDN(target)
	if suffix == "" {
		return config.GroupConfig{}
	}
	return config.GroupConfig{
		Match: config.InventoryMatch{"domain_suffix": []string{suffix}},
	}
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

func runSetHost(host, group, hostname string, aliases []string, user string, port int, portSet bool, authPatch inventoryAuthPatch) error {
	if group != "" {
		if err := validateLocalGroupID(group); err != nil {
			return err
		}
	}
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	if err := authPatch.Validate(cfg); err != nil {
		return err
	}
	var parser *sshconfig.Parser
	paths := config.DefaultPaths()
	pendingCreatedGroup := ""
	if group != "" || hostname != "" || len(aliases) > 0 || user != "" || portSet || !authPatch.HasChange() {
		existing, _, err := findInventoryHostWithLocation(parser, cfg, paths, host)
		if err != nil {
			return err
		}
		if existing != nil && metadataForHost(existing, cfg, paths, nil).Owner != "local" {
			return fmt.Errorf("host %q is provider-owned; change provider group selector config instead", host)
		}
		interactiveAdd := shouldPromptLocalHostAddDetails(existing, group, hostname, user, portSet, authPatch)
		ui.Info(`Inventory Provider: "local"`)
		ui.Info("%s", localProviderOwnerLabel(paths))
		patch := hostPatch{
			Host:     host,
			HostName: hostname,
			Aliases:  aliases,
			User:     user,
			Port:     port,
			PortSet:  portSet,
		}
		hostAuthChanged := false
		var groupCreated bool
		for {
			if interactiveAdd {
				patch, err = promptLocalHostHost(patch, nil)
				if err != nil {
					return err
				}
				host = patch.Host
				existing, _, err = findInventoryHostWithLocation(parser, cfg, paths, host)
				if err != nil {
					return err
				}
				if existing != nil && metadataForHost(existing, cfg, paths, nil).Owner != "local" {
					return fmt.Errorf("host %q is provider-owned; change provider group selector config instead", host)
				}
			}
			groupPrompt := promptInventoryGroup
			if summaries, err := loadInventoryGroupSummaries(cfg, parser, paths); err == nil {
				options := inventoryGroupSelectOptions(summaries, patch.Host)
				groupPrompt = func(groups []string) (string, error) {
					return promptInventoryGroupOptions(inventoryGroupSelectOptionsForNames(groups, options))
				}
			}
			resolvedGroup, err := resolveLocalHostGroup(cfg, hostPatch{
				Host:  patch.Host,
				Group: group,
			}, existing, groupPrompt)
			if err != nil {
				if errors.Is(err, errPromptBack) {
					return nil
				}
				return err
			}
			patch.Group = resolvedGroup
			if interactiveAdd {
				proxyHosts, listErr := inventoryProxyHostNames(parser, cfg, paths, patch.Host)
				if listErr != nil {
					return listErr
				}
				patch, err = promptLocalHostConnectionDetailsWithProxyHosts(cfg, patch, nil, proxyHosts)
				if errors.Is(err, errPromptBack) && group == "" {
					continue
				}
				if err != nil {
					return err
				}
				host = patch.Host
			}
			groupCreated, err = ensureLocalGroup(cfg, patch.Group, patch)
			if err != nil {
				return err
			}
			if groupCreated {
				pendingCreatedGroup = patch.Group
			}
			if interactiveAdd {
				hostAuthChanged = applyInteractiveHostAuthSelection(cfg, patch)
				credentialRecord, err := resolveLocalHostCredentialRecord(cfg, patch)
				if err != nil {
					return err
				}
				credentialSecret, err := localHostProbeCredentialSecret(patch, credentialRecord)
				if err != nil {
					return err
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
						return nil
					}
				}
			}
			break
		}
		if err := upsertLocalHost(parser, cfg, paths, patch); err != nil {
			return err
		}
		if groupCreated && hostAuthChanged {
			if err := config.SaveInventoryGroupAndHostAuth(config.DefaultPaths().ConfigFile, cfg, patch.Group, patch.Host); err != nil {
				return err
			}
			ui.Success("Group %q created", patch.Group)
			stopAgentAfterInventoryAuthMutation()
			pendingCreatedGroup = ""
		} else if groupCreated && !authPatch.HasChange() {
			if err := config.SaveInventoryGroup(config.DefaultPaths().ConfigFile, cfg, patch.Group); err != nil {
				return err
			}
			ui.Success("Group %q created", patch.Group)
			pendingCreatedGroup = ""
		} else if hostAuthChanged {
			if err := config.SaveInventoryHostAuth(config.DefaultPaths().ConfigFile, cfg, patch.Host); err != nil {
				return err
			}
			stopAgentAfterInventoryAuthMutation()
		}
		if interactiveAdd {
			printLocalWrittenHostConfig(parser, cfg, paths, patch.Host)
		}
	}
	if authPatch.HasChange() {
		if err := applyInventoryAuthPatch(parser, cfg, config.DefaultPaths(), host, authPatch); err != nil {
			return err
		}
		if pendingCreatedGroup != "" {
			if err := config.SaveInventoryGroupAndHostAuth(config.DefaultPaths().ConfigFile, cfg, pendingCreatedGroup, host); err != nil {
				return err
			}
			ui.Success("Group %q created", pendingCreatedGroup)
		} else if err := config.SaveInventoryHostAuth(config.DefaultPaths().ConfigFile, cfg, host); err != nil {
			return err
		}
		stopAgentAfterInventoryAuthMutation()
	}
	return nil
}
