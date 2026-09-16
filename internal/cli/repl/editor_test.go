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

func TestEditorColonRemainsFieldTextAfterDeletingHost(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'edge' ] ( '' )")
	m.input.SetCursor(7)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":help")})
	m = next.(model)
	if m.input.Value() != "[ ':help' ] ( '' )" {
		t.Fatal(m.input.Value())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(model)
	if m.input.Value() != "[ ':hel' ] ( '' )" {
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
	if !m.pickerOpen || m.input.Value() != "[ 'ops@edge1' ] ( 'show version' )" || m.pickerFilter.Value() != "edge1" || len(m.matches) != 2 {
		t.Fatalf("Tab transition: %q at %d, picker=%v", m.input.Value(), m.input.Position(), m.pickerOpen)
	}
}

func TestArrowNavigationEditsBothDevicesFromCommand(t *testing.T) {
	m := testTUI(120)
	m.input.Focus()
	m.input.SetValue("[ 'acm-eec-comp-sw4.custcbb.local', 'acm-eec-comp-sw5.custcbb.local' ] ( 'show int status' )")
	m.focusForm(true)
	for _, replacement := range []string{"7", "6"} {
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyLeft})
		fields, _ := m.formFields()
		expected := fields[len(fields)-1].end
		if replacement == "6" {
			expected = fields[0].end
		}
		if m.input.Position() != expected {
			t.Fatal("Left did not jump to previous field end")
		}
		for range ".custcbb.local" {
			m = testUpdate(m, tea.KeyMsg{Type: tea.KeyLeft})
		}
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyBackspace})
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(replacement)})
		fields, _ = m.formFields()
		if replacement == "7" {
			for m.input.Position() > fields[1].start {
				m = testUpdate(m, tea.KeyMsg{Type: tea.KeyLeft})
			}
		}
	}
	want := "[ 'acm-eec-comp-sw6.custcbb.local', 'acm-eec-comp-sw7.custcbb.local' ] ( 'show int status' )"
	if m.input.Value() != want {
		t.Fatal(m.input.Value())
	}
	fields, _ := editorFields(want)
	for i := 0; i < len(fields)-1; i++ {
		m.input.SetCursor(fields[i].end)
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRight})
		if m.input.Position() != fields[i+1].start {
			t.Fatal("Right did not skip syntax")
		}
	}
}

func TestArrowNavigationTraversesEmptyAndUnicodeFields(t *testing.T) {
	m := testTUI(120)
	m.input.SetValue("[ '機器', '' ] ( '', 'show' )")
	fields, _ := editorFields(m.input.Value())
	for i := 0; i < len(fields)-1; i++ {
		m.input.SetCursor(fields[i].end)
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRight})
		if m.input.Position() != fields[i+1].start {
			t.Fatal("right skipped an empty field")
		}
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyLeft})
		if m.input.Position() != fields[i].end {
			t.Fatal("left skipped an empty field")
		}
	}
}
