package repl

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// hostListOutput presents completed jobs without retaining their output. The
// executor serializes callbacks; stdout and stderr keep their original streams.
type hostListOutput struct {
	out, errOut io.Writer
	targets     []core.ResolvedTarget
}

func (p *hostListOutput) start(command string, targets []core.ResolvedTarget) {
	p.targets = targets
	_, _ = fmt.Fprintf(p.errOut, "%s\n%d hosts\n\n", taskBanner("Command: "+displayLabel(command)), len(targets))
}

func (p *hostListOutput) event(event core.Event) {
	if event.State == core.Queued || event.State == core.Running {
		return
	}
	name := hostListName(event.Target)
	status := hostListStatus(event.State)
	heading := fmt.Sprintf("[%s] %s", name, strings.ToUpper(status))
	if event.State == core.Failed {
		heading += fmt.Sprintf(" (exit %d)", event.Result.ExitCode)
	}
	// Keep the result heading next to its output, including when only one
	// stream is redirected. A stderr payload always has its own attribution.
	if len(event.Result.Stdout) > 0 {
		p.block(p.out, heading, event.Result.Stdout, event.State)
	}
	if len(event.Result.Stderr) > 0 {
		stderrHeading := heading
		if len(event.Result.Stdout) > 0 {
			stderrHeading = "[" + name + "] STDERR"
		}
		p.block(p.errOut, stderrHeading, event.Result.Stderr, event.State)
	}
	if len(event.Result.Stdout) == 0 && len(event.Result.Stderr) == 0 {
		_, _ = fmt.Fprintln(p.errOut, hostListColor(p.errOut, taskBanner(heading), event.State))
	}
	if event.Err != nil && event.State == core.Failed {
		_, _ = fmt.Fprintf(p.errOut, "error: [%s] %s\n", name, displayLabel(event.Err.Error()))
	}
	if event.Result.Truncated {
		_, _ = fmt.Fprintf(p.errOut, "warning: [%s] output truncated\n", name)
	}
}

func (p *hostListOutput) block(w io.Writer, heading string, data []byte, state core.State) {
	_, _ = fmt.Fprintln(w, hostListColor(w, taskBanner(heading), state))
	_, _ = w.Write(data)
	if data[len(data)-1] != '\n' {
		_, _ = fmt.Fprintln(w)
	}
	_, _ = fmt.Fprintln(w)
}

func (p *hostListOutput) recap(events []core.Event) {
	if len(p.targets) == 0 {
		return
	}
	states := make(map[string]core.State, len(p.targets))
	width := 0
	for _, target := range p.targets {
		width = max(width, len(hostListName(target)))
	}
	for _, event := range events {
		if event.State != core.Queued && event.State != core.Running {
			states[event.Target.Identity] = event.State
		}
	}
	_, _ = fmt.Fprintf(p.errOut, "%s\n", taskBanner("Results"))
	for _, target := range p.targets {
		state := states[target.Identity]
		ok, failed, canceled := 0, 0, 0
		switch state {
		case core.Completed:
			ok = 1
		case core.Failed:
			failed = 1
		case core.Canceled:
			canceled = 1
		}
		line := fmt.Sprintf("%-*s : ok=%d  failed=%d  canceled=%d", width, hostListName(target), ok, failed, canceled)
		_, _ = fmt.Fprintln(p.errOut, hostListColor(p.errOut, line, state))
	}
}

func hostListName(target core.ResolvedTarget) string {
	if name, ok := target.Value.(string); ok && name != "" {
		return displayLabel(name)
	}
	return displayLabel(target.Identity)
}
func hostListStatus(state core.State) string {
	if state == core.Completed {
		return "ok"
	}
	return string(state)
}
func taskBanner(title string) string { return title + " " + strings.Repeat("-", max(1, 79-len(title))) }

// Escape control characters in metadata; remote output bytes remain untouched.
func displayLabel(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) {
			_, _ = fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func hostListColor(w io.Writer, text string, state core.State) string {
	file, ok := w.(*os.File)
	_, noColor := os.LookupEnv("NO_COLOR")
	if !ok || !isTerminal(file) || noColor || os.Getenv("TERM") == "dumb" {
		return text
	}
	switch state {
	case core.Completed:
		return ui.StyleGreen.Render(text)
	case core.Failed:
		return ui.StyleRed.Render(text)
	case core.Canceled, core.Skipped:
		return ui.StyleYellow.Render(text)
	}
	return text
}
