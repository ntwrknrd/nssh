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
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var (
	subcommands = map[string]bool{
		"inv":                true,
		"agent":              true,
		"log":                true,
		"repl":               true,
		"cp":                 true,
		"self":               true,
		"connect":            true,
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

	verbose     bool
	showVersion bool
)

// NewRootCmd creates and configures the root Cobra command with all subcommands.
func NewRootCmd(opts Options) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "nssh [host]",
		Short: "Smart connect to host",
		Long: `SSH wrapper for power users: manage hosts and credentials, inject passwords automatically,
and record sessions.`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if showVersion {
				self.RunVersionExit()
			}
			initLogging(verbose)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Print debug messages")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "Print command version")
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	rootCmd.PersistentFlags().BoolP("help", "h", false, "Print command help")

	self.SetVersion(opts.Version, opts.Commit, opts.Date)

	rootCmd.AddCommand(newConnectCmd())
	rootCmd.AddCommand(newSmartConnectCmd())
	agentCmd := agentcmd.NewCmd()
	ui.ApplyStyledHelp(agentCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(newInvCmd())
	rootCmd.AddCommand(newLogCmd())
	rootCmd.AddCommand(repl.NewCmd())
	rootCmd.AddCommand(newCpCmd())
	rootCmd.AddCommand(newSelfCmd())
	rootCmd.AddCommand(newListSubcommandsCmd())

	ui.ApplyStyledHelp(rootCmd)
	return rootCmd
}

// PreprocessArgs transforms "nssh hostname" into "nssh smart-connect hostname".
func PreprocessArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	var globalFlagArgs []string
	var sshPassthroughArgs []string
	var hostnameIdx = -1

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if !strings.HasPrefix(arg, "-") {
			if subcommands[arg] {
				return args
			}
			hostnameIdx = i
			sshPassthroughArgs = append(sshPassthroughArgs, args[i+1:]...)
			break
		}

		switch {
		case globalFlags[arg]:
			globalFlagArgs = append(globalFlagArgs, arg)
		case len(arg) == 2 && sshFlagsWithValue[arg]:
			sshPassthroughArgs = append(sshPassthroughArgs, arg)
			if i+1 < len(args) {
				i++
				sshPassthroughArgs = append(sshPassthroughArgs, args[i])
			}
		case strings.HasPrefix(arg, "--"):
			if arg == "--verbose" || arg == "--version" || arg == "--help" {
				globalFlagArgs = append(globalFlagArgs, arg)
			} else {
				sshPassthroughArgs = append(sshPassthroughArgs, arg)
			}
		default:
			sshPassthroughArgs = append(sshPassthroughArgs, arg)
		}
	}

	if hostnameIdx == -1 {
		return args
	}

	hostname := args[hostnameIdx]
	result := make([]string, 0, len(args)+1)
	result = append(result, globalFlagArgs...)
	result = append(result, "smart-connect", hostname)
	result = append(result, sshPassthroughArgs...)
	return result
}

func newConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect [host]",
		Short: "Connect to host",
		Long: `Connect to a host via SSH with direct routing.

Without a hostname, opens the fuzzy finder across all known hosts.
With a hostname, bypasses smart matching and treats the argument as the
SSH destination verbatim - no host-add fallback. Useful when a Host alias
conflicts with a subcommand name (e.g., "host", "log", "cp", "self").

Example: nssh connect host -p 2222`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				hostname, err := connect.ResolveHostname("")
				if err != nil {
					return err
				}
				return connect.ConnectHost(context.Background(), hostname, nil)
			}
			return connect.ConnectHost(context.Background(), args[0], args[1:])
		},
	}

	cmd.Flags().SetInterspersed(false)
	ui.ApplyStyledHelp(cmd)
	return cmd
}

func newSmartConnectCmd() *cobra.Command {
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
			hostname, err := connect.ResolveHostname(host)
			if err != nil {
				return err
			}
			var sshArgs []string
			if len(args) > 1 {
				sshArgs = args[1:]
			}
			if user != "" {
				sshArgs = append([]string{"-l", user}, sshArgs...)
			}
			return connect.ConnectHost(context.Background(), hostname, sshArgs)
		},
	}

	cmd.Flags().SetInterspersed(false)
	return cmd
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
	cmd.AddCommand(bench.NewCredentialCmd())

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
			for _, subcmd := range []string{"inv", "agent", "log", "cp", "self", "connect"} {
				fmt.Println(subcmd)
			}
		},
	}
}
