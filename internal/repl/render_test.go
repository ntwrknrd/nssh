package repl

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestRenderOutputBannerColorCodesHostAndCommand(t *testing.T) {
	got := RenderOutputBanner("edge01", "show hostname")

	hostStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	commandStyle := lipgloss.NewStyle().Foreground(ui.ColorGreen)

	if !strings.Contains(got, hostStyle.Render("edge01")) {
		t.Fatalf("banner missing colored host: %q", got)
	}
	if !strings.Contains(got, commandStyle.Render("show hostname")) {
		t.Fatalf("banner missing colored command: %q", got)
	}
	if !strings.Contains(lipgloss.NewStyle().Render(got), "edge01") {
		t.Fatalf("banner missing host text: %q", got)
	}
}
