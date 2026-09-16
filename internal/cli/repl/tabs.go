package repl

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func terminalMessageGroup(msg tea.Msg) int {
	switch v := msg.(type) {
	case terminalTargetsMsg:
		return v.group
	case terminalStartedMsg:
		return v.group
	case terminalOutputMsg:
		return v.group
	case terminalClosedMsg:
		return v.group
	case terminalCopyMsg:
		return v.group
	}
	return 0
}
func (m *model) saveTab() {
	if m.group == 0 {
		return
	}
	for i := range m.tabs {
		if m.tabs[i].group == m.group {
			m.tabs[i] = m.terminalGroup
			return
		}
	}
	m.tabs = append(m.tabs, m.terminalGroup)
}
func (m *model) switchTab(index int) {
	m.saveTab()
	if index < 0 || index >= len(m.tabs) {
		m.message = "Use :tab N with a tab number"
		return
	}
	m.terminalGroup = m.tabs[index]
	m.interactive, m.choosing, m.controlOpen = true, false, false
	m.input.SetValue("")
	m.message = ""
	m.resizePanes()
}
func (m *model) chooseTab() {
	m.saveTab()
	if len(m.tabs) >= 8 {
		m.message = "Close a tab before opening another (maximum 8)"
		return
	}
	m.terminalGroup = terminalGroup{target: -1}
	m.interactive, m.choosing, m.controlOpen = true, true, false
	m.input.SetValue("[ '' ] ( '' )")
	m.input.SetCursor(3)
	m.openPicker()
	m.refreshLayout()
}
func (m *model) closePanes() {
	m.saveTab()
	for _, tab := range m.tabs {
		m.terminalGroup = tab
		m.closeCurrentTab()
	}
	m.tabs = nil
	m.terminalGroup = terminalGroup{}
}
func (m *model) removeTab() {
	id := m.group
	m.closeCurrentTab()
	for i, tab := range m.tabs {
		if tab.group == id {
			m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
			break
		}
	}
	if len(m.tabs) > 0 {
		m.switchTab(len(m.tabs) - 1)
	} else {
		m.returnToBatch()
	}
}
func (m *model) returnToBatch() {
	m.saveTab()
	m.interactive, m.choosing, m.controlOpen = false, false, false
	m.input.SetValue(m.batchDraft)
	m.refreshLayout()
}
func (m model) tabTitles() []string {
	tabs := append([]terminalGroup(nil), m.tabs...)
	found := false
	for i := range tabs {
		if tabs[i].group == m.group {
			tabs[i] = m.terminalGroup
			found = true
		}
	}
	if !found && m.group != 0 {
		tabs = append(tabs, m.terminalGroup)
	}
	var labels []string
	for i, tab := range tabs {
		marker := " "
		if tab.group == m.group {
			marker = "*"
		}
		labels = append(labels, fmt.Sprintf("[%sTab %d] ", marker, i+1))
	}
	return labels
}
func (m model) tabBar() string {
	return ansi.Truncate(strings.Join(m.tabTitles(), ""), max(1, m.width), "...")
}
func (m *model) clickTab(x int) {
	offset := 0
	for i, label := range m.tabTitles() {
		offset += ansi.StringWidth(label)
		if x < offset {
			m.switchTab(i)
			return
		}
	}
}
func (m model) updateTerminalControl(msg tea.Msg) (model, tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.controlInput, cmd = m.controlInput.Update(msg)
		return m, cmd, true
	}
	if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlP {
		m.controlOpen = false
		m.controlInput.SetValue("")
		return m, nil, true
	}
	if key.Type != tea.KeyEnter {
		var cmd tea.Cmd
		m.controlInput, cmd = m.controlInput.Update(msg)
		return m, cmd, true
	}
	line := strings.TrimPrefix(strings.TrimSpace(m.controlInput.Value()), ":")
	m.controlInput.SetValue("")
	if m.active && line != "help" && line != "clear" && line != "copy" && line != "quit" && line != "" {
		m.message = "Wait for the batch request to finish"
		return m, nil, true
	}
	switch {
	case line == "interactive":
		if !m.interactive {
			m.batchDraft = m.input.Value()
		}
		if m.group != 0 || len(m.tabs) > 0 {
			m.saveTab()
			m.switchTab(len(m.tabs) - 1)
		} else {
			m.chooseTab()
		}
	case line == "wipe" && !m.interactive:
		if err := m.history.clear(); err != nil {
			m.message = "history wipe failed: " + displayLabel(err.Error())
			return m, nil, true
		}
		m.entries = nil
		m.historyAt = 0
		m.clearScrollback()
		m.controlOpen = false
	case line == "stacked" && !m.interactive:
		m.stacked = !m.stacked
		m.selected = false
		m.refreshLayout()
		m.controlOpen = false
	case line == "":
		m.controlOpen = false
	case line == "new":
		if !m.interactive {
			m.batchDraft = m.input.Value()
		}
		m.chooseTab()
	case strings.HasPrefix(line, "tab "):
		if !m.interactive {
			m.batchDraft = m.input.Value()
		}
		var n int
		if _, err := fmt.Sscanf(line, "tab %d", &n); err != nil {
			m.message = "Use :tab N"
		} else {
			m.switchTab(n - 1)
		}
	case (line == "close" || line == "disconnect") && m.interactive:
		m.removeTab()
	case line == "batch":
		if m.interactive {
			m.returnToBatch()
		} else {
			m.controlOpen = false
		}
	case line == "quit" || line == "exit":
		m.closePanes()
		return m, tea.Quit, true
	case line == "help":
		m.controlOpen = false
		m.helpOpen = true
		m.helpOffset = 0
	case (line == "reconnect" || strings.HasPrefix(line, "reconnect ")) && m.interactive:
		index := -1
		if line != "reconnect" {
			var n int
			if _, err := fmt.Sscanf(line, "reconnect %d", &n); err != nil || n < 1 || n > len(m.panes) {
				m.message = "Use :reconnect or :reconnect N"
				return m, nil, true
			}
			index = n - 1
		}
		m.reconnectTerminals(index)
		m.controlOpen = false
	case line == "scroll-lock" && m.interactive:
		m.independentScroll = !m.independentScroll
		m.controlOpen = false
	case line == "all" && m.interactive:
		m.target = -1
		m.broadcastPaused = false
		m.controlOpen = false
		m.message = ""
	case strings.HasPrefix(line, "target ") && m.interactive:
		var n int
		if _, err := fmt.Sscanf(line, "target %d", &n); err != nil || n < 1 || n > len(m.panes) {
			m.message = "Use :target N with a pane number"
		} else {
			m.target = n - 1
			m.page = (n - 1) / 4
			m.controlOpen = false
			m.message = ""
		}
	case line == "next" && m.interactive:
		m.page = min(max(0, (len(m.panes)-1)/4), m.page+1)
		m.controlOpen = false
	case line == "prev" && m.interactive:
		m.page = max(0, m.page-1)
		m.controlOpen = false
	case line == "clear":
		m.clearDisplay()
		m.controlOpen = false
	case line == "copy":
		if !m.interactive {
			return m, m.copySelection(true), true
		}
		return m, m.copyTerminalSelection(true), true
	default:
		m.message = "Unknown control; use the index above"
	}
	return m, nil, true
}
func (m model) terminalControlView(base string) string {
	width := max(1, min(70, m.width-4))
	input := m.controlInput
	input.Placeholder = ":help"
	input.Width = max(1, width-4)
	text := "Local controls - input stays here\n\n:interactive  Open or resume sessions\n:new       Open a new session tab\n:tab N     Switch tab (Alt-Left/Right cycles)\n:close     Disconnect this tab\n:reconnect [N]  Reconnect (Ctrl+R: all closed panes)\n:scroll-lock   Toggle synchronized scrolling\n:target N  Focus one device\n:all       Broadcast to all devices\n:next / :prev  Change pane page\n:clear     Clear scrollback (Ctrl+K / Ctrl+L)\n:wipe      Clear batch output and history\n:stacked   Toggle stacked batch results\n:copy      Copy selection\n:batch     Return to batch; keep tabs connected\n:help      Full help\n:quit      Close every session and exit\n\n> " + input.View() + "\nEsc / Ctrl+P closes controls"
	lines := strings.Split(ansi.Hardwrap(text, width, true), "\n")
	if len(lines) > max(4, m.height-4) {
		lines = append(lines[:max(1, m.height-8)], ":help lists all controls", "> "+input.View(), "Esc / Ctrl+P returns")
	}
	panel := lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).Render(strings.Join(lines, "\n"))
	screen := strings.Split(base, "\n")
	overlay := strings.Split(panel, "\n")
	x, y := max(0, (m.width-lipgloss.Width(panel))/2), max(0, (len(screen)-len(overlay))/2)
	for i, line := range overlay {
		if y+i >= len(screen) {
			break
		}
		line = ansi.Truncate(line, max(1, m.width-x), "")
		bg := screen[y+i]
		screen[y+i] = padCells(ansi.Cut(bg, 0, x), x) + line + ansi.Cut(bg, x+ansi.StringWidth(line), ansi.StringWidth(bg))
	}
	return strings.Join(screen, "\n")
}

func (m *model) clearDisplay() {
	if m.interactive && !m.choosing {
		for _, p := range m.panes {
			p.clearScrollback()
		}
		m.message = ""
	} else {
		m.clearScrollback()
	}
}

func (m *model) cycleTab(delta int) {
	m.saveTab()
	if len(m.tabs) < 2 {
		return
	}
	index := 0
	for i, tab := range m.tabs {
		if tab.group == m.group {
			index = i
			break
		}
	}
	m.switchTab((index + delta + len(m.tabs)) % len(m.tabs))
}
