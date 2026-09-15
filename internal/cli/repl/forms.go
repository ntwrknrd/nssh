package repl

import (
	"github.com/charmbracelet/lipgloss"
)

func (m model) formFields() (devices, commands []editorField) {
	value := []rune(m.input.Value())
	fields, _ := editorFields(string(value))
	for _, field := range fields {
		if _, target := activeTargetStart(value, field.start); target {
			devices = append(devices, field)
		} else {
			commands = append(commands, field)
		}
	}
	return
}

func (m model) commandFocused() bool {
	_, commands := m.formFields()
	return len(commands) > 0 && m.input.Position() >= commands[0].start-1
}

func (m *model) focusForm(command bool) {
	if m.input.Value() == "" {
		m.input.SetValue("[ '' ] ( '' )")
	}
	devices, commands := m.formFields()
	fields := devices
	if command {
		fields = commands
	}
	if len(fields) > 0 {
		m.input.SetCursor(fields[0].start)
	}
}

func (m *model) addFormRow() {
	devices, commands := m.formFields()
	fields := devices
	if m.commandFocused() {
		fields = commands
	}
	if len(fields) == 0 {
		return
	}
	field := fields[len(fields)-1]
	for _, f := range fields {
		if m.input.Position() >= f.start && m.input.Position() <= f.end {
			field = f
			break
		}
	}
	value := []rune(m.input.Value())
	at := field.end + 1
	m.input.SetValue(string(value[:at]) + ", ''" + string(value[at:]))
	m.input.SetCursor(at + 3)
}

// Batch has one editable request, including its targets and commands.
func (m model) formsView(width int) string {
	if m.interactive && m.choosing {
		devices, _ := m.formFields()
		if len(devices) > 0 {
			value := []rune(m.input.Value())
			m.input.SetValue(string(value[:devices[len(devices)-1].end+1]) + " ]")
			m.input.Placeholder = "Choose devices"
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Width(max(1, width-2)).Render(m.editorView(max(1, width-4)))
}

// Keep navigation inside the editable values of the batch request.
func (m *model) clampFormCursor() {
	devices, commands := m.formFields()
	if len(devices) == 0 || len(commands) == 0 {
		return
	}
	pos := m.input.Position()
	closest, distance := pos, len([]rune(m.input.Value()))+1
	for _, f := range append(devices, commands...) {
		at := max(f.start, min(f.end, pos))
		delta := at - pos
		if delta < 0 {
			delta = -delta
		}
		if delta < distance {
			closest, distance = at, delta
		}
	}
	m.input.SetCursor(closest)
}

func (m model) emptyCommandField() bool {
	if !m.commandFocused() {
		return false
	}
	_, commands := m.formFields()
	for _, field := range commands {
		if field.start == field.end && m.input.Position() == field.start {
			return true
		}
	}
	return false
}
func (m *model) resetInput() { m.input.SetValue("") }
