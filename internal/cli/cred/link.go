package cred

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newLinkCmd() *cobra.Command {
	var group string
	var ref string
	var username string
	var usernameRef string
	cmd := &cobra.Command{
		Use:   "link HOST",
		Short: "Link existing credentials",
		Long:  "Link a host override or group fallback credential to an existing item in the active credential provider.",
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh cred link HOST --ref REF\nnssh cred link -g GROUP --ref REF",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := credentialScopeArg(args, group)
			if err != nil {
				return err
			}
			record := config.CredentialRefConfig{
				Ref:         ref,
				Username:    username,
				UsernameRef: usernameRef,
			}
			if err := record.Validate("credential link"); err != nil {
				return err
			}
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			if err := setCredentialLink(cfg, scope, record); err != nil {
				return err
			}
			if err := config.Save(config.DefaultPaths().ConfigFile, cfg); err != nil {
				return err
			}
			if scope.Host != "" {
				ui.Success("Credential linked for host %s", scope.Host)
				return nil
			}
			ui.Success("Credential linked for group %s", scope.Group)
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "group credential scope")
	cmd.Flags().StringVar(&ref, "ref", "", "provider item or secret reference")
	cmd.Flags().StringVar(&username, "username", "", "literal SSH username")
	cmd.Flags().StringVar(&usernameRef, "username-ref", "", "provider secret reference for SSH username")
	return cmd
}

func setCredentialLink(cfg *config.Config, scope credentialScope, ref config.CredentialRefConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if err := ref.Validate("credential link"); err != nil {
		return err
	}
	if scope.Host != "" {
		if cfg.Credential.Host == nil {
			cfg.Credential.Host = make(map[string]config.CredentialRefConfig)
		}
		cfg.Credential.Host[scope.Host] = ref
		return nil
	}
	if scope.Group != "" {
		if cfg.Credential.Group == nil {
			cfg.Credential.Group = make(map[string]config.CredentialRefConfig)
		}
		cfg.Credential.Group[scope.Group] = ref
		return nil
	}
	return fmt.Errorf("host or --group is required")
}

func clearCredentialLink(cfg *config.Config, scope credentialScope) bool {
	if cfg == nil {
		return false
	}
	if scope.Host != "" && cfg.Credential.Host != nil {
		if _, ok := cfg.Credential.Host[scope.Host]; ok {
			delete(cfg.Credential.Host, scope.Host)
			return true
		}
	}
	if scope.Group != "" && cfg.Credential.Group != nil {
		if _, ok := cfg.Credential.Group[scope.Group]; ok {
			delete(cfg.Credential.Group, scope.Group)
			return true
		}
	}
	return false
}

func clearCredentialLinkForScope(scope credentialScope) (bool, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return false, err
	}
	if !clearCredentialLink(cfg, scope) {
		return false, nil
	}
	return true, config.Save(config.DefaultPaths().ConfigFile, cfg)
}
