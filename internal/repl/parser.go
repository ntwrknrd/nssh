package repl

import (
	"fmt"
	"strings"
)

type TargetKind int

const (
	TargetHost TargetKind = iota
	TargetSelector
)

type Request struct {
	Kind     TargetKind
	Target   string
	Targets  []string
	Command  string
	Commands []string
}

func ParseLine(line string) (Request, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Request{}, fmt.Errorf("empty input")
	}
	if !strings.HasPrefix(line, "[") {
		return Request{}, fmt.Errorf("target group must start with [")
	}
	targetBody, rest, ok := bracketBody(line, '[', ']')
	if !ok {
		return Request{}, fmt.Errorf("invalid target group")
	}
	targets, err := parseQuotedList(targetBody, "target")
	if err != nil {
		return Request{}, err
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "(") {
		return Request{}, fmt.Errorf("command group must start with (")
	}
	commandBody, rest, ok := bracketBody(rest, '(', ')')
	if !ok {
		return Request{}, fmt.Errorf("invalid command group")
	}
	if strings.TrimSpace(rest) != "" {
		return Request{}, fmt.Errorf("unexpected text after command group")
	}
	commands, err := parseQuotedList(commandBody, "command")
	if err != nil {
		return Request{}, err
	}
	target := targets[0]
	req := Request{Kind: TargetHost, Target: target, Targets: targets, Command: commands[0], Commands: commands}
	if selector, ok := strings.CutPrefix(target, "select:"); ok {
		if len(targets) > 1 {
			return Request{}, fmt.Errorf("selectors cannot be combined with multiple targets")
		}
		selector = strings.TrimSpace(selector)
		if selector == "" {
			return Request{}, fmt.Errorf("missing selector")
		}
		req.Kind = TargetSelector
		req.Target = selector
	}
	return req, nil
}

func bracketBody(value string, open, close byte) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value[0] != open {
		return "", "", false
	}
	inQuote := false
	escaped := false
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if escaped {
			escaped = false
			continue
		}
		if inQuote && ch == '\\' {
			escaped = true
			continue
		}
		if ch == '\'' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && ch == close {
			return value[1:i], value[i+1:], true
		}
	}
	return "", "", false
}

func parseQuotedList(value, name string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("missing %s", name)
	}
	var values []string
	for value != "" {
		if !strings.HasPrefix(value, "'") {
			return nil, fmt.Errorf("expected quoted %s", name)
		}
		var b strings.Builder
		closed := false
		for i := 1; i < len(value); i++ {
			ch := value[i]
			if ch == '\\' && i+1 < len(value) && value[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			if ch == '\'' {
				item := b.String()
				if strings.TrimSpace(item) == "" {
					return nil, fmt.Errorf("missing %s", name)
				}
				values = append(values, item)
				value = strings.TrimSpace(value[i+1:])
				closed = true
				break
			}
			b.WriteByte(ch)
		}
		if !closed {
			return nil, fmt.Errorf("unterminated quoted %s", name)
		}
		if value == "" {
			break
		}
		if !strings.HasPrefix(value, ",") {
			return nil, fmt.Errorf("expected comma between %ss", name)
		}
		value = strings.TrimSpace(strings.TrimPrefix(value, ","))
		if value == "" {
			return nil, fmt.Errorf("missing %s", name)
		}
	}
	return values, nil
}

func ExpandHostPattern(target string) ([]string, error) {
	start := strings.Index(target, "(")
	end := strings.LastIndex(target, ")")
	if start == -1 && end == -1 {
		return []string{target}, nil
	}
	if start == -1 || end == -1 || end < start || end != len(target)-1 {
		return nil, fmt.Errorf("invalid host expansion %q", target)
	}
	prefix := target[:start]
	body := target[start+1 : end]
	if prefix == "" || strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("invalid host expansion %q", target)
	}
	parts := strings.Split(body, ",")
	hosts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid host expansion %q", target)
		}
		hosts = append(hosts, prefix+part)
	}
	return hosts, nil
}

type TargetResolver interface {
	ResolveHost(host string) (string, error)
	SelectHosts(selector string) ([]string, error)
}

func ResolveTargets(req Request, resolver TargetResolver) ([]string, error) {
	if resolver == nil {
		return nil, fmt.Errorf("missing target resolver")
	}
	var targets []string
	switch req.Kind {
	case TargetSelector:
		hosts, err := resolver.SelectHosts(req.Target)
		if err != nil {
			return nil, err
		}
		targets = append(targets, hosts...)
	default:
		requested := req.Targets
		if len(requested) == 0 {
			requested = []string{req.Target}
		}
		for _, target := range requested {
			expanded, err := ExpandHostPattern(target)
			if err != nil {
				return nil, err
			}
			for _, host := range expanded {
				resolved, err := resolver.ResolveHost(host)
				if err != nil {
					return nil, err
				}
				targets = append(targets, resolved)
			}
		}
	}
	targets = dedupeStrings(targets)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets matched")
	}
	return targets, nil
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
