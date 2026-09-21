package repl

import (
	"fmt"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func terminalModel(t *testing.T, width int) model {
	t.Helper()
	m := testTUI(width)
	m.interactive = true
	m.target = -1
	m.group = 1
	for _, name := range []string{"alice@eos", "bob@junos"} {
		e := vt.NewEmulator(40, 8)
		e.SetScrollbackSize(100)
		p := &terminalPane{name: name, terminal: e, wire: &terminalWire{}, state: "open"}
		m.panes = append(m.panes, p)
		go func() { _, _ = io.Copy(io.Discard, e) }()
	}
	m.resizePanes()
	t.Cleanup(func() { m.closePanes() })
	return m
}
func TestTerminalPanesEmulateAndIsolateOutput(t *testing.T) {
	m := terminalModel(t, 160)
	for i, text := range []string{"old\r\x1b[2Kleft\r\necho\r\neos(config)# ", "right\r\njunos> "} {
		next, _, _ := m.updateInteractive(terminalOutputMsg{1, i, []byte(text)})
		m = next
	}
	view := ansi.Strip(m.interactiveView())
	if !strings.Contains(view, "left") || strings.Contains(view, "old") || !strings.Contains(view, "eos(config)#") || !strings.Contains(view, "junos>") {
		t.Fatal(view)
	}
	if lipgloss.Width(view) > 160 || lipgloss.Height(view) > 30 {
		t.Fatalf("overflow %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}
	m.panes[0].selected = true
	m.panes[0].selectionStart = 0
	m.panes[0].selectionEnd = 1
	m.panes[0].captureSelection()
	if m.panes[0].selectionText != "left\necho" {
		t.Fatalf("wrong copy: %q", m.panes[0].selectionText)
	}
	next, _, _ := m.updateInteractive(terminalCopyMsg{1, 0, m.panes[0].selectionText, true, nil})
	m = next
	if m.panes[0].selected {
		t.Fatal("copy retained selection")
	}
}
func TestTerminalBatchPreservesTabsAndHistory(t *testing.T) {
	m := terminalModel(t, 100)
	m.entries = []string{"[ 'first' ] ( 'one' )"}
	m.controlOpen = true
	m.controlInput.SetValue(":batch")
	m, _, _ = m.updateTerminalControl(tea.KeyMsg{Type: tea.KeyEnter})
	if m.interactive || len(m.panes) != 2 || len(m.tabs) != 1 || len(m.entries) != 1 {
		t.Fatal("lost sessions or history")
	}
	m.switchTab(0)
	if !m.interactive || m.controlOpen || len(m.panes) != 2 {
		t.Fatal("failed resume")
	}
}
func TestTerminalLossPausesBroadcastAndStaleOutputIsIgnored(t *testing.T) {
	m := terminalModel(t, 160)
	m, _, _ = m.updateInteractive(terminalClosedMsg{1, 0, nil})
	if !m.broadcastPaused {
		t.Fatal("broadcast stayed enabled")
	}
	m.sendInteractive([]byte("never\r"))
	if !strings.Contains(m.message, "paused") {
		t.Fatal(m.message)
	}
	m, _, _ = m.updateInteractive(terminalOutputMsg{0, 0, []byte("stale")})
	if strings.Contains(m.panes[0].terminal.String(), "stale") {
		t.Fatal("stale group mutated current panes")
	}
}

func TestInteractiveHostKeyRequestReachesModal(t *testing.T) {
	m := terminalModel(t, 160)
	request := &trustRequest{prompt: connector.HostKeyPrompt{Host: "second", KeyType: "ssh-ed25519", Fingerprint: "SHA256:test", Changed: true}, response: make(chan connector.HostKeyAction, 1)}
	next, _ := m.Update(request)
	m = next.(model)
	if m.trust != request {
		t.Fatal("interactive mode swallowed host-key approval")
	}
	if !strings.Contains(ansi.Strip(m.View()), "CHANGED HOST KEY") {
		t.Fatal("changed-key warning hidden")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = next.(model)
	select {
	case got := <-request.response:
		if got != connector.HostKeyAcceptOnce {
			t.Fatal(got)
		}
	default:
		t.Fatal("approval not delivered")
	}
	if m.trust != nil {
		t.Fatal("modal remained open")
	}
}

func TestSelectingTerminalOutputDoesNotChangeBroadcast(t *testing.T) {
	m := terminalModel(t, 160)
	_, _ = m.panes[1].terminal.Write([]byte("device output"))
	x := m.panes[0].terminal.Width() + terminalGutter + 3
	next, _ := m.Update(tea.MouseMsg{X: x, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if m.target != -1 || !m.panes[1].selected {
		t.Fatal("output selection changed broadcast targets")
	}
	next, _ = m.Update(tea.MouseMsg{X: x, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if m.target != 1 {
		t.Fatal("header did not focus device")
	}
	next, _ = m.Update(tea.MouseMsg{X: x, Y: 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if m.panes[1].selected {
		t.Fatal("blank click selected empty output")
	}
}

func TestTerminalStatusReplyUsesANSIForm(t *testing.T) {
	emu := newTerminalEmulator(80, 24)
	replies := make(chan string, 1)
	go func() { buf := make([]byte, 64); n, _ := emu.Read(buf); replies <- string(buf[:n]) }()
	_, _ = emu.Write([]byte("\x1b[5n"))
	got := <-replies
	_ = emu.InputPipe().(io.Closer).Close()
	if got != "\x1b[0n" {
		t.Fatalf("malformed status reply: %q", got)
	}
}

func TestBackgroundTabOutputAndCloseAreIsolated(t *testing.T) {
	m := terminalModel(t, 160)
	m.saveTab()
	first := m.terminalGroup
	m.terminalGroup = terminalGroup{group: 2, target: -1}
	m.saveTab()
	m, _, _ = m.updateInteractive(terminalOutputMsg{1, 0, []byte("background")})
	if len(m.panes) != 0 || !strings.Contains(first.panes[0].terminal.String(), "background") {
		t.Fatal("background output routed incorrectly")
	}
	m, _, _ = m.updateInteractive(terminalClosedMsg{1, 1, nil})
	if m.broadcastPaused || !m.tabs[0].broadcastPaused {
		t.Fatal("background close affected active broadcast")
	}
	m.removeTab()
	if m.group != 1 || len(m.panes) != 2 {
		t.Fatal("closing tab did not restore previous tab")
	}
	m.closePanes()
	m, _, _ = m.updateInteractive(terminalOutputMsg{1, 0, []byte("stale")})
	if len(m.tabs) != 0 || len(m.panes) != 0 {
		t.Fatal("closed tab revived")
	}
}
func TestControlOverlayCapturesKeysUntilEscape(t *testing.T) {
	m := terminalModel(t, 100)
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("local")})
	if !m.controlOpen || m.controlInput.Value() != "local" || strings.Contains(m.message, "Input not sent") {
		t.Fatal("control input leaked")
	}
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.controlOpen || m.controlInput.Value() != "" {
		t.Fatal("control draft retained")
	}
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeySpace})
	if !strings.Contains(m.message, "Input not sent") {
		t.Fatal("space did not reach terminal path")
	}
	if strings.Contains(ansi.Strip(m.View()), "Type a command") {
		t.Fatal("command box remains")
	}
}

func TestTerminalHeaderSelectionSurvivesDisconnect(t *testing.T) {
	m := terminalModel(t, 160)
	_, _ = m.panes[0].terminal.Write([]byte("output"))
	m, _, _ = m.updateInteractive(tea.MouseMsg{X: 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m, _, _ = m.updateInteractive(tea.MouseMsg{X: 2, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if m.target != -1 || m.panes[0].selectionText != "[alice@eos]\noutput" {
		t.Fatalf("header not selectable: %q", m.panes[0].selectionText)
	}
	m, _, _ = m.updateInteractive(terminalClosedMsg{1, 0, nil})
	m, _, _ = m.updateInteractive(terminalClosedMsg{1, 1, nil})
	view := ansi.Strip(m.interactiveView())
	if strings.Contains(view, "Sending to") || strings.Contains(view, "closed") || strings.Count(view, "alice@eos") != 1 || !strings.Contains(view, "Disconnected") {
		t.Fatal(view)
	}
	if m.panes[0].selectionText != "[alice@eos]\noutput" {
		t.Fatal("disconnect changed header copy")
	}
}

func testUpdate(m model, msg tea.Msg) model { next, _ := m.Update(msg); return next.(model) }

func TestClearKeysAndControlDraftAcrossModes(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		m := terminalModel(t, 100)
		m.interactive = interactive
		m.input.SetValue("[ 'draft' ] ( 'show' )")
		m.entries = []string{"saved"}
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyCtrlP})
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("help")})
		if !m.controlOpen || m.controlInput.Value() != "help" || m.input.Value() != "[ 'draft' ] ( 'show' )" {
			t.Fatal("overlay damaged draft")
		}
		m = testUpdate(m, tea.KeyMsg{Type: tea.KeyCtrlP})
		for _, key := range []tea.KeyType{tea.KeyCtrlK, tea.KeyCtrlL} {
			_, _ = m.panes[0].terminal.Write([]byte(strings.Repeat("line\r\n", 100)))
			m.acceptResult(tuiEvent("a", "show", "output", 0))
			m = testUpdate(m, tea.KeyMsg{Type: key})
			if interactive && m.panes[0].terminal.ScrollbackLen() != 0 {
				t.Fatal("terminal scrollback retained")
			}
			if !interactive && len(m.blocks) != 0 {
				t.Fatal("batch scrollback retained")
			}
			if m.controlOpen || m.input.Value() != "[ 'draft' ] ( 'show' )" || len(m.entries) != 1 {
				t.Fatal("clear changed input/history")
			}
		}
	}
}

func TestPaneScrollLockDefaultsOnAndCanBeDisabled(t *testing.T) {
	m := terminalModel(t, 160)
	for _, p := range m.panes {
		_, _ = p.terminal.Write([]byte(strings.Repeat("line\r\n", 80)))
	}
	m = testUpdate(m, tea.MouseMsg{X: 2, Y: 3, Button: tea.MouseButtonWheelUp})
	if m.panes[0].offset != 3 || m.panes[1].offset != 3 {
		t.Fatal("scroll did not move together")
	}
	m.independentScroll = true
	m = testUpdate(m, tea.MouseMsg{X: 2, Y: 3, Button: tea.MouseButtonWheelUp})
	if m.panes[0].offset != 6 || m.panes[1].offset != 3 {
		t.Fatal("independent scroll affected another pane")
	}
}
func TestTabShortcutsCycleAndWrap(t *testing.T) {
	m := terminalModel(t, 160)
	m.saveTab()
	m.tabs = append(m.tabs, terminalGroup{group: 2, target: -1, independentScroll: true})
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.group != 2 || !m.independentScroll {
		t.Fatal("next tab not selected")
	}
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.group != 1 || m.independentScroll {
		t.Fatal("next did not wrap or changed tab preference")
	}
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if m.group != 2 {
		t.Fatal("previous did not wrap")
	}
}
func TestReconnectSkipsOpenPanes(t *testing.T) {
	m := terminalModel(t, 160)
	m.reconnectTerminals(-1)
	if m.message != "No disconnected panes to reconnect" || m.panes[0].state != "open" {
		t.Fatal("reconnect touched live sessions")
	}
}

func TestPaneStatusBadgeIsIndependentFromCopiedHeader(t *testing.T) {
	m := terminalModel(t, 160)
	p := m.panes[0]
	for _, tc := range []struct{ state, label string }{{"opening", "[connecting]"}, {"open", "[connected]"}, {"closed (exit status 255)", "[disconnected]"}} {
		p.state = tc.state
		title := ansi.Strip(p.paneTitle(1))
		if !strings.Contains(title, tc.label) || strings.Contains(title, "255") || ansi.StringWidth(title) > p.terminal.Width()+terminalGutter {
			t.Fatal(title)
		}
		p.selectionStart, p.selectionEnd = -1, -1
		p.captureSelection()
		if p.selectionText != "[alice@eos]" {
			t.Fatal("status polluted identity copy", p.selectionText)
		}
	}
}
func TestWakePauseConsumesFurtherInputUntilTargetSelection(t *testing.T) {
	m := terminalModel(t, 160)
	m.wakePaused = true
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("never send")})
	if !m.wakePaused || !strings.Contains(m.message, "Input paused") {
		t.Fatal("typing bypassed wake pause")
	}
	m.controlOpen = true
	m.controlInput.SetValue(":target 1")
	m = testUpdate(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.wakePaused || m.target != 0 {
		t.Fatal("explicit focus failed to resume")
	}
}

func TestInteractiveNumberedPanesAndSelectionStatus(t *testing.T) {
	m := terminalModel(t, 200)
	p := m.panes[0]
	_, _ = p.terminal.Write([]byte("first\r\nsecond"))
	p.selected = true
	p.selectionStart = 0
	p.selectionEnd = 1
	p.captureSelection()
	m.panes[1].state = "closed"
	view := ansi.Strip(m.interactiveView())
	for _, want := range []string{"     1 first", "     2 second", "2 lines selected | interactive | connected 1  connecting 0  disconnected 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %s", want, view)
		}
	}
	if p.selectionText != "first\nsecond" {
		t.Fatal("gutter copied", p.selectionText)
	}
	if m.paneAt(101, 3) != 1 {
		t.Fatal("right pane hit area shifted")
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatal("view overflow")
		}
	}
}

func TestInteractiveLineNumbersFollowScrollback(t *testing.T) {
	m := terminalModel(t, 160)
	p := m.panes[0]
	for i := 0; i < p.terminal.Height()+5; i++ {
		_, _ = p.terminal.Write([]byte("row\r\n"))
	}
	p.offset = 3
	body := ansi.Strip(p.paneBody())
	want := fmt.Sprintf("%6d row", p.terminal.ScrollbackLen()-p.offset+1)
	if !strings.HasPrefix(body, want) {
		t.Fatalf("want %q, got %q", want, body)
	}
}
