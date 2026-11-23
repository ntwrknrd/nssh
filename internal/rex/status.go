// Package rex integrates nssh with the Rex terminal when nssh runs inside it.
// Every feature is optional: outside Rex, or when disabled, nothing is emitted.
package rex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// App is the OSC 7501 program identifier nssh reports under.
const App = "nssh"

// Program status states from the Rex OSC 7501 protocol.
const (
	StateIdle    = "idle"
	StateWorking = "working"
	StateDone    = "done"
	StateBlocked = "blocked"
	StateError   = "error"
	StateClear   = "clear"
)

// Blocked kinds from the Rex OSC 7501 protocol.
const (
	KindAuth     = "auth"
	KindQuestion = "question"
)

// Protocol limits on decoded text.
const (
	maxMsgBytes   = 2048
	maxTitleBytes = 192
	maxSegment    = 32
)

// Report is one OSC 7501 record. Each report replaces its record completely.
type Report struct {
	State    string
	ID       string // empty for the root record
	Kind     string // only with StateBlocked
	Title    string
	Msg      string
	Progress int // 1-100; zero or less means indeterminate
}

// Detected reports whether nssh runs inside a Rex terminal with integration
// enabled. NSSH_REX=0 disables it.
func Detected() bool {
	if os.Getenv("NSSH_REX") == "0" {
		return false
	}
	return os.Getenv("REX_SESSION") != "" && os.Getenv("REX_BLOCK") != ""
}

// Encode renders a report as an OSC 7501 sequence terminated by ST. App is
// set on root reports only; child records inherit it.
func Encode(r Report) string {
	var b strings.Builder
	b.WriteString("\x1b]7501;state=")
	b.WriteString(r.State)
	if r.ID != "" {
		b.WriteString(":id=")
		b.WriteString(r.ID)
	} else if r.State != StateClear {
		b.WriteString(":app=" + App)
	}
	if r.State == StateClear {
		b.WriteString("\x1b\\")
		return b.String()
	}
	if r.State == StateBlocked && r.Kind != "" {
		b.WriteString(":kind=")
		b.WriteString(r.Kind)
	}
	if (r.State == StateWorking || r.State == StateBlocked) && r.Progress > 0 && r.Progress <= 100 {
		b.WriteString(":progress=")
		b.WriteString(strconv.Itoa(r.Progress))
	}
	if t := clean(r.Title, maxTitleBytes); t != "" {
		b.WriteString(":title=")
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(t)))
	}
	if m := clean(r.Msg, maxMsgBytes); m != "" {
		b.WriteString(":msg=")
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(m)))
	}
	b.WriteString("\x1b\\")
	return b.String()
}

// Segment maps a host name to an id segment. Names outside the segment
// grammar, or longer than 32 bytes, map to a stable short hash.
func Segment(name string) string {
	valid := name != "" && len(name) <= maxSegment
	for i := 0; valid && i < len(name); i++ {
		c := name[i]
		valid = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '.' || c == '+' || c == '-'
	}
	if valid {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "h-" + hex.EncodeToString(sum[:6])
}

// clean drops control characters and invalid UTF-8, which would make Rex
// discard the whole report, and truncates to limit bytes on a rune boundary.
func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Writer emits reports to a terminal. A nil Writer is a no-op, so callers
// can hold one unconditionally.
type Writer struct {
	mu  sync.Mutex
	out io.Writer
}

// NewWriter returns a Writer for out, or nil when Rex is not detected.
func NewWriter(out io.Writer) *Writer {
	if out == nil || !Detected() {
		return nil
	}
	return &Writer{out: out}
}

// Report writes one report.
func (w *Writer) Report(r Report) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = io.WriteString(w.out, Encode(r))
}
