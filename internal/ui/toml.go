package ui

import (
	"strings"
	"unicode"
)

var (
	tomlTableStyle   = ansiStyle("1;35")
	tomlCommentStyle = ansiStyle("2;37")
	tomlKeyStyle     = ansiStyle("36")
	tomlEqualStyle   = ansiStyle("2;37")
	tomlStringStyle  = ansiStyle("32")
	tomlNumberStyle  = ansiStyle("33")
	tomlPunctStyle   = ansiStyle("2;37")
)

type ansiStyle string

func (s ansiStyle) Render(text string) string {
	if text == "" {
		return ""
	}
	return "\x1b[" + string(s) + "m" + text + "\x1b[0m"
}

// HighlightTOML renders TOML with ANSI syntax color for terminal output.
func HighlightTOML(text string) string {
	if text == "" {
		return ""
	}

	var sb strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		sb.WriteString(highlightTOMLLine(line))
	}
	return sb.String()
}

func highlightTOMLLine(line string) string {
	body, ending := splitLineEnding(line)
	code, comment := splitTOMLComment(body)
	trimmed := strings.TrimSpace(code)

	var rendered string
	switch {
	case trimmed == "" && comment != "":
		rendered = code + tomlCommentStyle.Render(comment)
	case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
		rendered = highlightTOMLTable(code)
	default:
		rendered = highlightTOMLAssignment(code)
	}

	if comment != "" && trimmed != "" {
		rendered += tomlCommentStyle.Render(comment)
	}
	return rendered + ending
}

func splitLineEnding(line string) (string, string) {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	case strings.HasSuffix(line, "\n"):
		return strings.TrimSuffix(line, "\n"), "\n"
	default:
		return line, ""
	}
}

func splitTOMLComment(line string) (string, string) {
	inString := false
	var quote byte
	escaped := false

	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		if c == '"' || c == '\'' {
			inString = true
			quote = c
			continue
		}
		if c == '#' {
			return line[:i], line[i:]
		}
	}
	return line, ""
}

func highlightTOMLTable(code string) string {
	leading := leadingWhitespace(code)
	trailing := trailingWhitespace(code)
	trimmed := strings.TrimSpace(code)
	return leading + tomlTableStyle.Render(trimmed) + trailing
}

func highlightTOMLAssignment(code string) string {
	idx := strings.IndexByte(code, '=')
	if idx < 0 {
		return code
	}

	keyPart := code[:idx]
	valuePart := code[idx+1:]
	return highlightTOMLKey(keyPart) + tomlEqualStyle.Render("=") + highlightTOMLValue(valuePart)
}

func highlightTOMLKey(keyPart string) string {
	leading := leadingWhitespace(keyPart)
	trailing := trailingWhitespace(keyPart)
	key := strings.TrimSpace(keyPart)
	if key == "" {
		return keyPart
	}
	return leading + tomlKeyStyle.Render(key) + trailing
}

func highlightTOMLValue(value string) string {
	var sb strings.Builder
	for i := 0; i < len(value); {
		c := value[i]
		switch {
		case c == '"' || c == '\'':
			end := scanTOMLString(value, i)
			sb.WriteString(tomlStringStyle.Render(value[i:end]))
			i = end
		case isTOMLNumberStart(value, i):
			end := scanWhile(value, i, func(r rune) bool {
				return unicode.IsDigit(r) || r == '+' || r == '-' || r == '_' || r == '.' || r == 'e' || r == 'E'
			})
			sb.WriteString(tomlNumberStyle.Render(value[i:end]))
			i = end
		case isTOMLIdentStart(c):
			end := scanWhile(value, i, func(r rune) bool {
				return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
			})
			token := value[i:end]
			if token == "true" || token == "false" || token == "inf" || token == "nan" {
				sb.WriteString(tomlNumberStyle.Render(token))
			} else {
				sb.WriteString(token)
			}
			i = end
		case strings.ContainsRune("[]{}.,", rune(c)):
			sb.WriteString(tomlPunctStyle.Render(string(c)))
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

func scanTOMLString(s string, start int) int {
	quote := s[start]
	escaped := false
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quote == '"' {
			escaped = true
			continue
		}
		if c == quote {
			return i + 1
		}
	}
	return len(s)
}

func isTOMLNumberStart(s string, i int) bool {
	c := s[i]
	if c >= '0' && c <= '9' {
		return true
	}
	if (c == '+' || c == '-') && i+1 < len(s) {
		next := s[i+1]
		return next >= '0' && next <= '9'
	}
	return false
}

func isTOMLIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func scanWhile(s string, start int, keep func(rune) bool) int {
	for i, r := range s[start:] {
		if !keep(r) {
			return start + i
		}
	}
	return len(s)
}

func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeftFunc(s, unicode.IsSpace))]
}

func trailingWhitespace(s string) string {
	return s[len(strings.TrimRightFunc(s, unicode.IsSpace)):]
}
