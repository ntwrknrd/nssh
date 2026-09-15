package repl

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type editorField struct{ start, end int }

// Return quoted value ranges and the surrounding structure. Quotes escaped
// inside values are content, so deleting one cannot turn it into a delimiter.
func editorFields(value string) ([]editorField, string) {
	runes := []rune(value)
	var fields []editorField
	var structure strings.Builder
	for i := 0; i < len(runes); i++ {
		structure.WriteRune(runes[i])
		if runes[i] != '\'' {
			continue
		}
		start := i + 1
		for i++; i < len(runes); i++ {
			if runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '\'' {
				i++
				continue
			}
			if runes[i] == '\'' {
				fields = append(fields, editorField{start: start, end: i})
				structure.WriteRune('\'')
				break
			}
		}
	}
	return fields, structure.String()
}

func emptyEditor(value string) bool {
	if value == "" {
		return true
	}
	fields, structure := editorFields(value)
	if structure != "[ '' ] ( '' )" {
		return false
	}
	for _, field := range fields {
		if field.start != field.end {
			return false
		}
	}
	return len(fields) == 2
}

// Run destructive editing against the current quoted value only. Bubbles
// retains ownership of word/character deletion semantics; scaffold stays intact.
func (m *model) deleteEditorField(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !strings.HasPrefix(strings.TrimSpace(m.input.Value()), "[") {
		return nil, false
	}
	bindings := m.input.KeyMap
	if !key.Matches(msg, bindings.DeleteBeforeCursor, bindings.DeleteAfterCursor, bindings.DeleteWordBackward, bindings.DeleteWordForward, bindings.DeleteCharacterBackward, bindings.DeleteCharacterForward) {
		return nil, false
	}
	value := []rune(m.input.Value())
	fields, structure := editorFields(string(value))
	cursor := m.input.Position()
	for _, field := range fields {
		if cursor < field.start || cursor > field.end {
			continue
		}
		input := m.input
		input.SetValue(string(value[field.start:field.end]))
		input.SetCursor(cursor - field.start)
		updated, cmd := input.Update(msg)
		next := string(value[:field.start]) + updated.Value() + string(value[field.end:])
		_, nextStructure := editorFields(next)
		if nextStructure == structure {
			m.input.SetValue(next)
			m.input.SetCursor(field.start + updated.Position())
		}
		return cmd, true
	}
	// A cursor on a bracket, parenthesis, separator or opening quote cannot
	// erase it. Move into a value to edit that value.
	return nil, true
}

// Reopening completion on an exact device starts a fresh filter, preserving
// an explicit username. The picker draft still supports Escape restoration.
func (m *model) resetCompletedTarget() {
	value := []rune(m.input.Value())
	fields, _ := editorFields(string(value))
	for _, f := range fields {
		if m.input.Position() < f.start || m.input.Position() > f.end {
			continue
		}
		host := string(value[f.start:f.end])
		user := ""
		if at := strings.LastIndex(host, "@"); at >= 0 {
			user, host = host[:at+1], host[at+1:]
		}
		for _, candidate := range m.candidates {
			if strings.EqualFold(host, candidate) {
				m.input.SetValue(string(value[:f.start]) + user + string(value[f.end:]))
				m.input.SetCursor(f.start + len([]rune(user)))
				return
			}
		}
	}
}
