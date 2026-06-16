package ui

import "strings"

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
	code, comment := splitTOMLComment(body)
	idx := strings.IndexByte(code, ':')
	if idx < 0 {
		if strings.TrimSpace(code) == "" && comment != "" {
			return code + tomlCommentStyle.Render(comment) + ending
		}
		return body + ending
	}
	rendered := highlightTOMLKey(code[:idx]) + tomlEqualStyle.Render(":") + highlightTOMLValue(code[idx+1:])
	if comment != "" {
		rendered += tomlCommentStyle.Render(comment)
	}
	return rendered + ending
}
