package repl

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/connect"
	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/session"
)

type interactiveState struct {
	interactive, choosing, direct bool
	batchDraft                    string
	panes                         []*terminalPane
	group                         int
	groupCancel                   context.CancelFunc
	target                        int // -1 broadcasts; otherwise one pane
	page                          int
	interactiveHistory            []string
	interactiveHistoryAt          int
	broadcastPaused               bool
}
type terminalPane struct {
	name                         string
	terminal                     *vt.Emulator
	wire                         *terminalWire
	state                        string
	offset                       int
	selected, selecting          bool
	selectionStart, selectionEnd int
	selectionText                string
}

// Emulator responses go only to their originating terminal, never broadcast.
// Keep this path independent of the UI event loop, since emulator writes may
// synchronously wait for a terminal-query response to be read.
type terminalWire struct {
	mu      sync.Mutex
	shell   *session.Session
	pending [][]byte
}

func (w *terminalWire) send(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.shell == nil {
		if len(w.pending) < 32 {
			w.pending = append(w.pending, append([]byte(nil), data...))
		}
		return nil
	}
	return w.shell.Send(data)
}
func (w *terminalWire) attach(s *session.Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shell = s
	for _, b := range w.pending {
		_ = s.Send(b)
	}
	w.pending = nil
}
func (w *terminalWire) alive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.shell != nil && w.shell.Alive()
}
func (w *terminalWire) resize(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.shell != nil {
		w.shell.Resize(x, y)
	}
}

type terminalCopyMsg struct {
	group, index int
	text         string
	clear        bool
	err          error
}
type terminalTargetsMsg struct {
	group   int
	targets []core.ResolvedTarget
	err     error
}
type terminalStartedMsg struct {
	group, index int
	shell        *session.Session
}
type terminalOutputMsg struct {
	group, index int
	data         []byte
}
type terminalClosedMsg struct {
	group, index int
	err          error
}

func (m *model) closePanes() {
	if m.groupCancel != nil {
		m.groupCancel()
		m.groupCancel = nil
	}
	for _, p := range m.panes {
		if closer, ok := p.terminal.InputPipe().(io.Closer); ok {
			_ = closer.Close()
		}
	}
	m.panes = nil
	m.group++
	m.broadcastPaused = false
	m.interactiveHistory = nil
	m.interactiveHistoryAt = 0
	m.direct = false
}
func (m *model) beginInteractive(targets []core.Target) {
	m.choosing = false
	m.input.SetValue("")
	m.pickerOpen = false
	m.matches = nil
	m.target = -1
	m.page = 0
	m.message = "Resolving devices..."
	m.group++
	ctx, cancel := context.WithCancel(m.owner.ctx)
	m.groupCancel = cancel
	group := m.group
	owner := m.owner
	owner.workers.Add(1)
	go func() {
		defer owner.workers.Done()
		cfg, err := config.LoadDefault()
		var resolved []core.ResolvedTarget
		if err == nil {
			cat, e := connect.BuildHostCatalog(cfg)
			err = e
			if err == nil {
				seen := map[string]bool{}
				for _, spec := range targets {
					ts, e := catalogResolver(cfg, cat)(ctx, spec)
					if e != nil {
						err = e
						break
					}
					for _, t := range ts {
						if !seen[t.Identity] {
							seen[t.Identity] = true
							resolved = append(resolved, t)
						}
					}
				}
			}
		}
		if err == nil && (len(resolved) == 0 || len(resolved) > 16) {
			err = fmt.Errorf("interactive mode requires 1-16 devices")
		}
		owner.program.Send(terminalTargetsMsg{group, resolved, err})
	}()
}
func (m *model) startTerminals(targets []core.ResolvedTarget) {
	m.message = "Opening SSH terminals; wait for each device's prompt before sending input"
	group, owner := m.group, m.owner
	// Each group has its own lifetime; switching back to batch closes all panes.
	ctx, cancel := context.WithCancel(owner.ctx)
	old := m.groupCancel
	m.groupCancel = func() {
		cancel()
		if old != nil {
			old()
		}
	}
	for _, target := range targets {
		emu := newTerminalEmulator(80, 24)
		emu.SetScrollbackSize(1000)
		wire := &terminalWire{}
		p := &terminalPane{name: target.Identity, terminal: emu, wire: wire, state: "opening"}
		m.panes = append(m.panes, p)
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := emu.Read(buf)
				if n > 0 {
					_ = wire.send(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()
	}
	m.resizePanes()
	sem := make(chan struct{}, max(1, min(m.concurrency, len(targets))))
	for index, target := range targets {
		p := m.panes[index]
		w, h := p.terminal.Width(), p.terminal.Height()
		owner.workers.Add(1)
		go func() {
			defer owner.workers.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			request := target.Value.(targetRequest)
			resolved, err := connect.ResolveLiteralHostFromCatalog(ctx, request.host, request.user, request.cfg, request.cat)
			released := false
			release := func() {
				if !released {
					<-sem
					released = true
				}
			}
			defer release()
			if err == nil {
				opts := connect.CaptureOptions{HostKeyPrompt: func(prompt connector.HostKeyPrompt) connector.HostKeyAction { return owner.prompt(ctx, prompt) }}
				err = connect.RunTerminal(ctx, resolved, opts, session.Options{Width: w, Height: h, Output: func(data []byte) { owner.program.Send(terminalOutputMsg{group, index, data}) }}, func(s *session.Session) {
					release()
					p.wire.attach(s)
					owner.program.Send(terminalStartedMsg{group, index, s})
				})
			}
			owner.program.Send(terminalClosedMsg{group, index, err})
		}()
	}
}

func (m *model) resizePanes() {
	n := min(4, len(m.panes))
	if n == 0 {
		return
	}
	width, height := max(20, m.width), max(10, m.height)
	cols := 1
	if width >= 120 && n > 1 {
		cols = 2
	}
	rows := (n + cols - 1) / cols
	w, h := max(8, width/cols-2), max(2, (height-6)/rows-3)
	for _, p := range m.panes {
		p.selected = false
		p.selecting = false
		p.terminal.Resize(w, h)
		p.wire.resize(w, h)
	}
}
func (m model) broadcastLabel() string {
	if m.target >= 0 && m.target < len(m.panes) {
		return "Sending to: " + displayLabel(m.panes[m.target].name)
	}
	names := make([]string, len(m.panes))
	for i, p := range m.panes {
		names[i] = displayLabel(p.name)
	}
	return "Sending to ALL: " + strings.Join(names, ", ")
}
func (m *model) sendInteractive(data []byte) {
	if len(m.panes) == 0 {
		m.message = "No open sessions"
		return
	}
	if m.target < 0 && m.broadcastPaused {
		m.message = "Broadcast paused: focus a pane with :target N; :all explicitly resumes"
		return
	}
	targets := m.panes
	if m.target >= 0 {
		targets = []*terminalPane{m.panes[m.target]}
	}
	for _, p := range targets {
		if !p.wire.alive() {
			m.message = "Input not sent: " + p.name + " is not open"
			m.broadcastPaused = true
			return
		}
	}
	for _, p := range targets {
		if err := p.wire.send(data); err != nil {
			m.message = "Input failed for " + p.name + ": " + err.Error()
			m.broadcastPaused = true
			return
		}
	}
	m.message = ""
	for _, p := range targets {
		p.offset = 0
	}
}

func (m model) updateInteractive(msg tea.Msg) (model, tea.Cmd, bool) {
	switch v := msg.(type) {
	case *trustRequest, trustFinishedMsg:
		// Connection workers use the shared modal handler in both modes.
		// Do not consume these as terminal input or cursor-blink events.
		return m, nil, false
	case terminalCopyMsg:
		if v.group == m.group && v.index < len(m.panes) {
			p := m.panes[v.index]
			if v.err != nil {
				m.message = "Clipboard write failed"
			} else {
				m.message = "Selection copied"
				if v.clear && p.selectionText == v.text {
					p.selected = false
					p.selecting = false
					p.selectionText = ""
				}
			}
		}
		return m, nil, true
	case terminalTargetsMsg:
		if v.group != m.group {
			return m, nil, true
		}
		if v.err != nil {
			m.message = v.err.Error()
			m.choosing = true
			m.input.SetValue("[ '' ] ( '' )")
			m.input.SetCursor(3)
		} else {
			m.startTerminals(v.targets)
		}
		return m, nil, true
	case terminalStartedMsg:
		if v.group == m.group && v.index < len(m.panes) {
			p := m.panes[v.index]
			p.state = "open"
			v.shell.Resize(p.terminal.Width(), p.terminal.Height())
		}
		return m, nil, true
	case terminalOutputMsg:
		if v.group == m.group && v.index < len(m.panes) {
			p := m.panes[v.index]
			p.selected = false
			p.selecting = false
			p.selectionText = ""
			_, _ = p.terminal.Write(v.data)
		}
		return m, nil, true
	case terminalClosedMsg:
		if v.group == m.group && v.index < len(m.panes) {
			p := m.panes[v.index]
			p.state = "closed"
			if v.err != nil {
				p.state += " (" + displayLabel(v.err.Error()) + ")"
			}
			m.broadcastPaused = true
			m.message = "A session closed; broadcast paused. No commands were replayed."
		}
		return m, nil, true
	}
	if !m.interactive {
		return m, nil, false
	}
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		v := msg.(tea.WindowSizeMsg)
		m.width, m.height = v.Width, v.Height
		m.resizePanes()
		m.refreshLayout()
		return m, nil, true
	}
	if m.helpOpen || m.trust != nil {
		return m, nil, false
	}
	if m.choosing {
		if key, ok := msg.(tea.KeyMsg); ok && !m.pickerOpen {
			if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
				m.interactive = false
				m.choosing = false
				m.input.SetValue(m.batchDraft)
				m.refreshLayout()
				return m, nil, true
			}
			if key.Type == tea.KeyEnter {
				switch strings.TrimSpace(m.input.Value()) {
				case ":help":
					m.helpOpen = true
					m.helpOffset = 0
					m.input.SetValue("[ '' ] ( '' )")
					m.input.SetCursor(3)
					return m, nil, true
				case ":quit", ":exit":
					m.closePanes()
					return m, tea.Quit, true
				case ":batch", ":mode batch":
					m.closePanes()
					m.interactive = false
					m.choosing = false
					m.input.SetValue("")
					m.refreshLayout()
					return m, nil, true
				}
				devices, _ := m.formFields()
				if len(devices) == 0 {
					m.message = "Choose devices with Tab"
					return m, nil, true
				}
				values := []rune(m.input.Value())
				var names []string
				for _, d := range devices {
					names = append(names, string(values[d.start-1:d.end+1]))
				}
				sub, err := core.Parse("[ " + strings.Join(names, ", ") + " ] ( 'unused' )")
				if err != nil {
					m.message = err.Error()
					return m, nil, true
				}
				m.beginInteractive(sub.Targets)
				return m, nil, true
			}
		}
		return m, nil, false
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		index := m.paneAt(mouse.X, mouse.Y)
		if mouse.Button == tea.MouseButtonRight {
			return m, m.copyTerminalSelection(true), true
		}
		if index >= 0 {
			p := m.panes[index]
			row := (mouse.Y-1)%(p.terminal.Height()+3) - 2
			switch mouse.Button {
			case tea.MouseButtonLeft:
				if mouse.Action == tea.MouseActionPress {
					if row < 0 {
						m.target = index
						m.message = "Focused one device; :all restores broadcast"
					}
					for _, other := range m.panes {
						other.selected = false
						other.selecting = false
					}
					if row >= 0 && row < p.terminal.Height() && p.hasOutputRow(row) {
						p.selected = true
						p.selecting = true
						p.selectionStart = row
						p.selectionEnd = row
						p.captureSelection()
					}
				}
				if mouse.Action == tea.MouseActionMotion && p.selecting {
					p.selectionEnd = max(0, min(p.terminal.Height()-1, row))
					p.captureSelection()
				}
			case tea.MouseButtonWheelUp:
				p.offset = min(p.terminal.ScrollbackLen(), p.offset+3)
				p.selected = false
			case tea.MouseButtonWheelDown:
				p.offset = max(0, p.offset-3)
				p.selected = false
			}
		}
		if mouse.Action == tea.MouseActionRelease {
			for _, p := range m.panes {
				p.selecting = false
			}
		}
		return m, nil, true
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd, true
	}
	if key.Type == tea.KeyCtrlCloseBracket {
		m.direct = !m.direct
		m.message = ""
		return m, nil, true
	}
	if m.direct {
		var data []byte
		switch key.Type {
		case tea.KeyRunes, tea.KeySpace:
			data = []byte(string(key.Runes))
		case tea.KeyEnter:
			data = []byte("\r")
		case tea.KeyTab:
			data = []byte("\t")
		case tea.KeyBackspace:
			data = []byte{127}
		case tea.KeyEsc:
			data = []byte{27}
		case tea.KeyUp:
			data = []byte("\x1b[A")
		case tea.KeyDown:
			data = []byte("\x1b[B")
		case tea.KeyRight:
			data = []byte("\x1b[C")
		case tea.KeyLeft:
			data = []byte("\x1b[D")
		case tea.KeyHome:
			data = []byte("\x1b[H")
		case tea.KeyEnd:
			data = []byte("\x1b[F")
		case tea.KeyDelete:
			data = []byte("\x1b[3~")
		case tea.KeyInsert:
			data = []byte("\x1b[2~")
		case tea.KeyPgUp:
			data = []byte("\x1b[5~")
		case tea.KeyPgDown:
			data = []byte("\x1b[6~")
		case tea.KeyShiftTab:
			data = []byte("\x1b[Z")
		case tea.KeyF1:
			data = []byte("\x1bOP")
		case tea.KeyF2:
			data = []byte("\x1bOQ")
		case tea.KeyF3:
			data = []byte("\x1bOR")
		case tea.KeyF4:
			data = []byte("\x1bOS")

		default:
			if key.Type >= 0 && key.Type < 32 {
				data = []byte{byte(key.Type)}
			}
		}
		if key.Alt {
			data = append([]byte{27}, data...)
		}
		if len(data) > 0 {
			m.sendInteractive(data)
		}
		return m, nil, true
	}
	switch key.Type {
	case tea.KeyCtrlC:
		m.sendInteractive([]byte{3})
		return m, nil, true
	case tea.KeyCtrlD:
		m.sendInteractive([]byte{4})
		return m, nil, true
	case tea.KeyCtrlY:
		return m, m.copyTerminalSelection(false), true
	case tea.KeyCtrlK:
		for _, p := range m.panes {
			p.clearScrollback()
		}
		m.message = "Terminal scrollback cleared"
		return m, nil, true
	case tea.KeyPgUp, tea.KeyPgDown:
		for i, p := range m.panes {
			if m.target < 0 || i == m.target {
				if key.Type == tea.KeyPgUp {
					p.offset = min(p.terminal.ScrollbackLen(), p.offset+p.terminal.Height())
				} else {
					p.offset = max(0, p.offset-p.terminal.Height())
				}
			}
		}
		return m, nil, true
	case tea.KeyUp, tea.KeyCtrlP:
		if len(m.interactiveHistory) > 0 {
			m.interactiveHistoryAt = max(0, m.interactiveHistoryAt-1)
			m.input.SetValue(m.interactiveHistory[m.interactiveHistoryAt])
			m.input.CursorEnd()
		}
		return m, nil, true
	case tea.KeyDown, tea.KeyCtrlN:
		m.interactiveHistoryAt = min(len(m.interactiveHistory), m.interactiveHistoryAt+1)
		if m.interactiveHistoryAt < len(m.interactiveHistory) {
			m.input.SetValue(m.interactiveHistory[m.interactiveHistoryAt])
		} else {
			m.input.SetValue("")
		}
		m.input.CursorEnd()
		return m, nil, true
	case tea.KeyTab:
		// Flush the local draft and switch to direct keys for remote completion.
		m.sendInteractive([]byte(m.input.Value() + "\t"))
		if m.message != "" {
			return m, nil, true
		}
		m.input.SetValue("")
		m.direct = true
		return m, nil, true
	case tea.KeyEnter:
		line := m.input.Value()
		switch {
		case line == ":batch" || line == ":mode batch" || line == ":disconnect":
			m.closePanes()
			m.interactive = false
			m.input.SetValue(m.batchDraft)
			m.refreshLayout()
			return m, nil, true
		case line == ":quit" || line == ":exit":
			m.closePanes()
			return m, tea.Quit, true
		case line == ":help":
			m.helpOpen = true
			m.helpOffset = 0
			m.input.SetValue("")
			return m, nil, true
		case line == ":keys":
			m.direct = true
		case line == ":all":
			m.target = -1
			m.broadcastPaused = false
			m.message = "Broadcast targets all open panes"
		case strings.HasPrefix(line, ":target "):
			var n int
			if _, err := fmt.Sscanf(line, ":target %d", &n); err != nil || n < 1 || n > len(m.panes) {
				m.message = "Use :target N with a pane number"
			} else {
				m.target = n - 1
				m.page = (n - 1) / 4
				m.message = "Focused " + m.panes[n-1].name
			}
		case line == ":next":
			m.page = min((len(m.panes)-1)/4, m.page+1)
		case line == ":prev":
			m.page = max(0, m.page-1)
		case line == ":clear":
			for _, p := range m.panes {
				p.clearScrollback()
			}
		case line == ":wipe":
			m.interactiveHistory = nil
			m.interactiveHistoryAt = 0
			for _, p := range m.panes {
				p.clearScrollback()
			}
		case strings.HasPrefix(line, ":"):
			m.message = "Unknown TUI command; :help lists controls. Prefix a remote colon command with a space."
		default:
			m.sendInteractive([]byte(line + "\r"))
			if m.message != "" {
				return m, nil, true
			}
			if m.message == "" && strings.TrimSpace(line) != "" {
				m.interactiveHistory = boundedHistory(append(m.interactiveHistory, line))
				m.interactiveHistoryAt = len(m.interactiveHistory)
			}
		}
		m.input.SetValue("")
		return m, nil, true
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd, true
}

func (m model) paneAt(x, y int) int {
	if len(m.panes) == 0 || y < 1 {
		return -1
	}
	w, h := m.panes[0].terminal.Width()+2, m.panes[0].terminal.Height()+3
	cols := 1
	if m.width >= 120 && len(m.panes) > 1 {
		cols = 2
	}
	col, row := x/w, (y-1)/h
	if col >= cols || y >= max(1, m.height)-5 {
		return -1
	}
	i := m.page*4 + row*cols + col
	if i >= len(m.panes) || i >= m.page*4+4 {
		return -1
	}
	return i
}
func (p *terminalPane) plainBody() string {
	if p.offset == 0 {
		return p.terminal.Render()
	}
	// Historical rows are plain text; screen contents retain terminal styling.
	w, h := p.terminal.Width(), p.terminal.Height()
	start := p.terminal.ScrollbackLen() - p.offset
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			var content string
			if start+y < p.terminal.ScrollbackLen() {
				if c := p.terminal.ScrollbackCellAt(x, start+y); c != nil {
					content = c.Content
					x += max(1, c.Width) - 1
				}
			} else {
				if c := p.terminal.CellAt(x, start+y-p.terminal.ScrollbackLen()); c != nil {
					content = c.Content
					x += max(1, c.Width) - 1
				}
			}
			if content == "" {
				content = " "
			}
			b.WriteString(content)
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}
func (p *terminalPane) captureSelection() {
	lines := strings.Split(ansi.Strip(p.plainBody()), "\n")
	first, last := min(p.selectionStart, p.selectionEnd), max(p.selectionStart, p.selectionEnd)
	var selected []string
	for i := first; i <= last && i < len(lines); i++ {
		selected = append(selected, strings.TrimRight(lines[i], " "))
	}
	p.selectionText = strings.Join(selected, "\n")
}
func (p *terminalPane) paneBody() string {
	lines := strings.Split(p.plainBody(), "\n")
	if p.selected {
		for i := min(p.selectionStart, p.selectionEnd); i <= max(p.selectionStart, p.selectionEnd) && i < len(lines); i++ {
			lines[i] = lipgloss.NewStyle().Reverse(true).Render(padCells(ansi.Strip(lines[i]), p.terminal.Width()))
		}
	}
	return strings.Join(lines, "\n")
}
func (m model) copyTerminalSelection(clear bool) tea.Cmd {
	for i, p := range m.panes {
		if p.selected && p.selectionText != "" && len(p.selectionText) <= 64<<10 {
			group, text := m.group, p.selectionText
			return func() tea.Msg {
				_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
				return terminalCopyMsg{group, i, text, clear, err}
			}
		}
	}
	return nil
}
func (m model) interactiveView() string {
	width, height := max(20, m.width), max(10, m.height)
	rows := []string{ansi.Truncate(m.broadcastLabel(), width, "...")}
	cols := 1
	if width >= 120 && len(m.panes) > 1 {
		cols = 2
	}
	var current []string
	for i := m.page * 4; i < min(len(m.panes), m.page*4+4); i++ {
		p := m.panes[i]
		title := fmt.Sprintf("%d [%s] %s", i+1, displayLabel(p.name), p.state)
		color := lipgloss.Color("8")
		if m.target < 0 || m.target == i {
			color = lipgloss.Color("10")
		}
		pane := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(color).Width(p.terminal.Width()).Render(ansi.Truncate(title, p.terminal.Width(), "...") + "\n" + p.paneBody())
		current = append(current, pane)
		if len(current) == cols || i == min(len(m.panes), m.page*4+4)-1 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, current...))
			current = nil
		}
	}
	body := strings.Join(rows, "\n")
	body = lipgloss.NewStyle().Height(max(1, height-5)).MaxHeight(max(1, height-5)).Render(body)
	input := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Width(width - 2).Render(m.editorView(width - 4))
	if m.direct {
		input = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Width(width - 2).Render("Direct keyboard input -> selected panes | Ctrl-] returns to command bar")
	}
	status := "interactive | " + m.message
	if len(m.panes) > 4 {
		status = fmt.Sprintf("interactive | page %d/%d | ", m.page+1, (len(m.panes)+3)/4) + m.message
	}
	status = padCells(ansi.Truncate(status, max(1, width-6), ""), width-5) + ":help"
	view := body + "\n" + input + "\n" + status
	if m.trust != nil {
		p := m.trust.prompt
		warning := "Verify host key"
		if p.Changed {
			warning = "CHANGED HOST KEY: verify replacement"
		}
		view = body + "\n" + safeTerminalText(fmt.Sprintf("%s for %s: %s %s\n[o] once [a] always [r] reject", warning, p.Host, p.KeyType, p.Fingerprint)) + "\n" + status
	}
	if m.helpOpen {
		return m.helpOverlay(view)
	}
	return view
}

func (p *terminalPane) clearScrollback() {
	p.terminal.ClearScrollback()
	p.offset = 0
	p.selected = false
	p.selecting = false
	p.selectionText = ""
}

func (p *terminalPane) hasOutputRow(row int) bool {
	lines := strings.Split(ansi.Strip(p.plainBody()), "\n")
	return row >= 0 && row < len(lines) && strings.TrimSpace(lines[row]) != ""
}

// The pinned VT library incorrectly answers ANSI DSR 5 with a DEC-private
// response (CSI ? 0 n). Use the ANSI response so a remote CLI does not receive
// an unexpected private sequence as keyboard input. Leave other queries to VT.
// https://invisible-island.net/xterm/ctlseqs/ctlseqs.html
func newTerminalEmulator(w, h int) *vt.Emulator {
	emu := vt.NewEmulator(w, h)
	emu.RegisterCsiHandler('n', func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 0)
		if !ok || n != 5 {
			return false
		}
		_, _ = io.WriteString(emu.InputPipe(), "\x1b[0n")
		return true
	})
	return emu
}
