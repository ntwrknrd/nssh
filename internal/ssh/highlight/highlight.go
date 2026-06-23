package highlight

import "time"

const (
	ProfileNone  = "none"
	ProfileJunos = "junos"

	maxLineLength       = 8192
	rateWindow          = 100 * time.Millisecond
	rateBypassDuration  = 250 * time.Millisecond
	rateWindowByteLimit = 64 << 20
)

type Options struct {
	Enabled bool
	Profile string
}

type Style uint8

const (
	StyleRed Style = iota + 1
	StyleYellow
	StyleGreen
	StyleCyan
	StyleBlue
	StyleMagenta
)

type Span struct {
	Start int
	End   int
	Style Style
}

type Profile interface {
	Scan(line []byte) []Span
}

type Highlighter struct {
	profile Profile

	windowStart time.Time
	windowBytes int
	bypassUntil time.Time
}

type JunosProfile struct{}

func New(options Options) *Highlighter {
	if !options.Enabled {
		return nil
	}
	switch options.Profile {
	case ProfileJunos:
		return &Highlighter{profile: JunosProfile{}}
	default:
		return nil
	}
}

func (h *Highlighter) Highlight(data []byte) []byte {
	if h == nil || h.profile == nil || len(data) == 0 {
		return data
	}
	if h.shouldBypassRate(len(data)) || containsUnsafe(data) {
		return data
	}

	var out []byte
	lineStart := 0
	for lineStart < len(data) {
		lineEnd := lineStart
		for lineEnd < len(data) && data[lineEnd] != '\n' {
			lineEnd++
		}
		if lineEnd < len(data) {
			lineEnd++
		}
		line := data[lineStart:lineEnd]
		var spans []Span
		if len(line) <= maxLineLength {
			var stack [64]Span
			spans = scanJunos(line, stack[:0])
		}
		if len(spans) > 0 && out == nil {
			out = make([]byte, 0, len(data)+len(spans)*12)
			out = append(out, data[:lineStart]...)
		}
		if out != nil {
			if len(spans) > 0 {
				out = appendHighlightedLine(out, line, spans)
			} else {
				out = append(out, line...)
			}
		}
		lineStart = lineEnd
	}
	if out == nil {
		return data
	}
	return out
}

func (h *Highlighter) shouldBypassRate(n int) bool {
	now := time.Now()
	if now.Before(h.bypassUntil) {
		return true
	}
	if h.windowStart.IsZero() || now.Sub(h.windowStart) > rateWindow {
		h.windowStart = now
		h.windowBytes = n
		return false
	}
	h.windowBytes += n
	if h.windowBytes > rateWindowByteLimit {
		h.bypassUntil = now.Add(rateBypassDuration)
		return true
	}
	return false
}

func appendHighlightedLine(out, line []byte, spans []Span) []byte {
	cursor := 0
	for _, span := range spans {
		if span.Start < cursor || span.End > len(line) || span.Start >= span.End {
			continue
		}
		out = append(out, line[cursor:span.Start]...)
		out = append(out, styleStart(span.Style)...)
		out = append(out, line[span.Start:span.End]...)
		out = append(out, "\x1b[0m"...)
		cursor = span.End
	}
	out = append(out, line[cursor:]...)
	return out
}

func (JunosProfile) Scan(line []byte) []Span {
	return scanJunos(line, nil)
}

func scanJunos(line []byte, spans []Span) []Span {
	for i := 0; i < len(line); {
		if !isTokenByte(line[i]) {
			i++
			continue
		}
		start := i
		for i < len(line) && isTokenByte(line[i]) {
			i++
		}
		if style, ok := classifyToken(line[start:i]); ok {
			spans = append(spans, Span{Start: start, End: i, Style: style})
		}
	}
	return spans
}

func classifyToken(token []byte) (Style, bool) {
	switch {
	case isMAC(token), isIPv4Token(token), isASN(token), isRouteTarget(token):
		return StyleCyan, true
	case isInterface(token):
		return StyleBlue, true
	}
	return classifyWord(token)
}

func classifyWord(token []byte) (Style, bool) {
	switch {
	case matchAny(token, "up", "enabled", "established", "active", "selected", "forwarding", "reachable"):
		return StyleGreen, true
	case matchAny(token, "down", "disabled", "error", "errors", "failed", "reject", "rejected", "discard", "dropped", "unreachable", "timeout", "denied"):
		return StyleRed, true
	case matchAny(token, "inactive", "pending", "hold", "stale", "hidden", "suppressed", "flapping", "degraded"):
		return StyleYellow, true
	case matchAny(token, "static", "direct", "local", "bgp", "ospf", "isis", "evpn", "ldp", "rsvp", "aggregate"):
		return StyleMagenta, true
	case matchAny(token, "set", "delete", "deleted", "deactivate", "activate", "annotate", "replace", "commit", "rollback", "changed"):
		return StyleMagenta, true
	case matchAny(token, "interfaces", "protocols", "routing-options", "policy-options", "firewall", "class-of-service", "routing-instances", "vlans", "bridge-domains", "system", "chassis"):
		return StyleMagenta, true
	default:
		return 0, false
	}
}

func isTokenByte(b byte) bool {
	return isAlphaNum(b) || b == '.' || b == '/' || b == '-' || b == '_' || b == ':'
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isHex(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func isMAC(token []byte) bool {
	if len(token) != 17 {
		return false
	}
	sep := token[2]
	if sep != ':' && sep != '-' {
		return false
	}
	for i := 0; i < len(token); i++ {
		if i%3 == 2 {
			if token[i] != sep {
				return false
			}
			continue
		}
		if !isHex(token[i]) {
			return false
		}
	}
	return true
}

func isIPv4Token(token []byte) bool {
	if len(token) < 7 {
		return false
	}
	addrEnd := len(token)
	if slash := indexByte(token, '/'); slash >= 0 {
		addrEnd = slash
		if slash == len(token)-1 || !parseRange(token[slash+1:], 0, 32) {
			return false
		}
	}
	octets := 0
	start := 0
	for i := 0; i <= addrEnd; i++ {
		if i == addrEnd || token[i] == '.' {
			if i == start || !parseRange(token[start:i], 0, 255) {
				return false
			}
			octets++
			start = i + 1
		}
	}
	return octets == 4
}

func parseRange(token []byte, minValue, maxValue int) bool {
	if len(token) == 0 {
		return false
	}
	value := 0
	for _, b := range token {
		if !isDigit(b) {
			return false
		}
		value = value*10 + int(b-'0')
		if value > maxValue {
			return false
		}
	}
	return value >= minValue
}

func isASN(token []byte) bool {
	if len(token) < 3 || lower(token[0]) != 'a' || lower(token[1]) != 's' {
		return false
	}
	return parseRange(token[2:], 1, 4294967295)
}

func isRouteTarget(token []byte) bool {
	if hasPrefixFold(token, "target:") {
		return routeParts(token[len("target:"):])
	}
	return routeParts(token)
}

func routeParts(token []byte) bool {
	first := indexByte(token, ':')
	if first <= 0 || first == len(token)-1 {
		return false
	}
	if indexByte(token[first+1:], ':') >= 0 {
		return false
	}
	return parseRange(token[:first], 1, 4294967295) && parseRange(token[first+1:], 0, 4294967295)
}

func isInterface(token []byte) bool {
	switch {
	case hasPrefixFold(token, "ge-"), hasPrefixFold(token, "xe-"), hasPrefixFold(token, "et-"):
		return len(token) > 3 && hasDigit(token[3:])
	case hasPrefixFold(token, "ae"), hasPrefixFold(token, "irb"), hasPrefixFold(token, "reth"), hasPrefixFold(token, "vlan"):
		return hasDigit(token)
	case hasPrefixFold(token, "lo0"):
		return true
	default:
		return false
	}
}

func hasDigit(token []byte) bool {
	for _, b := range token {
		if isDigit(b) {
			return true
		}
	}
	return false
}

func matchAny(token []byte, values ...string) bool {
	for _, value := range values {
		if asciiEqual(token, value) {
			return true
		}
	}
	return false
}

func asciiEqual(token []byte, value string) bool {
	if len(token) != len(value) {
		return false
	}
	for i, b := range token {
		if lower(b) != value[i] {
			return false
		}
	}
	return true
}

func hasPrefixFold(token []byte, prefix string) bool {
	if len(token) < len(prefix) {
		return false
	}
	for i := range prefix {
		if lower(token[i]) != prefix[i] {
			return false
		}
	}
	return true
}

func lower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func indexByte(token []byte, needle byte) int {
	for i, b := range token {
		if b == needle {
			return i
		}
	}
	return -1
}

func containsUnsafe(data []byte) bool {
	for _, b := range data {
		switch {
		case b == 0x1b:
			return true
		case b < 0x20 && b != '\t' && b != '\r' && b != '\n':
			return true
		}
	}
	return false
}

func styleStart(style Style) string {
	switch style {
	case StyleRed:
		return "\x1b[31m"
	case StyleYellow:
		return "\x1b[33m"
	case StyleGreen:
		return "\x1b[32m"
	case StyleCyan:
		return "\x1b[36m"
	case StyleBlue:
		return "\x1b[34m"
	case StyleMagenta:
		return "\x1b[35m"
	default:
		return ""
	}
}
