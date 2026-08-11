package highlight

const (
	ProfileNone  = "none"
	ProfileJunos = "junos"

	maxLineLength = 8192
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
	StyleBrightBlue
	StyleBrightYellow
	StyleDimGray
)

type Kind uint8

const (
	KindAction Kind = iota + 1
	KindHierarchy
	KindProtocol
	KindIdentifier
	KindInterface
	KindStateGood
	KindStateWarn
	KindStateBad
	KindComment
	KindString
	KindVariable
	KindNumber
	KindURL
	KindRoutingTable
)

type Span struct {
	Start int
	End   int
	Kind  Kind
}

type lineShape uint8

const (
	lineShapeFree lineShape = iota
	lineShapeConfig
)

type Profile interface {
	Scan(line []byte) []Span
}

type Highlighter struct {
	profile Profile
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
	if containsUnsafe(data) {
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
		shape := detectLineShape(line)
		var spans []Span
		if len(line) <= maxLineLength {
			var stack [64]Span
			spans = scanJunosWithShape(shape, line, stack[:0])
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

func appendHighlightedLine(out, line []byte, spans []Span) []byte {
	cursor := 0
	for _, span := range spans {
		if span.Start < cursor || span.End > len(line) || span.Start >= span.End {
			continue
		}
		out = append(out, line[cursor:span.Start]...)
		out = append(out, styleStart(styleForKind(span.Kind))...)
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
	return scanJunosWithShape(detectLineShape(line), line, spans)
}

func scanJunosWithShape(shape lineShape, line []byte, spans []Span) []Span {
	expectDescription := false
	expectNumber := false
	expectVariable := false

	for i := 0; i < len(line); {
		switch line[i] {
		case '\n', '\r':
			return spans
		case ' ', '\t':
			i++
			continue
		case '#':
			end := lineContentEnd(line, i)
			return append(spans, Span{Start: i, End: end, Kind: KindComment})
		case '/':
			if i+1 < len(line) && line[i+1] == '*' {
				end := scanBlockComment(line, i)
				spans = append(spans, Span{Start: i, End: end, Kind: KindComment})
				i = end
				continue
			}
		case '\'', '"':
			end := scanQuotedString(line, i)
			spans = append(spans, Span{Start: i, End: end, Kind: KindString})
			i = end
			expectDescription = false
			expectVariable = false
			expectNumber = false
			continue
		}

		if urlEnd := scanURL(line, i); urlEnd > i {
			spans = append(spans, Span{Start: i, End: urlEnd, Kind: KindURL})
			i = urlEnd
			expectDescription = false
			expectVariable = false
			expectNumber = false
			continue
		}

		if !isTokenByte(line[i]) {
			i++
			continue
		}

		start := i
		for i < len(line) && isTokenByte(line[i]) {
			i++
		}
		token := line[start:i]

		kind, ok := classifyToken(shape, token)
		switch {
		case expectDescription:
			kind, ok = KindString, true
		case isRoutingTable(token):
			kind, ok = KindRoutingTable, true
		case expectNumber && isNumericRange(token):
			kind, ok = KindNumber, true
		case expectVariable && !ok:
			kind, ok = KindVariable, true
		}
		if ok {
			spans = append(spans, Span{Start: start, End: i, Kind: kind})
		}

		expectDescription = shape == lineShapeConfig && asciiEqual(token, "description")
		expectNumber = shape == lineShapeConfig && isNumberValueKeyword(token)
		expectVariable = shape == lineShapeConfig && isVariableNameKeyword(token)
	}
	return spans
}

func detectLineShape(line []byte) lineShape {
	i := 0
	for i < len(line) && !isTokenByte(line[i]) {
		i++
	}
	if i == len(line) {
		return lineShapeFree
	}
	start := i
	for i < len(line) && isTokenByte(line[i]) {
		i++
	}
	token := line[start:i]
	if isActionWord(token) {
		return lineShapeConfig
	}
	if (isHierarchyWord(token) || isProtocolWord(token)) && opensConfigStanza(line[i:]) {
		return lineShapeConfig
	}
	return lineShapeFree
}

func opensConfigStanza(rest []byte) bool {
	for _, b := range rest {
		switch b {
		case ' ', '\t':
			continue
		case '\n', '\r', ';':
			return false
		case '{':
			return true
		}
	}
	return false
}

func classifyToken(shape lineShape, token []byte) (Kind, bool) {
	switch {
	case isMAC(token), isIPv4Token(token), isIPv6Token(token), isASN(token), isRouteTarget(token):
		return KindIdentifier, true
	case isInterface(token):
		return KindInterface, true
	}
	return classifyWord(shape, token)
}

func classifyWord(shape lineShape, token []byte) (Kind, bool) {
	switch {
	case matchAny(token, "up", "enabled", "established", "active", "selected", "forwarding", "reachable", "accept", "permit"):
		return KindStateGood, true
	case matchAny(token, "down", "disabled", "error", "errors", "failed", "reject", "rejected", "discard", "dropped", "unreachable", "timeout", "denied"):
		return KindStateBad, true
	case matchAny(token, "inactive", "pending", "hold", "stale", "hidden", "suppressed", "flapping", "degraded"):
		return KindStateWarn, true
	case shape == lineShapeConfig && isActionWord(token):
		return KindAction, true
	case shape == lineShapeConfig && isHierarchyWord(token):
		return KindHierarchy, true
	case shape == lineShapeConfig && isProtocolWord(token):
		return KindProtocol, true
	default:
		return 0, false
	}
}

func lineContentEnd(line []byte, start int) int {
	for start < len(line) && line[start] != '\n' && line[start] != '\r' {
		start++
	}
	return start
}

func scanBlockComment(line []byte, start int) int {
	for i := start + 2; i < len(line); i++ {
		if line[i] == '\n' || line[i] == '\r' {
			return i
		}
		if line[i] == '*' && i+1 < len(line) && line[i+1] == '/' {
			return i + 2
		}
	}
	return len(line)
}

func scanQuotedString(line []byte, start int) int {
	quote := line[start]
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\n', '\r':
			return i
		case '\\':
			if i+1 < len(line) && line[i+1] != '\n' && line[i+1] != '\r' {
				i++
			}
		case quote:
			return i + 1
		}
	}
	return len(line)
}

func scanURL(line []byte, start int) int {
	if !hasURLScheme(line[start:]) {
		return -1
	}
	i := start
	for i < len(line) {
		switch line[i] {
		case ' ', '\t', '\n', '\r', ';':
			return i
		}
		i++
	}
	return i
}

func hasURLScheme(token []byte) bool {
	return hasPrefixFold(token, "http://") ||
		hasPrefixFold(token, "https://") ||
		hasPrefixFold(token, "scp://") ||
		hasPrefixFold(token, "ftp://") ||
		hasPrefixFold(token, "tftp://") ||
		hasPrefixFold(token, "sftp://")
}

func isRoutingTable(token []byte) bool {
	dot := lastIndexByte(token, '.')
	if dot <= 0 || dot == len(token)-1 || !parseRange(token[dot+1:], 0, 99) {
		return false
	}
	base := token[:dot]
	if asciiEqual(base, "bgp.l2vpn") || asciiEqual(base, "bgp.l3vpn") ||
		hasSuffixFold(base, ".bgp.l2vpn") || hasSuffixFold(base, ".bgp.l3vpn") {
		return true
	}
	if tailDot := lastIndexByte(base, '.'); tailDot >= 0 {
		base = base[tailDot+1:]
	}
	return matchAny(base, "inet", "inet6", "mpls", "inetflow", "iso")
}

func isNumericRange(token []byte) bool {
	if len(token) == 0 {
		return false
	}
	if dash := indexByte(token, '-'); dash >= 0 {
		return dash > 0 && dash < len(token)-1 && parseRange(token[:dash], 0, 65535) && parseRange(token[dash+1:], 0, 65535)
	}
	return parseRange(token, 0, 65535)
}

func isTokenByte(b byte) bool {
	return isAlphaNum(b) || b == '.' || b == '/' || b == '-' || b == '_' || b == ':' || b == '<' || b == '>'
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

func isIPv6Token(token []byte) bool {
	if len(token) < 2 || indexByte(token, ':') < 0 {
		return false
	}
	addrEnd := len(token)
	if slash := indexByte(token, '/'); slash >= 0 {
		addrEnd = slash
		if slash == len(token)-1 || !parseRange(token[slash+1:], 0, 128) {
			return false
		}
	}
	addr := token[:addrEnd]
	if len(addr) < 2 || indexByte(addr, '.') >= 0 {
		return false
	}

	i := 0
	groups := 0
	compressed := false
	if len(addr) >= 2 && addr[0] == ':' && addr[1] == ':' {
		compressed = true
		i = 2
	}
	for i < len(addr) {
		start := i
		for i < len(addr) && addr[i] != ':' {
			if !isHex(addr[i]) {
				return false
			}
			i++
		}
		if i == start || i-start > 4 {
			return false
		}
		groups++
		if groups > 8 {
			return false
		}
		if i == len(addr) {
			break
		}
		if i+1 < len(addr) && addr[i+1] == ':' {
			if compressed {
				return false
			}
			compressed = true
			i += 2
			continue
		}
		i++
		if i == len(addr) {
			return false
		}
	}
	if compressed {
		return groups < 8
	}
	return groups == 8
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
	case hasPrefixFold(token, "ge-"), hasPrefixFold(token, "xe-"), hasPrefixFold(token, "et-"),
		hasPrefixFold(token, "so-"), hasPrefixFold(token, "fe-"), hasPrefixFold(token, "gr-"),
		hasPrefixFold(token, "lt-"), hasPrefixFold(token, "vt-"), hasPrefixFold(token, "si-"),
		hasPrefixFold(token, "sp-"):
		return len(token) > 3 && hasDigit(token[3:])
	case hasPrefixFold(token, "st"), hasPrefixFold(token, "lo"), hasPrefixFold(token, "me"),
		hasPrefixFold(token, "vme"), hasPrefixFold(token, "ae"), hasPrefixFold(token, "reth"),
		hasPrefixFold(token, "irb"), hasPrefixFold(token, "vlan"):
		return hasDigit(token)
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

func hasSuffixFold(token []byte, suffix string) bool {
	if len(token) < len(suffix) {
		return false
	}
	offset := len(token) - len(suffix)
	for i := range suffix {
		if lower(token[offset+i]) != suffix[i] {
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

func lastIndexByte(token []byte, needle byte) int {
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == needle {
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

func styleForKind(kind Kind) Style {
	switch kind {
	case KindAction:
		return StyleBrightYellow
	case KindHierarchy:
		return StyleMagenta
	case KindProtocol:
		return StyleBrightBlue
	case KindIdentifier:
		return StyleCyan
	case KindInterface:
		return StyleBlue
	case KindStateGood:
		return StyleGreen
	case KindStateWarn:
		return StyleYellow
	case KindStateBad:
		return StyleRed
	case KindComment:
		return StyleDimGray
	case KindString:
		return StyleGreen
	case KindVariable:
		return StyleCyan
	case KindNumber:
		return StyleCyan
	case KindURL:
		return StyleCyan
	case KindRoutingTable:
		return StyleCyan
	default:
		return 0
	}
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
	case StyleBrightBlue:
		return "\x1b[94m"
	case StyleBrightYellow:
		return "\x1b[93m"
	case StyleDimGray:
		return "\x1b[2;37m"
	default:
		return ""
	}
}
