package repl

import (
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
	m.input.SetValue(":batch")
	m, _, _ = m.updateInteractive(tea.KeyMsg{Type: tea.KeyEnter})
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
	x := m.panes[0].terminal.Width() + 3
	next, _ := m.Update(tea.MouseMsg{X: x, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(model)
	if m.target != -1 || !m.panes[1].selected {
		t.Fatal("output selection changed broadcast targets")
	}
	next, _ = m.Update(tea.MouseMsg{X: x, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
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
	m, _, _ = m.updateInteractive(tea.KeyMsg{Type: tea.KeyCtrlK})
	m, _, _ = m.updateInteractive(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("local")})
	if !m.controlOpen || m.input.Value() != "local" || strings.Contains(m.message, "Input not sent") {
		t.Fatal("control input leaked")
	}
	m, _, _ = m.updateInteractive(tea.KeyMsg{Type: tea.KeyEsc})
	if m.controlOpen || m.input.Value() != "" {
		t.Fatal("control draft retained")
	}
	m, _, _ = m.updateInteractive(tea.KeyMsg{Type: tea.KeySpace})
	if !strings.Contains(m.message, "Input not sent") {
		t.Fatal("space did not reach terminal path")
	}
	if strings.Contains(ansi.Strip(m.View()), "Type a command") {
		t.Fatal("command box remains")
	}
}
