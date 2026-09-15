package repl

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpOverlayPreservesOutputAndCloses(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("a", "show", "retained output", 0))
	before := m.renderBlocks()
	m.input.SetValue(":help")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.helpOpen || m.renderBlocks() != before || len(m.entries) != 0 {
		t.Fatal("help changed transcript/history")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Help / command index") {
		t.Fatal("missing overlay")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.helpOffset == 0 {
		t.Fatal("help did not scroll")
	}
	next, _ = m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 1})
	m = next.(model)
	if m.selected {
		t.Fatal("overlay click selected underlying output")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.helpOpen || cmd != nil || m.renderBlocks() != before {
		t.Fatal("closing help changed session")
	}
}

func TestStatusHintAndHelpFitTerminal(t *testing.T) {
	for _, width := range []int{12, 60, 120} {
		m := testTUI(width)
		m.refreshLayout()
		view := ansi.Strip(m.View())
		rows := strings.Split(view, "\n")
		if !strings.HasSuffix(rows[len(rows)-1], ":help") || ansi.StringWidth(rows[len(rows)-1]) != width {
			t.Fatalf("status width %d: %q", width, rows[len(rows)-1])
		}
		if strings.Contains(view, "Enter run") {
			t.Fatal("old hint row retained")
		}
		m.helpOpen = true
		rows = strings.Split(m.View(), "\n")
		if len(rows) > m.height {
			t.Fatal("overlay exceeded terminal height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("overlay exceeded width %d: %q", width, row)
			}
		}
	}
}
