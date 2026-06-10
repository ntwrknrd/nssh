package repl

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestNewCmdRegistersHiddenBrokerCommand(t *testing.T) {
	cmd := NewCmd()

	brokerCmd, _, err := cmd.Find([]string{"broker"})
	if err != nil {
		t.Fatalf("find broker: %v", err)
	}
	if brokerCmd == cmd || brokerCmd.Name() != "broker" {
		t.Fatalf("expected broker child command, got %v", brokerCmd)
	}
	if !brokerCmd.Hidden {
		t.Fatal("broker command should be hidden")
	}
}

func TestNewCmdHelpDoesNotShowBrokerCommand(t *testing.T) {
	cmd := NewCmd()

	help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	if strings.Contains(help, "broker") {
		t.Fatalf("broker command should be hidden from help:\n%s", help)
	}
}

func TestNewCmdRegistersDiffFlag(t *testing.T) {
	cmd := NewCmd()

	if flag := cmd.Flags().Lookup("diff"); flag == nil {
		t.Fatal("missing --diff flag")
	}
}

func TestNewCmdRegistersCursorFlag(t *testing.T) {
	cmd := NewCmd()

	flag := cmd.Flags().Lookup("cursor")
	if flag == nil {
		t.Fatal("missing --cursor flag")
	}
	if flag.DefValue != "pipe" {
		t.Fatalf("cursor default = %q, want pipe", flag.DefValue)
	}
}
