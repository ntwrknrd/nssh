package cred

import (
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:     "rm HOST",
		Aliases: []string{"remove"},
		Short:   "Remove credentials",
		Long:    "Remove a host override or group fallback credential.",
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh cred rm HOST\nnssh cred rm -g GROUP",
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
			var removed bool
			if scope.Host != "" {
				removed, err = provider.RemoveHost(scope.Host)
			} else {
				removed, err = provider.RemoveGroup(scope.Group)
			}
			if err != nil {
				return err
			}
			if removed {
				ui.Success("Credential removed")
			} else {
				ui.Noop("No credential found")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "group credential scope")
	return cmd
}
