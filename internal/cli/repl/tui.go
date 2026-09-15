package repl

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

type tuiBatchMsg struct{ targets, commands int }
type tuiResultMsg struct{ event core.Event }
type tuiBlock struct {
	text  string
	event *core.Event
	batch int
	size  int
}
type tuiState struct {
	blocks                                          []tuiBlock
	bytes, width, height, batch, commandIndex       int
	total, running, done, failed, canceled, skipped int
	diff, stacked                                   bool
	matches                                         []string
	picked                                          map[int]bool
	pickAt                                          int
	message                                         string
	selectionStart, selectionEnd                    int
	selectionBlock                                  int
	selecting, selected                             bool
}

var (
	tuiDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	tuiTarget  = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	tuiCommand = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

func (m *model) acceptResult(e core.Event) {
	if e.State != core.Queued && e.CommandIndex != m.commandIndex {
		m.commandIndex = e.CommandIndex
		m.appendTranscript(fmt.Sprintf("Command %d: %s\n", e.CommandIndex+1, displayLabel(e.Command)))
	}
	switch e.State {
	case core.Queued:
		return
	case core.Running:
		m.running++
		return
	}
	// Jobs canceled before starting and skipped jobs do not own a running slot.
	if e.State != core.Skipped && (e.State != core.Canceled || e.Result.Err != nil) {
		m.running = max(0, m.running-1)
	}
	switch e.State {
	case core.Completed:
		m.done++
	case core.Failed:
		m.failed++
	case core.Canceled:
		m.canceled++
	case core.Skipped:
		m.skipped++
	}

	e.Target = core.ResolvedTarget{Identity: hostListName(e.Target)}
	e.Command = displayLabel(e.Command)
	e.Result.Stdout = []byte(safeTerminalText(string(e.Result.Stdout)))
	e.Result.Stderr = []byte(safeTerminalText(string(e.Result.Stderr)))
	// Keep only bounded display data, never resolver state or raw terminal escapes.
	m.addBlock(tuiBlock{event: &e, batch: m.batch})
}

func blockBody(e core.Event) string {
	var b strings.Builder
	b.Write(e.Result.Stdout)
	if len(e.Result.Stderr) > 0 {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("stderr:\n")
		b.Write(e.Result.Stderr)
	}
	if e.Err != nil {
		_, _ = fmt.Fprintf(&b, "\nerror: %s", safeTerminalText(e.Err.Error()))
	}
	if e.Result.Truncated {
		b.WriteString("\n[output truncated]")
	}
	return strings.TrimRight(strings.ReplaceAll(b.String(), "\t", "    "), "\n")
}
func (m *model) addBlock(b tuiBlock) {
	text := b.text
	if b.event != nil {
		text = blockBody(*b.event) + b.event.Target.Identity + b.event.Command + "\n"
	}
	limit := m.transcript.max
	if limit <= 0 {
		limit = core.MaxSessionOutput
		m.transcript.max = limit
	}
	// An oversized individual result becomes a bounded text block with attribution.
	size := len(text)
	if b.event != nil {
		size = max(size, len(b.event.Result.Stdout)+len(b.event.Result.Stderr)+len(b.event.Command)+len(b.event.Target.Identity))
	}
	if size > limit {
		if len(text) > limit {
			text = text[len(text)-limit:]
		}
		b = tuiBlock{text: text}
		m.transcript.truncated = true
	}
	_, _ = m.transcript.Write([]byte(text))
	b.size = len(text)
	if b.event != nil {
		b.size = size
	}
	m.blocks = append(m.blocks, b)
	m.bytes += b.size
	for (m.bytes > limit || len(m.blocks) > 4096) && len(m.blocks) > 1 {
		m.bytes -= m.blocks[0].size
		m.blocks[0] = tuiBlock{}
		m.blocks = m.blocks[1:]
		m.transcript.truncated = true
	}
	m.selected = false
	bottom := m.viewport.AtBottom()
	m.refreshLayout()
	if bottom {
		m.viewport.GotoBottom()
	}
}

func resultHeading(e core.Event, width int) string {
	status := strings.ToUpper(hostListStatus(e.State))
	if e.State == core.Failed {
		status += fmt.Sprintf(" exit %d", e.Result.ExitCode)
	}
	return tuiTarget.Bold(true).Render(ansi.Truncate("["+e.Target.Identity+"] "+status, width, "..."))
}
func resultLines(e core.Event, width int) []string {
	// Wrapping preserves full output in narrow panes; Ctrl-L provides full width.
	body := blockBody(e)
	if body == "" {
		return nil
	}
	return strings.Split(ansi.Hardwrap(body, max(1, width), true), "\n")
}

type tuiSpan struct {
	block, offset, width int
	text                 string
}
type tuiRow struct {
	styled string
	spans  []tuiSpan
}

func (m model) renderRows() []tuiRow {
	width := max(1, m.viewport.Width)
	var rows []tuiRow
	if m.transcript.truncated {
		rows = append(rows, tuiRow{styled: "[older transcript output evicted]"})
	}
	for i := 0; i < len(m.blocks); i++ {
		b := m.blocks[i]
		if len(rows) > 0 {
			rows = append(rows, tuiRow{})
		}
		if b.event == nil {
			for _, line := range strings.Split(ansi.Hardwrap(strings.TrimRight(b.text, "\n"), width, true), "\n") {
				rows = append(rows, tuiRow{styled: line})
			}
			continue
		}
		if !m.stacked && width >= 100 && i+1 < len(m.blocks) {
			next := m.blocks[i+1]
			if next.event != nil && next.batch == b.batch && next.event.CommandIndex == b.event.CommandIndex {
				col := (width - 4) / 2
				pair := strings.Split(m.renderPair(*b.event, *next.event, width), "\n")
				a, c := resultLines(*b.event, col-7), resultLines(*next.event, col-7)
				for j, line := range pair {
					row := tuiRow{styled: line}
					if j > 0 {
						if j <= len(a) {
							row.spans = append(row.spans, tuiSpan{block: i, offset: 7, width: col - 7, text: a[j-1]})
						}
						if j <= len(c) {
							row.spans = append(row.spans, tuiSpan{block: i + 1, offset: col + 11, width: col - 7, text: c[j-1]})
						}
					}
					rows = append(rows, row)
				}
				i++
				continue
			}
		}
		rows = append(rows, tuiRow{styled: resultHeading(*b.event, width)})
		for _, line := range resultLines(*b.event, width) {
			rows = append(rows, tuiRow{styled: line, spans: []tuiSpan{{block: i, width: width, text: line}}})
		}
	}
	return rows
}
func (m model) renderBlocks() string {
	rows := m.renderRows()
	lines := make([]string, len(rows))
	for i, row := range rows {
		line := row.styled
		if m.selected && i >= min(m.selectionStart, m.selectionEnd) && i <= max(m.selectionStart, m.selectionEnd) {
			for _, span := range row.spans {
				if span.block == m.selectionBlock {
					line = ansi.Cut(line, 0, span.offset) + lipgloss.NewStyle().Reverse(true).Render(padCells(span.text, span.width)) + ansi.Cut(line, span.offset+span.width, ansi.StringWidth(line))
				}
			}
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
func (m model) renderPair(left, right core.Event, width int) string {
	column := (width - 4) / 2
	// Reserve a fixed gutter only after knowing the number of wrapped lines.
	a, b := resultLines(left, column-7), resultLines(right, column-7)
	lines := []string{padCells(resultHeading(left, column), column) + "    " + resultHeading(right, column)}
	for i := 0; i < max(len(a), len(b)); i++ {
		l, r := "", ""
		if i < len(a) {
			l = a[i]
		}
		if i < len(b) {
			r = b[i]
		}
		lp, rp := l, r
		if m.diff && l != r {
			lp = lipgloss.NewStyle().Background(lipgloss.Color("52")).Render(l)
			rp = lipgloss.NewStyle().Background(lipgloss.Color("22")).Render(r)
		}
		if i < len(a) {
			lp = tuiDim.Render(fmt.Sprintf("%6d ", i+1)) + lp
		}
		if i < len(b) {
			rp = tuiDim.Render(fmt.Sprintf("%6d ", i+1)) + rp
		}
		lines = append(lines, padCells(lp, column)+"    "+rp)
	}
	return strings.Join(lines, "\n")
}
func padCells(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func (m *model) refreshLayout() {
	width, height := m.width, m.height
	if width <= 0 {
		width = m.viewport.Width
	}
	if height <= 0 {
		height = m.viewport.Height + 7
	}
	m.viewport.Width = max(1, width)
	footer := 5
	if len(m.matches) > 0 {
		footer += min(6, len(m.matches)) + 1
	}
	if m.trust != nil {
		footer += 2
	}
	m.viewport.Height = max(1, height-footer)
	m.input.Width = max(1, width-6)
	m.viewport.SetContent(m.transcriptContent())
}
func (m model) tuiView() string {
	width := max(1, m.viewport.Width)
	title := "nssh repl  |  Tab hosts  PgUp/PgDn scroll  Ctrl-L layout  Ctrl-G diff  Ctrl-Y copy"
	parts := []string{tuiDim.Render(ansi.Truncate(title, width, "")), m.viewport.View()}
	if m.trust != nil {
		p := m.trust.prompt
		warning := "Verify host key"
		if p.Changed {
			warning = "CHANGED HOST KEY: verify replacement"
		}
		parts = append(parts, safeTerminalText(fmt.Sprintf("%s for %s: %s %s\n[o] accept once  [a] trust permanently  [r] reject  Ctrl-C cancel", warning, p.Host, p.KeyType, p.Fingerprint)))
	} else {
		if len(m.matches) > 0 {
			parts = append(parts, m.pickerView())
		}
		box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Width(max(1, width-2)).Render(m.editorView(max(1, width-4)))
		parts = append(parts, box)
	}
	pending := max(0, m.total-m.running-m.done-m.failed-m.canceled-m.skipped)
	status := fmt.Sprintf("running %d  done %d  failed %d  pending %d  canceled %d  skipped %d", m.running, m.done, m.failed, pending, m.canceled, m.skipped)
	if m.active {
		status += " | Ctrl-C cancels"
	}
	if m.diff {
		status += " | diff on"
	}
	if m.message != "" {
		status += " | " + m.message
	}
	parts = append(parts, tuiDim.Render(ansi.Truncate(status, width, "")))
	return strings.Join(parts, "\n")
}

func (m model) editorView(width int) string {
	value := []rune(m.input.Value())
	pos := m.input.Position()
	if len(value) == 0 {
		return "> " + tuiDim.Render("[ 'host' ] ( 'command' )")
	}
	suggestion := ""
	_, _, matches := completeTargetToken(m.input.Value(), pos, m.candidates)
	if len(matches) > 0 {
		if token, ok := activeTargetStart(value, pos); ok {
			for i := token; i < pos; i++ {
				if value[i] == '@' {
					token = i + 1
				}
			}
			candidate := []rune(matches[0])
			if pos-token <= len(candidate) {
				suggestion = string(candidate[pos-token:])
			}
		}
	}
	start := 0
	for start < pos && ansi.StringWidth(string(value[start:pos])) > max(1, width-5) {
		start++
	}
	var out strings.Builder
	out.WriteString("> ")
	visible := 2
	inTargets := true
	for i, r := range value {
		if r == ']' {
			inTargets = false
		}
		if i < start {
			continue
		}
		if visible >= width-1 {
			break
		}
		if i == pos && suggestion != "" {
			ghost := ansi.Truncate(displayLabel(suggestion), max(0, width-visible-1), "")
			out.WriteString(tuiDim.Render(ghost))
			visible += ansi.StringWidth(ghost)
		}
		visible += ansi.StringWidth(string(r))
		style := tuiCommand
		if inTargets {
			style = tuiTarget
		}
		if strings.ContainsRune("[]()'", r) {
			style = tuiDim
		}
		if i == pos {
			style = style.Reverse(true)
		}
		out.WriteString(style.Render(safeTerminalText(string(r))))
	}
	if pos == len(value) {
		out.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "))
	}

	return ansi.Truncate(out.String(), width, "")
}
func (m *model) openPicker() {
	value, cursor, matches := completeTargetToken(m.input.Value(), m.input.Position(), m.candidates)
	if len(matches) == 1 {
		m.input.SetValue(value)
		m.input.SetCursor(cursor)
		return
	}
	m.matches = matches
	m.pickAt = 0
	m.picked = map[int]bool{}
	m.refreshLayout()
}
func (m model) pickerView() string {
	var rows []string
	start := max(0, m.pickAt-5)
	for i := start; i < min(len(m.matches), start+6); i++ {
		marker := "[ ]"
		if m.picked[i] {
			marker = "[x]"
		}
		row := marker + " " + displayLabel(m.matches[i])
		if i == m.pickAt {
			row = "> " + row
		} else {
			row = "  " + row
		}
		rows = append(rows, ansi.Truncate(row, m.viewport.Width, "..."))
	}
	return strings.Join(rows, "\n") + "\n" + tuiDim.Render("Space select | Enter insert | Esc close")
}
func (m *model) updatePicker(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEsc:
		m.matches = nil
	case tea.KeyUp:
		m.pickAt = max(0, m.pickAt-1)
	case tea.KeyDown, tea.KeyTab:
		m.pickAt = min(len(m.matches)-1, m.pickAt+1)
	case tea.KeySpace:
		m.picked[m.pickAt] = !m.picked[m.pickAt]
	case tea.KeyEnter:
		var chosen []string
		for i, name := range m.matches {
			if m.picked[i] {
				chosen = append(chosen, name)
			}
		}
		if len(chosen) == 0 {
			chosen = []string{m.matches[m.pickAt]}
		}
		m.insertHosts(chosen)
		m.matches = nil
	}
	m.refreshLayout()
}
func (m *model) insertHosts(hosts []string) {
	value := []rune(m.input.Value())
	pos := m.input.Position()
	start, ok := activeTargetStart(value, pos)
	if !ok {
		return
	}
	end := pos
	for end < len(value) && value[end] != '\'' {
		end++
	}
	// Preserve an explicitly typed username for every selected host.
	prefix := string(value[start:pos])
	user := ""
	if at := strings.LastIndex(prefix, "@"); at >= 0 {
		user = prefix[:at+1]
	}
	escaped := make([]string, len(hosts))
	for i, host := range hosts {
		escaped[i] = strings.ReplaceAll(user+host, "'", "\\'")
	}
	replacement := strings.Join(escaped, "', '")
	m.input.SetValue(string(value[:start]) + replacement + string(value[end:]))
	m.input.SetCursor(start + len([]rune(replacement)))
}

// Mouse selection copies only the chosen pane's displayed lines. Clipboard
// access is explicit (Ctrl-Y), never triggered by remote terminal sequences.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.trust != nil {
		return m, nil
	}
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	if msg.Action == tea.MouseActionRelease {
		m.selecting = false
		return m, nil
	}
	row := msg.Y - 1 + m.viewport.YOffset
	if msg.Y < 1 || msg.Y > m.viewport.Height {
		return m, nil
	}
	rows := m.renderRows()
	if row < 0 || row >= len(rows) {
		return m, nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.selected = false
		m.selecting = false
		for _, span := range rows[row].spans {
			if msg.X >= span.offset && msg.X < span.offset+span.width {
				m.selectionStart = row
				m.selectionEnd = row
				m.selectionBlock = span.block
				m.selected = true
				m.selecting = true
				break
			}
		}
	} else if msg.Action == tea.MouseActionMotion && m.selecting {
		m.selectionEnd = row
	}
	if m.selected {
		m.message = "lines selected; Ctrl-Y copies"
	}
	m.viewport.SetContent(m.renderBlocks())
	return m, nil
}
func (m model) selectedText() string {
	if !m.selected {
		return ""
	}
	rows := m.renderRows()
	start, end := min(m.selectionStart, m.selectionEnd), max(m.selectionStart, m.selectionEnd)
	if start < 0 || start >= len(rows) {
		return ""
	}
	end = min(end, len(rows)-1)
	var selected []string
	for _, row := range rows[start : end+1] {
		for _, span := range row.spans {
			if span.block == m.selectionBlock {
				selected = append(selected, span.text)
			}
		}
	}
	return strings.Join(selected, "\n")
}
func (m model) copySelection() tea.Cmd {
	text := m.selectedText()
	if text == "" || len(text) > 64<<10 {
		return nil
	}
	return func() tea.Msg {
		_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
		return tuiCopyMsg{err: err}
	}
}

type tuiCopyMsg struct{ err error }
