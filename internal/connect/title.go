package connect

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshargs"
	"golang.org/x/term"
)

// xterm title-stack and OSC 2 sequences. Terminals without a title stack
// ignore the push and pop, leaving the shell's next prompt to redraw the title.
const (
	titlePush = "\x1b[22;2t"
	titlePop  = "\x1b[23;2t"
)

var (
	titleOutput   io.Writer = os.Stdout
	titleTerminal           = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	}
)

// setTerminalTitle names the terminal after the resolved target for an
// interactive session and returns a function that restores the prior title.
func setTerminalTitle(resolved *ResolvedHost, sshArgs []string, cfg *config.Config) func() {
	title := terminalTitle(resolved)
	if cfg == nil || !cfg.SSH.TerminalTitle || title == "" || !interactiveSession(sshArgs) || !titleTerminal() {
		return func() {}
	}
	_, _ = io.WriteString(titleOutput, titlePush+"\x1b]2;"+title+"\a")
	return func() { _, _ = io.WriteString(titleOutput, titlePop) }
}

// terminalTitle prefers the inventory name over the transport address. It
// drops control characters and invalid UTF-8 so inventory data cannot inject
// escape sequences.
func terminalTitle(resolved *ResolvedHost) string {
	if resolved == nil {
		return ""
	}
	name := resolved.Canonical
	if name == "" {
		name = resolved.Hostname
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) || r == utf8.RuneError {
			return -1
		}
		return r
	}, name)
}

// interactiveSession reports whether the SSH options leave a login shell:
// no -N, -W, -f, -G, -Q, -s, or -T mode.
func interactiveSession(sshArgs []string) bool {
	interactive := true
	sshargs.Walk(sshArgs, func(option sshargs.Option) bool {
		if strings.ContainsRune("NWfGQsT", rune(option.Name)) {
			interactive = false
			return false
		}
		return true
	})
	return interactive
}
