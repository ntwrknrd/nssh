package repl

import "strings"

type HostSuggester interface {
	SuggestHosts(prefix string) ([]string, error)
}

type TargetCompletion struct {
	Input       string
	Matches     []string
	Completed   bool
	SingleMatch bool
}

func SuggestTargetInputs(line string, suggester HostSuggester, limit int) ([]string, error) {
	prefix, ok := targetCompletionPrefix(line)
	if suggester == nil || !ok {
		return nil, nil
	}
	if prefix == "" || strings.HasPrefix(prefix, "select:") {
		return nil, nil
	}
	hosts, err := suggester.SuggestHosts(prefix)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 8
	}
	seen := make(map[string]bool, len(hosts))
	suggestions := make([]string, 0, min(limit, len(hosts)))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		suggestions = append(suggestions, host)
		if len(suggestions) >= limit {
			break
		}
	}
	return suggestions, nil
}

func CompleteTargetInput(line string, suggester HostSuggester) (TargetCompletion, error) {
	result := TargetCompletion{Input: line}
	start, end, ok := targetCompletionSpan(line)
	if !ok || suggester == nil {
		return result, nil
	}
	prefix := line[start:end]
	hosts, err := suggester.SuggestHosts(prefix)
	if err != nil {
		return result, err
	}
	hosts = dedupeStrings(hosts)
	result.Matches = hosts
	switch len(hosts) {
	case 0:
		return result, nil
	case 1:
		result.Input = line[:start] + hosts[0] + line[end:]
		result.Completed = result.Input != line
		result.SingleMatch = true
		return result, nil
	default:
		common := longestCommonPrefix(hosts)
		if len(common) > len(prefix) {
			result.Input = line[:start] + common + line[end:]
			result.Completed = true
		}
		return result, nil
	}
}

func targetCompletionPrefix(line string) (string, bool) {
	start, end, ok := targetCompletionSpan(line)
	if !ok {
		return "", false
	}
	return line[start:end], true
}

func targetCompletionSpan(line string) (int, int, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return 0, 0, false
	}
	close := strings.Index(line, "]")
	if close < 0 || strings.TrimSpace(line[close+1:]) != "( '' )" {
		return 0, 0, false
	}
	body := line[1:close]
	trimmed := strings.TrimSpace(body)
	offset := strings.Index(body, trimmed)
	if offset < 0 || !strings.HasPrefix(trimmed, "'") || !strings.HasSuffix(trimmed, "'") || strings.Count(trimmed, "'") != 2 {
		return 0, 0, false
	}
	start := 1 + offset + 1
	end := 1 + offset + len(trimmed) - 1
	return start, end, true
}

func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
