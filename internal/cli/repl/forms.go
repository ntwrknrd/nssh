package repl

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// Both forms edit ranges in the same submission, so history and parsing keep
// their existing contract. Only the display separates values into rows.
func (m model) formsView(width int) string {
	if m.input.Value() == "" {
		m.input.SetValue("[ '' ] ( '' )")
		m.input.SetCursor(3)
	}
	devices, commands := m.formFields()
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), ":") || len(devices) == 0 || len(commands) == 0 {
		return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Width(max(1, width-2)).Render(m.editorView(max(1, width-4)))
	}
	boxWidth := width
	render := func(title string, fields []editorField, focused bool) string {
		inner := max(1, boxWidth-4)
		rows := []string{title}
		current := 0
		for i, f := range fields {
			if m.input.Position() >= f.start && m.input.Position() <= f.end {
				current = i
			}
		}
		start := 0
		if focused {
			start = max(0, current-3)
		}
		value := []rune(m.input.Value())
		for i := start; i < min(len(fields), start+4); i++ {
			f := fields[i]
			first := f.start - 1
			if focused && i == current {
				for first < m.input.Position() && ansi.StringWidth(string(value[first:m.input.Position()])) > max(1, inner-3) {
					first++
				}
			}
			var row strings.Builder
			for pos := first; pos <= f.end; pos++ {
				style := lipgloss.NewStyle()
				if focused && pos == m.input.Position() {
					style = style.Reverse(true)
				}
				row.WriteString(style.Render(safeTerminalText(string(value[pos]))))
			}
			rows = append(rows, ansi.Truncate(row.String(), inner, ""))
		}
		if len(fields) > 4 {
			rows[0] += " (" + fmt.Sprintf("%d-%d/%d", start+1, min(len(fields), start+4), len(fields)) + ")"
		}
		border := lipgloss.Color("8")
		if focused {
			border = lipgloss.Color("81")
		}
		return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Width(max(1, boxWidth-2)).Render(strings.Join(rows, "\n"))
	}
	left := render("Devices", devices, !m.commandFocused())
	right := render("Commands", commands, m.commandFocused())
	return left + "\n" + right
}

func (m *model) moveFormRow(delta int) bool {
	devices, commands := m.formFields()
	fields := devices
	if m.commandFocused() {
		fields = commands
	}
	if len(fields) < 2 {
		return false
	}
	for i, field := range fields {
		if m.input.Position() >= field.start && m.input.Position() <= field.end {
			target := fields[max(0, min(len(fields)-1, i+delta))]
			m.input.SetCursor(min(target.end, target.start+m.input.Position()-field.start))
			return true
		}
	}
	return false
}

// Hidden submission delimiters are not cursor destinations in the forms.
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
func (m *model) resetInput() {
	if m.configMode && m.configDraft != "" {
		m.input.SetValue(m.configDraft)
		m.focusForm(true)
	} else {
		m.input.SetValue("")
	}
}
