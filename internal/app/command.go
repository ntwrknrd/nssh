package app

import (
	"context"
	"fmt"
	"strings"

	agentcmd "github.com/ntwrknrd/nssh/internal/cli/agent"
	"github.com/ntwrknrd/nssh/internal/cli/cp"
	"github.com/ntwrknrd/nssh/internal/cli/inv"
	"github.com/ntwrknrd/nssh/internal/cli/log"
	"github.com/ntwrknrd/nssh/internal/cli/repl"
	"github.com/ntwrknrd/nssh/internal/cli/self"
	"github.com/ntwrknrd/nssh/internal/cli/self/bench"
	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/ssh/sshargs"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var (
	subcommands = map[string]bool{
		"inv":                true,
		"agent":              true,
		"log":                true,
		"cp":                 true,
		"repl":               true,
		"self":               true,
		"smart-connect":      true,
		"__list-subcommands": true,
		"__agent":            true,
	}

	completionEntrypoints = map[string]bool{
		"completion":       true,
		"__complete":       true,
		"__completeNoDesc": true,
	}

	verboseCount int
	showVersion  bool

	connectRequestFunc = connect.ConnectRequest
	runHostListFunc    = repl.RunHosts
)

// NewRootCmd creates and configures the root Cobra command with all subcommands.
func NewRootCmd(opts Options) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "nssh [opts] host [cmd]",
		Short: "Smart connect to host",
		Long: `SSH wrapper for power users: manage hosts and credentials, inject passwords automatically,
and record sessions.

Run one remote command across a bare comma-separated host list:
  nssh 'host1,host2' 'show version'
Lists use four workers, require a command, and close remote stdin.
Use --target to force a literal destination.`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh [flags] [ssh-options] HOST [command]\nnssh [flags] [ssh-options] 'HOST1,HOST2' command",
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if showVersion {
				self.RunVersionExit()
			}
			initLogging(verboseCount > 0)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().CountVarP(&verboseCount, "verbose", "v", "Increase debug verbosity")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "Print command version")
	rootCmd.Flags().Bool("select", false, "Open smart target picker")
	rootCmd.Flags().String("target", "", "Use literal target")
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	rootCmd.PersistentFlags().BoolP("help", "h", false, "Print command help")

	self.SetVersion(opts.Version, opts.Commit, opts.Date)

	rootCmd.AddCommand(newSmartConnectCmd())
	agentCmd := agentcmd.NewCmd()
	ui.ApplyStyledHelp(agentCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(newInvCmd())
	rootCmd.AddCommand(newLogCmd())
	rootCmd.AddCommand(newCpCmd())
	rootCmd.AddCommand(newReplCmd())
	rootCmd.AddCommand(newSelfCmd())
	rootCmd.AddCommand(newListSubcommandsCmd())

	ui.ApplyStyledHelp(rootCmd)
	return rootCmd
}

func newSmartConnectCmd() *cobra.Command {
	var literalTarget bool
	cmd := &cobra.Command{
		Use:    "smart-connect [host] [ssh-args...]",
		Short:  "Connect to a host with smart resolution",
		Args:   cobra.ArbitraryArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := &connect.Request{}
			if len(args) > 0 {
				var err error
				req, err = destinationRequest(args[0], nil)
				if err != nil {
					return err
				}
			}
			if len(args) > 1 {
				sshArgs, command := splitSmartConnectArgs(args[1:])
				req.SSHArgs = append(req.SSHArgs, sshArgs...)
				req.RemoteCommand = command
			}
			req.LiteralTarget = literalTarget
			req.Options = connect.Options{Verbosity: verboseCount, SSHVerbosity: sshVerbosity()}
			return connectRequestFunc(context.Background(), *req)
		},
	}

	cmd.Flags().BoolVar(&literalTarget, "literal-target", false, "Use literal target")
	_ = cmd.Flags().MarkHidden("literal-target")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func splitSmartConnectArgs(args []string) (sshArgs, remoteCommand []string) {
	return sshargs.Split(args)
}

func sshVerbosity() int {
	level := verboseCount - 1
	if level < 0 {
		return 0
	}
	if level > 3 {
		return 3
	}
	return level
}

func parseUserHost(input string) (username, hostname string) {
	if idx := strings.LastIndex(input, "@"); idx != -1 {
		return input[:idx], input[idx+1:]
	}
	return "", input
}

func newInvCmd() *cobra.Command {
	cmd := inv.NewCmd()
	ui.ApplyStyledHelpRecursive(cmd)
	return cmd
}

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Manage session recordings",
		Long:  "Manage recorded SSH sessions including playback, export, and upload.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(log.NewListCmd())
	cmd.AddCommand(log.NewPlayCmd())
	cmd.AddCommand(log.NewDeleteCmd())
	cmd.AddCommand(log.NewUploadCmd())
	cmd.AddCommand(log.NewExportCmd())
	cmd.AddCommand(log.NewAuthCmd())
	cmd.AddCommand(log.NewSearchCmd())
	cmd.AddCommand(log.NewArchiveCmd())

	ui.ApplyStyledHelpRecursive(cmd)
	return cmd
}

func newCpCmd() *cobra.Command {
	return cp.NewCmd()
}

func newReplCmd() *cobra.Command {
	cmd := repl.NewCmd()
	ui.ApplyStyledHelp(cmd)
	return cmd
}

func newBenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Performance benchmarking",
		Long:  "Run performance benchmarks for SSH and SCP connections.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(bench.NewSSHCmd())
	cmd.AddCommand(bench.NewSCPCmd())

	return cmd
}

func newSelfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage nssh",
		Long:  "Manage nssh installation and updates.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(self.NewInitCmd())
	cmd.AddCommand(self.NewStatusCmd())
	cmd.AddCommand(self.NewImportCmd())
	cmd.AddCommand(self.NewReinstallCmd())
	cmd.AddCommand(self.NewUninstallCmd())
	cmd.AddCommand(self.NewResetCmd())
	cmd.AddCommand(self.NewVersionCmd())
	cmd.AddCommand(self.NewCfgCmd())
	cmd.AddCommand(newBenchCmd())

	ui.ApplyStyledHelpRecursive(cmd)
	return cmd
}

func newListSubcommandsCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__list-subcommands",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			for _, subcmd := range []string{"inv", "agent", "log", "cp", "repl", "self"} {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), subcmd)
			}
		},
	}
}
