// Package agentcmd implements nssh agent management commands.
package agentcmd

import (
	"errors"
	"fmt"

	runtimeagent "github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var startRuntimeAgent = runtimeagent.SpawnRuntime

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage nssh agent",
		Long:  "Manage the nssh background runtime.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newStopCmd())
	cmd.AddCommand(newRestartCmd())
	cmd.AddCommand(newDoctorCmd())
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		Long:  "Show nssh background runtime status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus()
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop agent",
		Long:  "Stop the nssh background runtime.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop()
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart agent",
		Long:  "Restart the nssh background runtime.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestart()
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose agent",
		Long:  "Diagnose the nssh background runtime.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runStatus() error {
	ui.CommandStart("AGENT")
	client, err := runtimeagent.Connect()
	if err != nil {
		if errors.Is(err, runtimeagent.ErrAgentNotRunning) {
			ui.PrintKeyValue("Agent", "inactive")
			ui.CommandEnd(ui.StatusNoop)
			return nil
		}
		return err
	}
	defer func() { _ = client.Close() }()

	status, err := client.Status()
	if err != nil {
		return err
	}
	ui.PrintKeyValue("Agent", "active")
	ui.PrintKeyValue("Provider sessions", fmt.Sprintf("%d", status.ProviderSessions))
	ui.PrintKeyValue("Idle in", formatAgentSeconds(status.RemainingIdle))
	ui.PrintKeyValue("Ends in", formatAgentSeconds(status.RemainingLife))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func runStop() error {
	client, err := runtimeagent.Connect()
	if err != nil {
		if errors.Is(err, runtimeagent.ErrAgentNotRunning) {
			ui.Success("Agent stopped")
			return nil
		}
		return err
	}
	defer func() { _ = client.Close() }()
	if err := client.Lock(); err != nil {
		return err
	}
	ui.Success("Agent stopped")
	return nil
}

func runRestart() error {
	if err := runStop(); err != nil {
		return err
	}
	if err := startRuntimeAgent(); err != nil {
		return err
	}
	ui.Success("Agent restarted")
	return nil
}

func runDoctor() error {
	ui.CommandStart("AGENT DOCTOR")
	if runtimeagent.IsRunning() {
		ui.PrintKeyValue("Socket", "active")
	} else {
		ui.PrintKeyValue("Socket", "inactive")
	}
	ui.PrintKeyValue("Peer verification", "enabled")
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func formatAgentSeconds(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	return fmt.Sprintf("%ds", seconds)
}
