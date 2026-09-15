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
	bytes, width, height, batch                     int
	total, running, done, failed, canceled, skipped int
	diff, stacked                                   bool
	helpOpen                                        bool
	helpOffset                                      int
	pickerOpen                                      bool
	pickerDraft                                     string
	pickerCursor                                    int
	matches                                         []string
	picked                                          map[string]bool
	pickAt                                          int
	message                                         string
	selectionStart, selectionEnd                    int
	selectionBlock                                  int
	selectionHeader                                 bool
	selectionBodyStart                              int
	selecting, selected                             bool
}

var (
	tuiDim     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "248"})
	tuiTarget  = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	tuiCommand = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

func (m *model) acceptResult(e core.Event) {
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

func resultLabel(e core.Event) string {
	status := strings.ToUpper(hostListStatus(e.State))
	if e.State == core.Failed {
		status += fmt.Sprintf(" exit %d", e.Result.ExitCode)
	}
	command := strings.ReplaceAll(displayLabel(e.Command), "'", "\\'")
	return status + ":  [" + e.Target.Identity + "] ('" + command + "')"
}

func resultHeading(e core.Event, width int) string {
	return hostListColor(os.Stdout, ansi.Truncate(resultLabel(e), width, "..."), e.State)
}

// Device tables often pad rows to a fixed width. Trim only trailing display
// padding before wrapping; indentation, column spacing, and real blank rows stay.
func sourceLines(e core.Event) []string {
	body := blockBody(e)
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return lines
}
func wrapResultLine(line string, width int) []string {
	return strings.Split(ansi.Hardwrap(line, max(1, width), true), "\n")
}
func resultLines(e core.Event, width int) []string {
	var lines []string
	for _, line := range sourceLines(e) {
		lines = append(lines, wrapResultLine(line, width)...)
	}
	return lines
}

type tuiSpan struct {
	block, offset, width int
	text                 string
}
type tuiRow struct {
	heading bool
	command string
	styled  string
	spans   []tuiSpan
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
			if next.event != nil && next.batch == b.batch && next.event.CommandIndex == b.event.CommandIndex && resultsFitPair(*b.event, *next.event, width) {
				rows = append(rows, m.renderPair(*b.event, *next.event, width, i)...)
				i++
				continue
			}
		}
		rows = append(rows, tuiRow{heading: true, command: b.event.Command, styled: resultHeading(*b.event, width), spans: []tuiSpan{{block: i, width: width, text: resultLabel(*b.event)}}})
		for _, line := range resultLines(*b.event, width) {
			rows = append(rows, tuiRow{command: b.event.Command, styled: line, spans: []tuiSpan{{block: i, width: width, text: line}}})
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
					line = ansi.Cut(line, 0, span.offset) + lipgloss.NewStyle().Reverse(true).Render(padCells(ansi.Truncate(span.text, span.width, "..."), span.width)) + ansi.Cut(line, span.offset+span.width, ansi.StringWidth(line))
				}
			}
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// Include the four-cell gap and seven-cell line-number gutter when deciding
// whether both devices fit. Measure the same normalized rows used by rendering.
func resultsFitPair(left, right core.Event, width int) bool {
	column := (width - 4) / 2
	for _, event := range []core.Event{left, right} {
		if ansi.StringWidth(resultLabel(event)) > column {
			return false
		}
		for _, line := range sourceLines(event) {
			if ansi.StringWidth(line) > column-7 {
				return false
			}
		}
	}
	return column > 7
}

func (m model) renderPair(left, right core.Event, width, block int) []tuiRow {
	column := (width - 4) / 2
	a, b := sourceLines(left), sourceLines(right)
	rows := []tuiRow{{heading: true, command: left.Command, styled: padCells(resultHeading(left, column), column) + "    " + resultHeading(right, column), spans: []tuiSpan{{block: block, width: column, text: resultLabel(left)}, {block: block + 1, offset: column + 4, width: column, text: resultLabel(right)}}}}
	// Pair original rows first. Wrapping either side adds continuation cells to
	// that pair, never shifts the next source row or consumes another line number.
	for i := 0; i < max(len(a), len(b)); i++ {
		var l, r []string
		if i < len(a) {
			l = wrapResultLine(a[i], column-7)
		}
		if i < len(b) {
			r = wrapResultLine(b[i], column-7)
		}
		differs := i >= len(a) || i >= len(b)
		if !differs {
			differs = a[i] != b[i]
		}
		for j := 0; j < max(len(l), len(r)); j++ {
			row := tuiRow{command: left.Command}
			cells := [2]string{}
			for side, lines := range [][]string{l, r} {
				if j >= len(lines) {
					continue
				}
				text := lines[j]
				painted := text
				if m.diff && differs {
					color := lipgloss.Color("52")
					if side == 1 {
						color = lipgloss.Color("22")
					}
					painted = lipgloss.NewStyle().Background(color).Render(text)
				}
				gutter := "       "
				if j == 0 {
					gutter = tuiDim.Render(fmt.Sprintf("%6d ", i+1))
				}
				cells[side] = gutter + painted
				row.spans = append(row.spans, tuiSpan{block: block + side, offset: side*(column+4) + 7, width: column - 7, text: text})
			}
			row.styled = padCells(cells[0], column) + "    " + cells[1]
			rows = append(rows, row)
		}
	}
	return rows
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
	footer := 2 + lipgloss.Height(m.formsView(max(1, width)))
	if m.pickerOpen {
		footer += max(1, min(6, len(m.matches)))
	}
	if m.trust != nil {
		footer += 2
	}
	m.viewport.Height = max(1, height-footer)
	m.input.Width = max(1, width-6)
	m.viewport.SetContent(m.transcriptContent())
}

// Pin the device status row belonging to the top visible output. Paired
// results retain both device headings and their independent selection spans.
func (m model) stickyHeader() (tuiRow, int) {
	rows := m.renderRows()
	top := min(m.viewport.YOffset, len(rows))
	for i := top; i < len(rows); i++ {
		if len(rows[i].spans) == 0 {
			continue
		}
		block := rows[i].spans[0].block
		for j := i; j >= 0; j-- {
			if rows[j].heading && rows[j].spans[0].block == block {
				return rows[j], j
			}
		}
	}
	return tuiRow{}, -1
}
func (m model) bodyOffset() int {
	_, index := m.stickyHeader()
	if index == m.viewport.YOffset {
		return index + 1
	}
	return m.viewport.YOffset
}
func (m model) commandHeader() string {
	row, index := m.stickyHeader()
	if index < 0 {
		return ""
	}
	if m.selected && (m.selectionHeader || index >= min(m.selectionStart, m.selectionEnd) && index <= max(m.selectionStart, m.selectionEnd)) {
		for _, span := range row.spans {
			if span.block == m.selectionBlock {
				row.styled = ansi.Cut(row.styled, 0, span.offset) + lipgloss.NewStyle().Reverse(true).Render(padCells(ansi.Truncate(span.text, span.width, "..."), span.width)) + ansi.Cut(row.styled, span.offset+span.width, ansi.StringWidth(row.styled))
			}
		}
	}
	return row.styled
}
func (m model) bodyView() string {
	view := m.viewport.View()
	if m.bodyOffset() > m.viewport.YOffset {
		lines := strings.Split(view, "\n")
		if len(lines) > 0 {
			return strings.Join(append(lines[1:], ""), "\n")
		}
	}
	return view
}

func (m model) tuiView() string {
	width := max(1, m.viewport.Width)
	parts := []string{m.commandHeader(), m.bodyView()}
	switch {
	case m.trust != nil:
		p := m.trust.prompt
		warning := "Verify host key"
		if p.Changed {
			warning = "CHANGED HOST KEY: verify replacement"
		}
		parts = append(parts, safeTerminalText(fmt.Sprintf("%s for %s: %s %s\n[o] accept once  [a] trust permanently  [r] reject  Ctrl-C cancel", warning, p.Host, p.KeyType, p.Fingerprint)))
	default:
		if m.pickerOpen {
			parts = append(parts, m.pickerView())
		}
		parts = append(parts, m.formsView(width))
	}
	pending := max(0, m.total-m.running-m.done-m.failed-m.canceled-m.skipped)
	status := fmt.Sprintf("running %d  done %d  failed %d  pending %d  canceled %d  skipped %d", m.running, m.done, m.failed, pending, m.canceled, m.skipped)
	if m.diff {
		status += " | diff on"
	}
	if m.message != "" {
		status = m.message + " | " + status
	}
	hint := ansi.Truncate(":help", width, "")
	leftWidth := max(0, width-ansi.StringWidth(hint)-1)
	status = ansi.Truncate(status, leftWidth, "")
	parts = append(parts, padCells(status, width-ansi.StringWidth(hint))+hint)
	view := strings.Join(parts, "\n")
	if m.helpOpen {
		return m.helpOverlay(view)
	}
	return view
}

func (m model) editorView(width int) string {
	value := []rune(m.input.Value())
	pos := m.input.Position()
	if len(value) == 0 {
		return ansi.Truncate("> "+tuiDim.Render("[ 'host' ] ( 'command' )"), width, "")
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
		visible += ansi.StringWidth(string(r))
		style := tuiCommand
		if inTargets {
			style = tuiTarget
		}
		if strings.ContainsRune("[]()'", r) {
			style = lipgloss.NewStyle()
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
	m.pickerDraft = m.input.Value()
	m.pickerCursor = m.input.Position()
	if strings.TrimSpace(m.input.Value()) == "" {
		m.input.SetValue("[ '' ] ( '' )")
		m.input.SetCursor(3)
	}
	_, ok := activeTargetStart([]rune(m.input.Value()), m.input.Position())
	if !ok {
		return
	}
	m.resetCompletedTarget()
	m.input.Focus()
	m.picked = map[string]bool{}
	m.pickerOpen = true
	m.filterPicker()
	m.refreshLayout()
}
func (m *model) filterPicker() {
	m.matches = nil
	query := ""
	if start, ok := activeTargetStart([]rune(m.input.Value()), m.input.Position()); ok {
		query = string([]rune(m.input.Value())[start:m.input.Position()])
		if at := strings.LastIndex(query, "@"); at >= 0 {
			query = query[at+1:]
		}
	}
	query = strings.ToLower(query)
	for _, name := range m.candidates {
		if strings.Contains(strings.ToLower(name), query) {
			m.matches = append(m.matches, name)
		}
	}
	m.pickAt = 0
}
func (m model) pickerView() string {
	var rows []string
	start := max(0, m.pickAt-5)
	for i := start; i < min(len(m.matches), start+6); i++ {
		marker := "[ ]"
		if m.picked[m.matches[i]] {
			marker = "[x]"
		}
		cursor := "  "
		if i == m.pickAt {
			cursor = "> "
		}
		rows = append(rows, ansi.Truncate(cursor+marker+" "+displayLabel(m.matches[i]), m.viewport.Width, "..."))
	}
	if len(m.matches) == 0 {
		rows = append(rows, "No matching hosts")
	}
	return strings.Join(rows, "\n")
}
func (m *model) updatePicker(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEsc:
		m.input.SetValue(m.pickerDraft)
		m.input.SetCursor(m.pickerCursor)
		m.pickerOpen = false
		m.matches = nil
	case tea.KeyUp:
		m.pickAt = max(0, m.pickAt-1)
	case tea.KeyDown:
		m.pickAt = min(max(0, len(m.matches)-1), m.pickAt+1)
	case tea.KeyTab:
		m.picked = map[string]bool{}
		m.pickAt = 0
	case tea.KeyShiftTab:
		m.input.SetValue(m.pickerDraft)
		m.input.SetCursor(m.pickerCursor)
		m.pickerOpen = false
		m.matches = nil
		m.focusForm(!m.commandFocused())
	case tea.KeySpace:
		if len(m.matches) > 0 {
			name := m.matches[m.pickAt]
			m.picked[name] = !m.picked[name]
		}
	case tea.KeyEnter:
		var chosen []string
		for _, name := range m.candidates {
			if m.picked[name] {
				chosen = append(chosen, name)
			}
		}
		if len(chosen) == 0 && len(m.matches) > 0 {
			chosen = []string{m.matches[m.pickAt]}
		}
		if len(chosen) == 0 {
			return
		}
		m.insertHosts(chosen)
		m.pickerOpen = false
		m.matches = nil
	default:
		if _, handled := m.deleteEditorField(key); !handled {
			m.input, _ = m.input.Update(key)
		}
		m.filterPicker()
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
// access is explicit (Ctrl-Y or right-click), never triggered by remote terminal sequences.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		if msg.Button == tea.MouseButtonWheelUp {
			m.helpOffset = max(0, m.helpOffset-3)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.helpOffset = min(m.helpMaxOffset(), m.helpOffset+3)
		}
		return m, nil
	}
	if m.trust != nil {
		return m, nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight {
		return m, m.copySelection(true)
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
	row := msg.Y - 1 + m.bodyOffset()
	sticky, index := m.stickyHeader()
	if msg.Y == 0 {
		row = index
	}
	if msg.Y < 0 || msg.Y > m.viewport.Height {
		return m, nil
	}
	rows := m.renderRows()
	if row < 0 || row >= len(rows) {
		return m, nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.selected = false
		m.selecting = false
		m.selectionHeader = msg.Y == 0
		m.selectionBodyStart = m.bodyOffset()
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
		if msg.Y == 0 && len(sticky.spans) > 0 {
			m.selectionHeader = true
			m.selectionBodyStart = m.bodyOffset()
		}
	}
	if m.selected {
		m.message = "lines selected"
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
	if m.selectionHeader {
		// Include the pinned status once, followed only by selected visible output.
		for _, row := range rows {
			if row.heading {
				for _, span := range row.spans {
					if span.block == m.selectionBlock {
						selected = append(selected, span.text)
					}
				}
			}
		}
		start = max(start, m.selectionBodyStart)
	}
	if start > end {
		return strings.Join(selected, "\n")
	}
	for _, row := range rows[start : end+1] {
		for _, span := range row.spans {
			if span.block == m.selectionBlock && !(m.selectionHeader && row.heading) {
				selected = append(selected, span.text)
			}
		}
	}
	return strings.Join(selected, "\n")
}
func (m model) copySelection(clearAfter bool) tea.Cmd {
	text := m.selectedText()
	if text == "" || len(text) > 64<<10 {
		return nil
	}
	return func() tea.Msg {
		_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
		return tuiCopyMsg{err: err, clearAfter: clearAfter, text: text}
	}
}

type tuiCopyMsg struct {
	err        error
	clearAfter bool
	text       string
}
