// Package agentcmd implements nssh agent management commands.
package agentcmd

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	runtimeagent "github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var (
	detectRuntimeAgentProcessCount = defaultRuntimeAgentProcessCount
)

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage nssh agent",
		Long:  "Manage the nssh background runtime.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newStopCmd())
	cmd.AddCommand(newResetCmd())
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		Long:  "Show nssh background runtime status.",
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop()
		},
	}
}

func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset agent",
		Long:  "Stop the nssh background runtime and clear retained access state.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset()
		},
	}
}

func runStatus() error {
	client, err := runtimeagent.Connect()
	if err != nil {
		if errors.Is(err, runtimeagent.ErrAgentNotRunning) {
			ui.PrintKeyValue("Agent", "inactive")
			return nil
		}
		return err
	}
	defer func() { _ = client.Close() }()

	status, err := client.Status()
	if err != nil {
		return err
	}
	if detected := detectRuntimeAgentProcessCount(); detected > status.ProcessCount {
		status.ProcessCount = detected
		status.DuplicateProcesses = detected > 1
	}
	ui.PrintKeyValue("Agent", "active")
	ui.PrintKeyValue("Access", formatManagedAccess(status))
	ui.PrintKeyValue("Health", formatHealth(status))
	ui.PrintKeyValue("Resources", formatResources(status))
	ui.PrintKeyValue("Idle shutdown in", formatAgentSeconds(status.RemainingIdle))
	ui.PrintKeyValue("Max lifetime ends in", formatAgentSeconds(status.RemainingLife))
	return nil
}

func runStop() error {
	if err := stopRuntimeAgent(); err != nil {
		return err
	}
	ui.Success("Agent stopped")
	return nil
}

func runReset() error {
	if err := stopRuntimeAgent(); err != nil {
		return err
	}
	ui.Success("Agent reset")
	return nil
}

func stopRuntimeAgent() error {
	client, err := runtimeagent.Connect()
	if err != nil {
		if errors.Is(err, runtimeagent.ErrAgentNotRunning) {
			return nil
		}
		return err
	}
	defer func() { _ = client.Close() }()
	if err := client.Lock(); err != nil {
		return err
	}
	return nil
}

func formatAgentSeconds(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	duration := time.Duration(seconds) * time.Second
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	secs := int(duration / time.Second)

	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", secs))
	}
	return strings.Join(parts, " ")
}

func formatCredentialProviders(names []string, count int) string {
	if len(names) == 0 {
		return fmt.Sprintf("%d", count)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return fmt.Sprintf("%d (%s)", len(sorted), strings.Join(sorted, ", "))
}

func formatManagedAccess(status *runtimeagent.StatusInfo) string {
	if status == nil || len(status.Access) == 0 {
		return "none retained"
	}
	parts := make([]string, 0, len(status.Access))
	for _, entry := range status.Access {
		switch {
		case entry.Type == "1password" && entry.OnePasswordKeepalive:
			parts = append(parts, formatOnePasswordAccess(entry))
		case entry.Type == "bitwarden" && entry.BitwardenWarmSession:
			state := "inactive"
			if entry.BitwardenWarmActive {
				state = "active"
			}
			parts = append(parts, fmt.Sprintf("%s warm session %s", entry.Name, state))
		}
	}
	if len(parts) == 0 {
		return "none retained"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func formatOnePasswordAccess(entry runtimeagent.AccessStatus) string {
	state := entry.OnePasswordState
	if state == "" {
		state = "unknown"
	}
	base := fmt.Sprintf("%s keepalive %s", entry.Name, state)
	details := make([]string, 0, 4)
	if entry.KeepaliveInterval > 0 {
		details = append(details, "every "+formatAgentSeconds(entry.KeepaliveInterval))
	}
	if entry.KeepaliveNextUnix > 0 {
		next := time.Until(time.Unix(entry.KeepaliveNextUnix, 0))
		if next <= 0 {
			details = append(details, "next due")
		} else {
			details = append(details, "next in "+formatAgentSeconds(int64(next.Seconds())))
		}
	}
	if entry.KeepaliveLastSuccess > 0 {
		ago := time.Since(time.Unix(entry.KeepaliveLastSuccess, 0))
		if ago < 0 {
			ago = 0
		}
		details = append(details, "last ok "+formatAgentSeconds(int64(ago.Seconds()))+" ago")
	}
	if entry.LastError != "" {
		details = append(details, "last error "+entry.LastError)
	}
	if len(details) == 0 {
		return base
	}
	return fmt.Sprintf("%s (%s)", base, strings.Join(details, ", "))
}

func formatHealth(status *runtimeagent.StatusInfo) string {
	if status == nil {
		return "unknown"
	}
	processWord := "process"
	if status.ProcessCount != 1 {
		processWord = "processes"
	}
	parts := []string{
		fmt.Sprintf("%d %s", status.ProcessCount, processWord),
		fmt.Sprintf("%d requests", status.ProviderRequests),
		fmt.Sprintf("%d failures", status.ProviderFailures),
	}
	if status.DuplicateProcesses {
		parts = append(parts, "duplicate runtime warning")
	}
	return strings.Join(parts, ", ")
}

func formatResources(status *runtimeagent.StatusInfo) string {
	if status == nil {
		return "unknown"
	}
	rss := "unknown RSS"
	if status.RSSBytes > 0 {
		rss = fmt.Sprintf("%d MB RSS", bytesToMB(status.RSSBytes))
	}
	openFDs := "unknown fds"
	if status.OpenFDs >= 0 {
		openFDs = fmt.Sprintf("%d fds", status.OpenFDs)
	}
	return fmt.Sprintf("%s, %d MB heap, %d goroutines, %s",
		rss,
		bytesToMB(status.HeapAllocBytes),
		status.Goroutines,
		openFDs)
}

func bytesToMB(bytes uint64) uint64 {
	return bytes / (1024 * 1024)
}

func defaultRuntimeAgentProcessCount() int {
	out, err := exec.Command("ps", "-axo", "command=").Output()
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "__agent") && strings.Contains(line, "nssh") {
			count++
		}
	}
	return count
}
