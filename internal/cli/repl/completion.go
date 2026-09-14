package repl

import "strings"

// completeTargetToken completes only the host portion of the quoted target at
// cursor. cursor is a rune offset so it can be used directly with textinput.Position.
func completeTargetToken(line string, cursor int, candidates []string) (updated string, newCursor int, matches []string) {
	runes := []rune(line)
	if cursor < 0 || cursor > len(runes) {
		return line, cursor, nil
	}
	start, ok := activeTargetStart(runes, cursor)
	if !ok {
		return line, cursor, nil
	}
	tokenStart := start
	for i := start; i < cursor; i++ {
		if runes[i] == '@' {
			tokenStart = i + 1
		}
	}
	prefix := string(runes[tokenStart:cursor])
	if prefix == "" {
		return line, cursor, nil
	}
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix)) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return line, cursor, nil
	}
	replacement := matches[0]
	for _, candidate := range matches[1:] {
		replacement = commonPrefixFold(replacement, candidate)
	}
	out := make([]rune, 0, len(runes)-cursor+tokenStart+len([]rune(replacement)))
	out = append(out, runes[:tokenStart]...)
	out = append(out, []rune(replacement)...)
	out = append(out, runes[cursor:]...)
	return string(out), tokenStart + len([]rune(replacement)), matches
}
func activeTargetStart(runes []rune, cursor int) (int, bool) {
	inTargets, quoted, escaped := false, false, false
	open := 0
	for i := 0; i < cursor; i++ {
		r := runes[i]
		if escaped {
			escaped = false
			continue
		}
		if quoted && r == '\\' && i+1 < cursor && runes[i+1] == '\'' {
			escaped = true
			continue
		}
		if r == '\'' {
			quoted = !quoted
			if quoted {
				open = i + 1
			}
			continue
		}
		if quoted {
			continue
		}
		if r == '[' {
			inTargets = true
			continue
		}
		if r == ']' {
			inTargets = false
		}
	}
	return open, inTargets && quoted
}
func commonPrefixFold(left, right string) string {
	a, b := []rune(left), []rune(right)
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && strings.EqualFold(string(a[i]), string(b[i])) {
		i++
	}
	return string(a[:i])
}
