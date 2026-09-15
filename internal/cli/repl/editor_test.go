package repl

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditorDeleteToStartPreservesScaffold(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'edge1', 'edge2' ] ( 'show version' )")
	m.input.SetCursor(len([]rune("[ 'edge1', 'edge2' ] ( 'show")))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(model)
	if got := m.input.Value(); got != "[ 'edge1', 'edge2' ] ( ' version' )" {
		t.Fatal(got)
	}
	m.input.SetCursor(len([]rune("[ 'edge1")))
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(model)
	if got := m.input.Value(); got != "[ '', 'edge2' ] ( ' version' )" {
		t.Fatal(got)
	}
}

func TestEditorCannotDeleteScaffold(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyBackspace}, {Type: tea.KeyDelete}, {Type: tea.KeyCtrlU}, {Type: tea.KeyCtrlW}, {Type: tea.KeyBackspace, Alt: true}, {Type: tea.KeyDelete, Alt: true}} {
		for pos := 0; pos <= len("[ '' ] ( '' )"); pos++ {
			m := testTUI(120)
			m.input.Focus()
			m.input.SetValue("[ '' ] ( '' )")
			m.input.SetCursor(pos)
			next, _ := m.Update(key)
			m = next.(model)
			if got := m.input.Value(); got != "[ '' ] ( '' )" {
				t.Fatalf("key=%v cursor=%d: %q", key, pos, got)
			}
		}
	}
}

func TestEditorColonEntersControlModeAfterDeletingHost(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'edge' ] ( '' )")
	m.input.SetCursor(7)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":help")})
	m = next.(model)
	if m.input.Value() != ":help" {
		t.Fatal(m.input.Value())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(model)
	if m.input.Value() != ":hel" {
		t.Fatal("control mode deletion was protected")
	}
}

func TestEditorDeletionKeepsQuotedCommandStructure(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'a' ] ( 'echo \\'quoted\\'' )")
	before := m.input.Value()
	m.input.SetCursor(len([]rune("[ 'a' ] ( 'echo \\")))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(model)
	if m.input.Value() != before {
		t.Fatal("deleting escape damaged quoted structure")
	}
}

func TestTabCompletedTargetRestartsPicker(t *testing.T) {
	m := testTUI(120)
	m.candidates = []string{"edge1", "edge10"}
	m.input.SetValue("[ 'ops@edge1' ] ( 'show version' )")
	m.input.SetCursor(len([]rune("[ 'ops@edge1")))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if !m.pickerOpen || m.input.Value() != "[ 'ops@' ] ( 'show version' )" || len(m.matches) != 2 {
		t.Fatalf("Tab transition: %q at %d, picker=%v", m.input.Value(), m.input.Position(), m.pickerOpen)
	}
}
