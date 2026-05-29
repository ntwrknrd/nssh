package inv

import (
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
	var user string
	var port int
	cmd := &cobra.Command{
		Use:   "set HOST",
		Short: "Create or update local inventory",
		Long:  "Create or update a local-provider host or group.",
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
			return runSetHost(args[0], group, hostname, user, port, cmd.Flags().Changed("port"))
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "target group")
	cmd.Flags().StringVar(&hostname, "hostname", "", "SSH HostName")
	cmd.Flags().StringVar(&user, "user", "", "SSH User")
	cmd.Flags().IntVar(&port, "port", 0, "SSH Port")
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
	if err := config.Save(config.DefaultPaths().ConfigFile, cfg); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if created {
		ui.Success("Group %q created", group)
	} else {
		ui.Noop("Group %q already exists", group)
	}
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
	cfg.Inventory.Group[group] = config.GroupConfig{LocalFile: "local_" + group + ".conf"}
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

func runSetHost(host, group, hostname, user string, port int, portSet bool) error {
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
	if err := upsertLocalHost(sshconfig.NewParser(), cfg, config.DefaultPaths(), hostPatch{
		Host:     host,
		Group:    group,
		HostName: hostname,
		User:     user,
		Port:     port,
		PortSet:  portSet,
	}); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	ui.Info("Credentials are managed separately with: nssh cred set %s", host)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
