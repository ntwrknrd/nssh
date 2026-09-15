package repl

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The help pane overlays the existing view without adding transcript or history.
func (m model) helpSize() (int, int) {
	width := max(1, min(96, m.viewport.Width)-4)
	height := max(1, min(24, m.height)-4)
	return width, height
}
func (m model) helpLines() []string {
	width, _ := m.helpSize()
	return strings.Split(ansi.Hardwrap(replHelp, width, true), "\n")
}
func (m model) helpMaxOffset() int {
	_, height := m.helpSize()
	return max(0, len(m.helpLines())-height)
}
func (m model) helpOverlay(base string) string {
	width, height := m.helpSize()
	lines := m.helpLines()
	start := min(m.helpOffset, m.helpMaxOffset())
	body := append([]string{}, lines[start:min(len(lines), start+height)]...)
	for len(body) < height {
		body = append(body, "")
	}
	content := []string{ansi.Truncate("Help / command index", width, "")}
	content = append(content, body...)
	content = append(content, ansi.Truncate("Up/Down/PgUp/PgDn scroll | Esc/Enter close", width, ""))
	panel := lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Render(strings.Join(content, "\n"))
	overlay := strings.Split(panel, "\n")
	screen := strings.Split(base, "\n")
	x := max(0, (m.viewport.Width-lipgloss.Width(panel))/2)
	y := max(0, (len(screen)-len(overlay))/2)
	for i, line := range overlay {
		if y+i >= len(screen) {
			break
		}
		line = ansi.Truncate(line, max(1, m.viewport.Width-x), "")
		background := screen[y+i]
		screen[y+i] = padCells(ansi.Cut(background, 0, x), x) + line + ansi.Cut(background, x+ansi.StringWidth(line), ansi.StringWidth(background))
	}
	return strings.Join(screen, "\n")
}
