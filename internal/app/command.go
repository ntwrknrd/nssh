package app

import (
	"context"
	"fmt"
	"strings"

	agentcmd "github.com/ntwrknrd/nssh/internal/cli/agent"
	"github.com/ntwrknrd/nssh/internal/cli/cp"
	"github.com/ntwrknrd/nssh/internal/cli/inv"
	"github.com/ntwrknrd/nssh/internal/cli/log"
	"github.com/ntwrknrd/nssh/internal/cli/self"
	"github.com/ntwrknrd/nssh/internal/cli/self/bench"
	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var (
	subcommands = map[string]bool{
		"inv":                true,
		"agent":              true,
		"log":                true,
		"cp":                 true,
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

	sshFlagsWithValue = map[string]bool{
		"-b": true,
		"-c": true,
		"-D": true,
		"-E": true,
		"-F": true,
		"-I": true,
		"-i": true,
		"-J": true,
		"-L": true,
		"-l": true,
		"-m": true,
		"-O": true,
		"-o": true,
		"-p": true,
		"-Q": true,
		"-R": true,
		"-S": true,
		"-W": true,
		"-w": true,
	}

	globalFlags = map[string]bool{
		"-v": true,
		"-V": true,
		"-h": true,
	}

	verboseCount int
	showVersion  bool
)

// NewRootCmd creates and configures the root Cobra command with all subcommands.
func NewRootCmd(opts Options) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "nssh [opts] host [cmd]",
		Short: "Smart connect to host",
		Long: `SSH wrapper for power users: manage hosts and credentials, inject passwords automatically,
and record sessions.`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh [flags] [ssh-options] HOST [command]",
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
	rootCmd.AddCommand(newSelfCmd())
	rootCmd.AddCommand(newListSubcommandsCmd())

	ui.ApplyStyledHelp(rootCmd)
	return rootCmd
}

// PreprocessArgs transforms root nssh invocations into hidden smart-connect
// calls while preserving OpenSSH grammar: options before destination, command
// after destination.
func PreprocessArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	var globalFlagArgs []string
	var sshOptionArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--select" {
			result := append([]string{}, globalFlagArgs...)
			return append(result, "smart-connect")
		}
		if arg == "--target" {
			if i+1 >= len(args) {
				return args
			}
			target := args[i+1]
			command := args[i+2:]
			return buildSmartConnectArgs(globalFlagArgs, true, target, sshOptionArgs, command)
		}

		switch {
		case globalFlags[arg] || isVerboseCluster(arg):
			globalFlagArgs = append(globalFlagArgs, arg)
		case len(arg) == 2 && sshFlagsWithValue[arg]:
			sshOptionArgs = append(sshOptionArgs, arg)
			if i+1 < len(args) {
				i++
				sshOptionArgs = append(sshOptionArgs, args[i])
			}
		case arg == "--":
			if i+1 >= len(args) {
				return args
			}
			target := args[i+1]
			command := args[i+2:]
			return buildSmartConnectArgs(globalFlagArgs, false, target, sshOptionArgs, command)
		case strings.HasPrefix(arg, "--"):
			if arg == "--verbose" || arg == "--version" || arg == "--help" {
				globalFlagArgs = append(globalFlagArgs, arg)
			} else {
				sshOptionArgs = append(sshOptionArgs, arg)
			}
		case strings.HasPrefix(arg, "-"):
			sshOptionArgs = append(sshOptionArgs, arg)
		default:
			if subcommands[arg] {
				return args
			}
			target := arg
			command := args[i+1:]
			return buildSmartConnectArgs(globalFlagArgs, false, target, sshOptionArgs, command)
		}
	}

	return args
}

func buildSmartConnectArgs(globalArgs []string, literal bool, target string, sshArgs, command []string) []string {
	result := make([]string, 0, len(globalArgs)+len(sshArgs)+len(command)+4)
	result = append(result, globalArgs...)
	result = append(result, "smart-connect")
	if literal {
		result = append(result, "--literal-target")
	}
	result = append(result, target)
	result = append(result, sshArgs...)
	if len(command) > 0 {
		result = append(result, "--")
		result = append(result, command...)
	}
	return result
}

func newSmartConnectCmd() *cobra.Command {
	var literalTarget bool
	cmd := &cobra.Command{
		Use:    "smart-connect [host] [ssh-args...]",
		Short:  "Connect to a host with smart resolution",
		Args:   cobra.ArbitraryArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var user, host string
			if len(args) > 0 {
				user, host = parseUserHost(args[0])
			}
			var sshArgs []string
			if len(args) > 1 {
				sshArgs = args[1:]
			}
			if user != "" {
				sshArgs = append([]string{"-l", user}, sshArgs...)
			}
			if literalTarget {
				return connect.ConnectLiteralHost(context.Background(), host, sshArgs, connect.Options{Verbosity: verboseCount, SSHVerbosity: sshVerbosity()})
			}
			hostname, err := connect.ResolveHostname(host)
			if err != nil {
				return err
			}
			return connect.ConnectHost(context.Background(), hostname, sshArgs, connect.Options{Verbosity: verboseCount, SSHVerbosity: sshVerbosity()})
		},
	}

	cmd.Flags().BoolVar(&literalTarget, "literal-target", false, "Use literal target")
	_ = cmd.Flags().MarkHidden("literal-target")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func isVerboseCluster(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	for _, ch := range arg[1:] {
		if ch != 'v' {
			return false
		}
	}
	return true
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
			for _, subcmd := range []string{"inv", "agent", "log", "cp", "self"} {
				fmt.Println(subcmd)
			}
		},
	}
}
