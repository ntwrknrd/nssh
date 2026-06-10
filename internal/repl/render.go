package repl

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ntwrknrd/nssh/internal/ui"
	"golang.org/x/term"
)

// RenderOutputBanner is scoped to REPL output. It is based on the old CLI banner
// ruler from e7df98f:internal/ui/components.go; caec758 removed those global
// command banners, and REPL should not restore them globally.
func RenderOutputBanner(host, command string) string {
	width := 80
	if w, _, err := term.GetSize(0); err == nil && w > 0 {
		width = w
	}
	return RenderOutputBannerForWidth(host, command, width)
}

func RenderOutputBannerForWidth(host, command string, width int) string {
	title := host + " | " + command
	if width <= 0 {
		width = 80
	}
	lineStyle := lipgloss.NewStyle().Foreground(ui.ColorDim)
	hostStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	separatorStyle := lipgloss.NewStyle().Foreground(ui.ColorDim)
	commandStyle := lipgloss.NewStyle().Foreground(ui.ColorGreen)
	if title == "" {
		return lineStyle.Render(strings.Repeat("-", width))
	}
	padded := " " + title + " "
	renderedTitle := " " + hostStyle.Render(host) + separatorStyle.Render(" | ") + commandStyle.Render(command) + " "
	remaining := width - lipgloss.Width(padded)
	if remaining < 2 {
		return hostStyle.Render(host) + separatorStyle.Render(" | ") + commandStyle.Render(command)
	}
	leftLen := remaining / 2
	rightLen := remaining - leftLen
	return lineStyle.Render(strings.Repeat("-", leftLen)) +
		renderedTitle +
		lineStyle.Render(strings.Repeat("-", rightLen))
}
