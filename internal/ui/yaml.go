package ui

import (
	"strconv"
	"strings"
	"unicode"
)

// HighlightYAML renders YAML with light ANSI syntax color for terminal output.
func HighlightYAML(text string) string {
	if text == "" {
		return ""
	}

	var sb strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		sb.WriteString(highlightYAMLLine(line))
	}
	return sb.String()
}

func highlightYAMLLine(line string) string {
	body, ending := splitLineEnding(line)
	code, comment := splitYAMLComment(body)
	idx := strings.IndexByte(code, ':')
	if idx < 0 {
		rendered := highlightYAMLListItem(code)
		if rendered == code && strings.TrimSpace(code) == "" && comment != "" {
			rendered = code
		}
		if comment != "" {
			rendered += syntaxCommentStyle.Render(comment)
		}
		return rendered + ending
	}
	rendered := highlightYAMLKey(code[:idx]) + syntaxSeparatorStyle.Render(":") + highlightYAMLValue(code[idx+1:])
	if comment != "" {
		rendered += syntaxCommentStyle.Render(comment)
	}
	return rendered + ending
}

func highlightYAMLListItem(code string) string {
	leading := leadingWhitespace(code)
	trimmed := strings.TrimLeftFunc(code, unicode.IsSpace)
	if !strings.HasPrefix(trimmed, "-") {
		return code
	}
	rest := strings.TrimPrefix(trimmed, "-")
	return leading + syntaxPunctStyle.Render("-") + highlightYAMLValue(rest)
}

func highlightYAMLValue(value string) string {
	leading := leadingWhitespace(value)
	trailing := trailingWhitespace(value)
	scalar := strings.TrimSpace(value)
	if scalar == "" {
		return value
	}
	return leading + highlightYAMLScalar(scalar) + trailing
}

func highlightYAMLScalar(value string) string {
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
		return highlightYAMLFlow(value)
	}
	if value[0] == '"' || value[0] == '\'' {
		return syntaxStringStyle.Render(value)
	}
	if isYAMLBoolNull(value) || isYAMLNumber(value) {
		return syntaxNumberStyle.Render(value)
	}
	return syntaxStringStyle.Render(value)
}

func highlightYAMLFlow(value string) string {
	var sb strings.Builder
	for i := 0; i < len(value); {
		c := value[i]
		switch {
		case c == '"' || c == '\'':
			end := scanQuotedString(value, i)
			sb.WriteString(syntaxStringStyle.Render(value[i:end]))
			i = end
		case strings.ContainsRune("[]{}:,", rune(c)):
			sb.WriteString(syntaxPunctStyle.Render(string(c)))
			i++
		case unicode.IsSpace(rune(c)):
			sb.WriteByte(c)
			i++
		default:
			end := scanWhile(value, i, func(r rune) bool {
				return !unicode.IsSpace(r) && !strings.ContainsRune("[]{}:,", r)
			})
			sb.WriteString(highlightYAMLScalar(value[i:end]))
			i = end
		}
	}
	return sb.String()
}

func isYAMLBoolNull(value string) bool {
	switch value {
	case "true", "false", "null", "Null", "NULL", "~":
		return true
	default:
		return false
	}
}

func isYAMLNumber(value string) bool {
	if strings.ContainsFunc(value, unicode.IsLetter) {
		return false
	}
	normalized := strings.ReplaceAll(value, "_", "")
	if normalized == "" {
		return false
	}
	_, err := strconv.ParseFloat(normalized, 64)
	return err == nil
}
