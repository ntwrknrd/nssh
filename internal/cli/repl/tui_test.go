package repl

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

func testTUI(width int) model {
	return model{tuiState: tuiState{width: width, height: 30, commandIndex: -1}, input: textinput.New(), viewport: viewport.New(width, 25), transcript: limitedBuffer{max: 4096}}
}
func tuiEvent(host, command, body string, index int) core.Event {
	return core.Event{Target: core.ResolvedTarget{Identity: host}, Command: command, CommandIndex: index, State: core.Completed, Result: core.Result{Stdout: []byte(body)}}
}
func TestTUIAdaptiveResultsAndCommandBoundaries(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("alpha", "one", "left unique\nshared", 0))
	m.acceptResult(tuiEvent("beta", "one", "right unique\nshared", 0))
	text := ansi.Strip(m.renderBlocks())
	if !strings.Contains(text, "Command 1: one") {
		t.Fatal(text)
	}
	paired := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "[alpha]") && strings.Contains(line, "[beta]") {
			paired = true
		}
	}
	if !paired {
		t.Fatal("wide view did not pair device headings", text)
	}
	m.width = 60
	m.refreshLayout()
	if strings.Index(m.renderBlocks(), "left unique") > strings.Index(m.renderBlocks(), "[beta]") {
		t.Fatal("narrow view must stack whole results")
	}
	m.width = 120
	m.refreshLayout()
	m.acceptResult(tuiEvent("alpha", "two", "second", 1))
	m.acceptResult(tuiEvent("beta", "three", "third", 2))
	for _, line := range strings.Split(ansi.Strip(m.renderBlocks()), "\n") {
		if strings.Contains(line, "second") && strings.Contains(line, "third") {
			t.Fatal("paired different commands")
		}
	}
	m.stacked = true
	m.refreshLayout()
	for _, line := range strings.Split(ansi.Strip(m.renderBlocks()), "\n") {
		if strings.Contains(line, "[alpha]") && strings.Contains(line, "[beta]") {
			t.Fatal("forced stacked view still paired")
		}
	}
}
func TestTUISelectionCopiesOnlyChosenDevice(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("a", "cmd", "left one\nleft two", 0))
	m.acceptResult(tuiEvent("b", "cmd", "right one\nright two", 0))
	rows := m.renderRows()
	start := -1
	for i, row := range rows {
		if len(row.spans) == 2 {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("no result rows")
	}
	m.viewport.GotoTop()
	next, _ := m.handleMouse(tea.MouseMsg{X: 8, Y: start + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 100, Y: start + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(model)
	if got := m.selectedText(); got != "left one\nleft two" {
		t.Fatalf("cross-pane selection: %q", got)
	}
	// Header and gutter clicks cannot copy neighboring output.
	next, _ = m.handleMouse(tea.MouseMsg{X: 2, Y: start + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if m.selectedText() != "" {
		t.Fatal("line number gutter selected output")
	}
}
func TestTUIPickerPreservesCommandAndUsername(t *testing.T) {
	m := testTUI(120)
	m.candidates = []string{"edge1", "edge2"}
	m.input.SetValue("[ 'alice@ed' ] ( 'show version' )")
	m.input.SetCursor(len([]rune("[ 'alice@ed")))
	if got := ansi.Strip(m.editorView(110)); !strings.Contains(got, "alice@edge1") {
		t.Fatalf("missing inline suggestion: %q", got)
	}
	m.openPicker()
	if len(m.matches) != 2 {
		t.Fatal(m.matches)
	}
	m.updatePicker(tea.KeyMsg{Type: tea.KeySpace})
	m.updatePicker(tea.KeyMsg{Type: tea.KeyDown})
	m.updatePicker(tea.KeyMsg{Type: tea.KeySpace})
	m.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "[ 'alice@edge1', 'alice@edge2' ] ( 'show version' )" {
		t.Fatal(got)
	}
	if len(m.matches) != 0 {
		t.Fatal("picker remained open")
	}
	if !strings.Contains(ansi.Strip(m.View()), "running 0") {
		t.Fatal(m.View())
	}
}
func TestTUIBoundsSanitizesAndReflowsResults(t *testing.T) {
	m := testTUI(110)
	m.transcript.max = 256
	for i := 0; i < 20; i++ {
		m.acceptResult(tuiEvent("host", "show", "\x1b]52;c;YXNk\a\x1b[2J"+strings.Repeat("x", 50)+"\n", 0))
	}
	if m.bytes > 256 || !m.transcript.truncated {
		t.Fatalf("bytes=%d", m.bytes)
	}
	if strings.Contains(m.renderBlocks(), "]52;") {
		t.Fatal("remote clipboard escape retained")
	}
	for _, width := range []int{12, 60, 110} {
		m.width = width
		m.refreshLayout()
		for _, row := range strings.Split(ansi.Strip(m.renderBlocks()), "\n") {
			if ansi.StringWidth(row) > width && !strings.Contains(row, "evicted") {
				t.Fatalf("width %d exceeded: %q", width, row)
			}
		}
	}
}
func TestTUIProgressAndFooter(t *testing.T) {
	m := testTUI(120)
	next, _ := m.Update(tuiBatchMsg{targets: 2, commands: 1})
	m = next.(model)
	e := tuiEvent("a", "cmd", "output", 0)
	e.State = core.Running
	m.acceptResult(e)
	if m.running != 1 || !strings.Contains(m.View(), "pending 1") {
		t.Fatal(m.View())
	}
	e.State = core.Completed
	m.acceptResult(e)
	canceled := tuiEvent("b", "cmd", "", 0)
	canceled.State = core.Canceled
	m.acceptResult(canceled)
	if m.running != 0 || m.done != 1 || m.canceled != 1 || !strings.Contains(m.View(), "pending 0") {
		t.Fatal(m.View())
	}
	text := ansi.Strip(m.View())
	if strings.Index(text, "> ") > strings.Index(text, "running 0") {
		t.Fatal("editor must appear above footer")
	}
	if len(strings.Split(text, "\n")) > m.height {
		t.Fatal("view overflows terminal height")
	}
}

func TestTUIAccountsForInvisiblePayloadBytes(t *testing.T) {
	m := testTUI(120)
	m.transcript.max = 256
	for i := 0; i < 10; i++ {
		m.acceptResult(tuiEvent("host", "command", strings.Repeat("\n", 200), 0))
	}
	retained := 0
	for _, b := range m.blocks {
		if b.event != nil {
			retained += len(b.event.Result.Stdout) + len(b.event.Result.Stderr)
		}
	}
	if retained > 256 || m.bytes > 256 {
		t.Fatalf("retained=%d accounted=%d", retained, m.bytes)
	}
}

func TestTUITablePaddingDoesNotCreateBlankRows(t *testing.T) {
	e := tuiEvent("host", "show", "  Et1    connected"+strings.Repeat(" ", 70)+"\n  Et2    connected"+strings.Repeat(" ", 70)+"\n", 0)
	lines := resultLines(e, 40)
	if len(lines) != 2 || lines[0] != "  Et1    connected" || lines[1] != "  Et2    connected" {
		t.Fatalf("padding created rows: %#v", lines)
	}
	e.Result.Stdout = []byte("first\n\n  indented\n")
	lines = resultLines(e, 40)
	if len(lines) != 3 || lines[1] != "" || lines[2] != "  indented" {
		t.Fatalf("lost real blank line or indentation: %#v", lines)
	}
}

func TestTUIPairsSourceRowsBeforeWrapping(t *testing.T) {
	m := testTUI(100)
	m.acceptResult(tuiEvent("a", "show", strings.Repeat("x", 50)+"\nEt2 left", 0))
	m.acceptResult(tuiEvent("b", "show", "short\nEt2 right", 0))
	for _, row := range m.renderRows() {
		line := ansi.Strip(row.styled)
		if strings.Contains(line, "Et2 left") {
			if !strings.Contains(line, "Et2 right") {
				t.Fatalf("second source rows drifted: %q", line)
			}
			if strings.Count(line, "     2 ") != 2 {
				t.Fatalf("line numbers count wrapped display rows: %q", line)
			}
			return
		}
	}
	t.Fatal("missing second source row")
}
