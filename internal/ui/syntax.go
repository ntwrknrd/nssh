package ui

import (
	"strings"
	"unicode"
)

var (
	syntaxCommentStyle   = ansiStyle("2;37")
	syntaxKeyStyle       = ansiStyle("36")
	syntaxSeparatorStyle = ansiStyle("2;37")
	syntaxStringStyle    = ansiStyle("32")
	syntaxNumberStyle    = ansiStyle("33")
	syntaxPunctStyle     = ansiStyle("2;37")
)

type ansiStyle string

func (s ansiStyle) Render(text string) string {
	if text == "" {
		return ""
	}
	return "\x1b[" + string(s) + "m" + text + "\x1b[0m"
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

func splitYAMLComment(line string) (string, string) {
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

func highlightYAMLKey(keyPart string) string {
	leading := leadingWhitespace(keyPart)
	trailing := trailingWhitespace(keyPart)
	key := strings.TrimSpace(keyPart)
	if key == "" {
		return keyPart
	}
	return leading + syntaxKeyStyle.Render(key) + trailing
}

func scanQuotedString(s string, start int) int {
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
