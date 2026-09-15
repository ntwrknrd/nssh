package repl

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

func testTUI(width int) model {
	return model{tuiState: tuiState{width: width, height: 30}, input: textinput.New(), viewport: viewport.New(width, 25), transcript: limitedBuffer{max: 4096}}
}
func tuiEvent(host, command, body string, index int) core.Event {
	return core.Event{Target: core.ResolvedTarget{Identity: host}, Command: command, CommandIndex: index, State: core.Completed, Result: core.Result{Stdout: []byte(body)}}
}
func TestTUIAdaptiveResultsAndCommandBoundaries(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("alpha", "one", "left unique\nshared", 0))
	m.acceptResult(tuiEvent("beta", "one", "right unique\nshared", 0))
	text := ansi.Strip(m.renderBlocks())
	if strings.Contains(text, "Command:") || !strings.Contains(text, "OK:  [alpha] ('one')") {
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
		if !row.heading && len(row.spans) == 2 {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("no result rows")
	}
	m.viewport.GotoTop()
	next, _ := m.handleMouse(tea.MouseMsg{X: 8, Y: start + 1 - m.bodyOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 100, Y: start + 2 - m.bodyOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(model)
	if got := m.selectedText(); got != "left one\nleft two" {
		t.Fatalf("cross-pane selection: %q", got)
	}
	// Header and gutter clicks cannot copy neighboring output.
	next, _ = m.handleMouse(tea.MouseMsg{X: 2, Y: start + 1 - m.bodyOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
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

func TestTUIStacksWhenPairWouldWrap(t *testing.T) {
	m := testTUI(120)
	body := strings.Repeat("x", 80) + "\nsecond"
	m.acceptResult(tuiEvent("a", "show env power", body, 0))
	m.acceptResult(tuiEvent("b", "show env power", body, 0))
	for _, row := range m.renderRows() {
		if len(row.spans) > 1 {
			t.Fatal("paired output that needs wrapping")
		}
	}
	if strings.Count(ansi.Strip(m.renderBlocks()), strings.Repeat("x", 80)) != 2 {
		t.Fatal("full-width rows were wrapped")
	}
	m.width = 200
	m.refreshLayout()
	paired := false
	for _, row := range m.renderRows() {
		if !row.heading && len(row.spans) == 2 {
			paired = true
		}
	}
	if !paired {
		t.Fatal("wide terminal did not pair")
	}
}

func TestTUIPairFitIncludesGutterUnicodeAndLabels(t *testing.T) {
	a := tuiEvent("a", "show", strings.Repeat("界", 25)+"x"+strings.Repeat(" ", 80), 0)
	b := tuiEvent("b", "show", "short", 0)
	if !resultsFitPair(a, b, 120) {
		t.Fatal("exact fit with trailing padding rejected")
	}
	a.Result.Stdout = []byte(strings.Repeat("界", 26))
	if resultsFitPair(a, b, 120) {
		t.Fatal("Unicode overflow accepted")
	}
	a.Result.Stdout = []byte("short")
	a.Target.Identity = strings.Repeat("h", 55)
	if resultsFitPair(a, b, 120) {
		t.Fatal("long heading accepted")
	}
}

func TestPromptPickerFiltersAndPreservesSelections(t *testing.T) {
	m := testTUI(120)
	m.candidates = []string{"agg1", "border1", "border2"}
	m.openPicker()
	m.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("border")})
	m.updatePicker(tea.KeyMsg{Type: tea.KeySpace})
	m.updatePicker(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("agg")})
	m.updatePicker(tea.KeyMsg{Type: tea.KeySpace})
	m.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickerOpen || m.active || m.input.Value() != "[ 'agg1', 'border1' ] ( '' )" {
		t.Fatal(m.input.Value())
	}
	if m.input.Position() != len([]rune(m.input.Value()))-3 {
		t.Fatal("cursor not in command field")
	}
	view := ansi.Strip(m.View())
	if strings.Contains(strings.Split(view, "\n")[0], "Tab") || strings.Count(view, "Enter run") != 1 || strings.Contains(view, "F5") {
		t.Fatal(view)
	}
}

func TestPromptPickerNoMatchesAndEscapePreserveDraft(t *testing.T) {
	m := testTUI(120)
	m.candidates = []string{"edge1", "edge2"}
	m.input.SetValue("[ 'ed' ] ( 'show version' )")
	m.input.SetCursor(5)
	draft, cursor := m.input.Value(), m.input.Position()
	m.openPicker()
	m.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	m.updatePicker(tea.KeyMsg{Type: tea.KeySpace})
	m.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickerOpen || m.active {
		t.Fatal("no-match picker submitted")
	}
	m.updatePicker(tea.KeyMsg{Type: tea.KeyEsc})
	if m.input.Value() != draft || m.input.Position() != cursor {
		t.Fatal("draft changed")
	}
	m.input.SetValue("")
	m.openPicker()
	m.updatePicker(tea.KeyMsg{Type: tea.KeyEsc})
	if m.input.Value() != "" {
		t.Fatal("empty draft changed")
	}
}

func TestUniqueCompletionReplacesWholeHostAtCursor(t *testing.T) {
	m := testTUI(120)
	m.candidates = []string{"edge1"}
	m.input.SetValue("[ 'ops@edZZ' ] ( 'show version' )")
	m.input.SetCursor(len([]rune("[ 'ops@ed")))
	m.openPicker()
	if m.pickerOpen || m.input.Value() != "[ 'ops@edge1' ] ( 'show version' )" {
		t.Fatal(m.input.Value())
	}
}

func TestTUIResultLabelIncludesStatusDeviceAndCommand(t *testing.T) {
	e := tuiEvent("user@device", "show env power", "output", 0)
	if got := resultLabel(e); got != "OK:  [user@device] ('show env power')" {
		t.Fatal(got)
	}
	e.State = core.Failed
	e.Result.ExitCode = 7
	if got := resultLabel(e); got != "FAILED exit 7:  [user@device] ('show env power')" {
		t.Fatal(got)
	}
}

func TestStickyCommandFollowsScrolledOutput(t *testing.T) {
	m := testTUI(80)
	m.acceptResult(tuiEvent("a", "first", strings.Repeat("one\n", 40), 0))
	m.acceptResult(tuiEvent("a", "second", strings.Repeat("two\n", 40), 1))
	m.viewport.SetYOffset(15)
	if row, _ := m.stickyHeader(); row.command != "first" {
		t.Fatal(row)
	}
	for i, row := range m.renderRows() {
		if row.command == "second" {
			m.viewport.SetYOffset(i + 2)
			break
		}
	}
	if row, _ := m.stickyHeader(); row.command != "second" {
		t.Fatal(row)
	}
	if strings.Contains(ansi.Strip(m.View()), "nssh repl") {
		t.Fatal("old title retained")
	}
}

func TestStickyCommandCopiesFullOriginalText(t *testing.T) {
	m := testTUI(40)
	command := "show " + strings.Repeat("long-command ", 10) + "\nnext"
	m.acceptResult(tuiEvent("a", command, "output", 0))
	if strings.Contains(m.commandHeader(), "\n") {
		t.Fatal("header spans multiple terminal rows")
	}
	next, _ := m.handleMouse(tea.MouseMsg{X: 10, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if got := m.selectedText(); got != resultLabel(tuiEvent("a", command, "output", 0)) {
		t.Fatalf("copy=%q", got)
	}
	if !strings.Contains(ansi.Strip(m.commandHeader()), "...") {
		t.Fatal("long header not truncated")
	}
}

func TestRightClickCopiesSelectionAndClearsAfterSuccess(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("a", "show", "left one\nleft two", 0))
	m.acceptResult(tuiEvent("b", "show", "right one\nright two", 0))
	m.viewport.GotoTop()
	row := 0
	for i, r := range m.renderRows() {
		if !r.heading && len(r.spans) == 2 {
			row = i
			break
		}
	}
	next, _ := m.handleMouse(tea.MouseMsg{X: 8, Y: row + 1 - m.bodyOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 8, Y: row + 2 - m.bodyOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(model)
	next, cmd := m.handleMouse(tea.MouseMsg{X: 100, Y: 0, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	m = next.(model)
	if cmd == nil || !m.selected {
		t.Fatal("selection cleared before copy")
	}
	// Capture the clipboard request without touching the user's clipboard.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original; writer.Close() }()
	msg := cmd()
	writer.Close()
	os.Stdout = original
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("left one\nleft two")) + "\x07"
	if string(data) != want {
		t.Fatalf("clipboard request=%q", data)
	}
	next, _ = m.Update(msg)
	m = next.(model)
	if m.selected || m.selecting || m.selectedText() != "" {
		t.Fatal("selection not cleared")
	}
	_, cmd = m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if cmd != nil {
		t.Fatal("empty selection copied")
	}
}

func TestCopyFailurePreservesSelection(t *testing.T) {
	m := testTUI(80)
	m.acceptResult(tuiEvent("a", "show", "output", 0))
	next, _ := m.handleMouse(tea.MouseMsg{X: 10, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.Update(tuiCopyMsg{err: errors.New("write failed"), clearAfter: true, text: "show"})
	m = next.(model)
	if !m.selected || m.selectedText() != "OK:  [a] ('show')" {
		t.Fatal("failed copy lost selection")
	}
}

func TestStickyStatusCopiesWithVisibleOutput(t *testing.T) {
	m := testTUI(80)
	m.acceptResult(tuiEvent("user@device", "show lldp nei", strings.Repeat("hidden\n", 30)+"visible one\nvisible two\n"+strings.Repeat("tail\n", 30), 0))
	m.viewport.SetYOffset(31)
	next, _ := m.handleMouse(tea.MouseMsg{X: 5, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 5, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(model)
	want := "OK:  [user@device] ('show lldp nei')\nvisible one\nvisible two"
	// Use a tall enough body so the viewport can reach the selected offset.
	if m.viewport.YOffset != 31 {
		t.Fatal("viewport did not reach test row")
	}
	if got := m.selectedText(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
func TestStickyStatusIsNotDuplicatedAtTop(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("a", "show", "body", 0))
	m.viewport.GotoTop()
	view := ansi.Strip(m.View())
	if strings.Contains(view, "Command:") || strings.Count(view, "OK:  [a] ('show')") != 1 {
		t.Fatal(view)
	}
	next, _ := m.handleMouse(tea.MouseMsg{X: 1, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(model)
	if got := m.selectedText(); got != "OK:  [a] ('show')\nbody" {
		t.Fatal(got)
	}
}

func TestStickyPairedStatusCopiesOnlyItsDevice(t *testing.T) {
	m := testTUI(120)
	m.acceptResult(tuiEvent("a", "show", "left", 0))
	m.acceptResult(tuiEvent("b", "show", "right", 0))
	m.viewport.GotoTop()
	next, _ := m.handleMouse(tea.MouseMsg{X: 80, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 80, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(model)
	if got := m.selectedText(); got != "OK:  [b] ('show')\nright" {
		t.Fatal(got)
	}
}
