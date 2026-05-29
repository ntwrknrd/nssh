package cred

import (
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

type credentialView struct {
	Scope           string
	HostOverride    string
	Group           string
	EffectiveSource string
	Record          *credential.Record
}

func newGetCmd() *cobra.Command {
	var group string
	var revealSecret bool
	cmd := &cobra.Command{
		Use:   "get HOST",
		Short: "Show credentials",
		Long:  "Show the effective credential for a host or the stored credential for a group.",
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh cred get HOST\nnssh cred get -g GROUP",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := credentialScopeArg(args, group)
			if err != nil {
				return err
			}
			provider, err := activeWritableProvider()
			if err != nil {
				return err
			}
			if scope.Host != "" {
				view, err := hostCredentialView(provider, nil, nil, scope.Host)
				if err != nil {
					return err
				}
				printCredentialView(view, revealSecret)
				return nil
			}
			record, err := provider.GetGroup(scope.Group)
			if err != nil {
				return err
			}
			printCredentialView(&credentialView{
				Scope:           "group " + scope.Group,
				EffectiveSource: sourceForRecord(record, "group "+scope.Group),
				Record:          record,
			}, revealSecret)
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "group credential scope")
	cmd.Flags().BoolVarP(&revealSecret, "reveal-secret", "r", false, "reveal password in plain text")
	return cmd
}

func hostCredentialView(provider credential.Provider, cfg *config.Config, parser *sshconfig.Parser, host string) (*credentialView, error) {
	record, err := provider.GetHost(host)
	if err != nil {
		return nil, err
	}
	group, err := credentialGroupForHost(cfg, parser, host)
	if err != nil {
		return nil, err
	}
	view := &credentialView{
		Scope:        "host " + host,
		HostOverride: "-",
		Group:        valueOrDash(group),
	}
	if record != nil {
		view.HostOverride = "set"
		view.EffectiveSource = "host " + host
		view.Record = record
		return view, nil
	}
	if group != "" {
		record, err = provider.GetGroup(group)
		if err != nil {
			return nil, err
		}
		if record != nil {
			view.EffectiveSource = "group " + group
			view.Record = record
			return view, nil
		}
	}
	view.EffectiveSource = "-"
	return view, nil
}

func credentialGroupForHost(cfg *config.Config, parser *sshconfig.Parser, host string) (string, error) {
	if cfg == nil {
		var err error
		cfg, err = config.LoadDefault()
		if err != nil {
			return "", err
		}
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return "", err
	}
	if parser == nil {
		parser = sshconfig.NewParser()
	}
	hostEntry, err := parser.FindHost(host)
	if err != nil || hostEntry == nil {
		return "", err
	}
	if index, err := inventory.BuildProviderIndex(); err == nil {
		if info := index[hostEntry.Host]; info != nil {
			return info.Group, nil
		}
		for _, pattern := range hostEntry.Patterns {
			if info := index[pattern]; info != nil {
				return info.Group, nil
			}
		}
	}
	return inventory.LocalHostGroup(hostEntry, cfg.Inventory.DefaultGroup), nil
}

func printCredentialView(view *credentialView, showSecret bool) {
	ui.CommandStart("CREDENTIAL")
	ui.PrintKeyValue("Scope", view.Scope)
	if view.HostOverride != "" {
		ui.PrintKeyValue("Host Override", view.HostOverride)
	}
	if view.Group != "" {
		ui.PrintKeyValue("Group", view.Group)
	}
	if view.EffectiveSource != "" {
		ui.PrintKeyValue("Effective Source", view.EffectiveSource)
	}
	if view.Record == nil {
		ui.PrintKeyValue("Credential", "-")
		ui.CommandEnd(ui.StatusNoop)
		return
	}
	ui.PrintKeyValue("Username", view.Record.Username)
	ui.PrintKeyValue("Password", displaySecret(view.Record, showSecret))
	if view.Record.Ref != "" {
		ui.PrintKeyValue("Ref", view.Record.Ref)
	}
	ui.CommandEnd(ui.StatusSuccess)
}

func sourceForRecord(record *credential.Record, source string) string {
	if record == nil {
		return "-"
	}
	return source
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func displaySecret(record *credential.Record, show bool) string {
	if record == nil || record.Secret == nil {
		return "-"
	}
	if !show {
		return "****"
	}
	value := ""
	if err := record.Secret.UseString(func(s string) error {
		value = s
		return nil
	}); err != nil {
		return "-"
	}
	if value == "" {
		return "-"
	}
	return value
}
