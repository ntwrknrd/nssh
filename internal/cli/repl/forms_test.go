package repl

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

func TestFormsSwitchAndAddRowsWithoutChangingSyntax(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'edge1' ] ( 'show version' )")
	m.input.SetCursor(8)
	before := m.input.Value()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.commandFocused() || m.input.Value() != before || m.active {
		t.Fatal("device Enter should only advance")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("show clock")})
	m = next.(model)
	sub, err := core.Parse(m.input.Value())
	if err != nil || len(sub.Commands) != 2 || sub.Commands[1] != "show clock" {
		t.Fatalf("row insertion: %q, %v", m.input.Value(), err)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(model)
	if m.commandFocused() || m.input.Position() != 3 {
		t.Fatal("Shift-Tab did not focus devices")
	}
	m.focusForm(true)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.commandFocused() {
		t.Fatal("Tab did not return to devices")
	}
}

func TestFormsStackedLayoutAndVisibleRows(t *testing.T) {
	m := testTUI(120)
	m.input.SetValue("[ 'edge1', 'edge2' ] ( 'one', 'two', 'three', 'four', 'five' )")
	m.focusForm(true)
	for i := 0; i < 4; i++ {
		m.moveFormRow(1)
	}
	for _, width := range []int{45, 120} {
		view := m.formsView(width)
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "Devices") || !strings.Contains(plain, "Commands (2-5/5)") || !strings.Contains(plain, "'five'") {
			t.Fatal(plain)
		}
		if lipgloss.Width(view) > width {
			t.Fatalf("overflow at %d", width)
		}
	}
	if lipgloss.Height(m.formsView(120)) != lipgloss.Height(m.formsView(45)) {
		t.Fatal("forms should stack at every width")
	}
	before := m.input.Value()
	m.formsView(120)
	if m.input.Value() != before {
		t.Fatal("render mutated submission")
	}
}

func TestFormsInternalCommandInput(t *testing.T) {
	m := testTUI(120)
	m.input.SetValue(":help")
	view := ansi.Strip(m.formsView(120))
	if !strings.Contains(view, ":help") || strings.Contains(view, "Devices") {
		t.Fatal(view)
	}
}

func TestFormsKeepCursorOutOfHiddenSyntax(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'edge' ] ( 'show version' )")
	m.input.CursorEnd()
	m.clampFormCursor()
	_, commands := m.formFields()
	if m.input.Position() != commands[0].end {
		t.Fatal("cursor hidden after history restore")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	if m.input.Position() != commands[0].end {
		t.Fatal("cursor escaped command field")
	}
}

func TestFormsAllowIncrementalExplicitSyntax(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	want := "[ 'edge' ] ( 'show version' )"
	for _, r := range want {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			key = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{r}}
		}
		next, _ := m.Update(key)
		m = next.(model)
	}
	if m.input.Value() != want {
		t.Fatalf("explicit syntax changed: %q", m.input.Value())
	}
}
