package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ui"
)

type fanoutEventMsg struct {
	Event repl.FanoutEvent
	OK    bool
	Ch    <-chan repl.FanoutEvent
}

type clipboardCopiedMsg struct {
	bytes int
	err   error
}

type model struct {
	ctx    context.Context
	opts   repl.Options
	input  textinput.Model
	view   viewport.Model
	width  int
	height int

	history           []string
	historyCursor     int
	picker            completionPicker
	completionMessage string
	transcriptBlocks  []transcriptBlock
	transcript        string
	rendered          renderBuffer
	selection         selectionState
	message           string
	command           string
	active            bool
	total             int
	running           int
	done              int
	failed            int
	pending           int
	startedMap        map[int]bool
	pendingResults    map[int]repl.CommandResult
	nextResultIndex   int
	batchTargetCount  int
	cancel            context.CancelFunc

	started   int
	cancelled bool
}

type completionPicker struct {
	open     bool
	matches  []string
	selected int
	checked  map[int]bool
}

type transcriptBlock struct {
	text   string
	result *repl.CommandResult
}

type selectionState struct {
	has     bool
	active  bool
	anchor  selectionPoint
	cursor  selectionPoint
	blockID int
	paneID  string
}

type selectionPoint struct {
	line int
	col  int
}

func newModel(ctx context.Context, opts repl.Options) model {
	opts = normalizeOptions(opts)
	input := textinput.New()
	input.Prompt = "nssh> "
	input.PromptStyle = lipgloss.NewStyle().Foreground(ui.ColorGray)
	input.Placeholder = "[ 'target' ] ( 'command' )"
	input.Focus()
	input.SetValue(promptStarter)
	input.SetCursor(promptStarterCursor)
	view := viewport.New(80, 20)
	history := loadHistory(opts.History)
	return model{
		ctx:           ctx,
		opts:          opts,
		input:         input,
		view:          view,
		history:       history,
		historyCursor: -1,
	}
}

func loadHistory(store repl.HistoryStore) []string {
	if store == nil {
		return nil
	}
	lines, err := store.Load()
	if err != nil {
		return nil
	}
	return lines
}

func normalizeOptions(opts repl.Options) repl.Options {
	if opts.Concurrency <= 0 {
		opts.Concurrency = repl.DefaultConcurrency
	}
	if opts.Resolver == nil {
		opts.Resolver = repl.DefaultTargetResolver{}
	}
	if opts.Runner == nil {
		opts.Runner = repl.SSHCommandRunner{}
	}
	if opts.History == nil {
		opts.History = repl.DefaultHistoryStore()
	}
	if opts.PromptCursor != repl.PromptCursorUnderscore {
		opts.PromptCursor = repl.PromptCursorPipe
	}
	return opts
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize(2)
		m.renderTranscript()
		return m, nil
	case tea.KeyMsg:
		if m.picker.open {
			return m.updatePicker(msg)
		}
		if isLeakedMouseEscapeRunes(msg) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.active {
				if m.cancel != nil {
					m.cancel()
				}
				m.cancelled = true
				m.active = false
				m.running = 0
				m.pending = 0
				m.message = "batch cancelled"
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyCtrlY:
			selected := m.selectedText()
			if selected == "" {
				m.message = "no selection"
				return m, nil
			}
			m.message = fmt.Sprintf("copying selection (%d bytes)", len(selected))
			return m, copyToClipboard(m.copyWriter(), selected)
		case tea.KeyEsc:
			if m.selection.has {
				m.clearSelection()
				return m, nil
			}
		case tea.KeyEnter:
			return m.submit()
		case tea.KeyTab:
			if m.acceptTargetSuggestion() || m.completePromptStructure() {
				return m, nil
			}
			return m, nil
		case tea.KeyBackspace, tea.KeyCtrlH:
			if m.backspacePromptChar() {
				m.completionMessage = ""
				return m, nil
			}
		case tea.KeyDelete, tea.KeyCtrlD:
			if m.deletePromptChar() {
				m.completionMessage = ""
				return m, nil
			}
		case tea.KeyRunes:
			if m.insertPromptRunes(msg.Runes) {
				m.completionMessage = ""
				return m, nil
			}
		case tea.KeyLeft:
			if m.movePromptCursorLeft() {
				return m, nil
			}
		case tea.KeyRight:
			if m.acceptTargetSuggestion() {
				return m, nil
			}
			if m.movePromptCursorRight() {
				return m, nil
			}
		case tea.KeyUp:
			if m.openTargetPicker() {
				return m, nil
			}
			m.recallHistory(-1)
			return m, nil
		case tea.KeyDown:
			m.recallHistory(1)
			return m, nil
		}
	case tea.MouseMsg:
		if next, cmd, handled := m.updateSelection(msg); handled {
			return next, cmd
		}
	case clipboardCopiedMsg:
		if msg.err != nil {
			m.message = fmt.Sprintf("copy failed: %v", msg.err)
			return m, nil
		}
		m.message = fmt.Sprintf("copied selection (%d bytes)", msg.bytes)
		return m, nil
	case fanoutEventMsg:
		if !msg.OK {
			m.active = false
			m.running = 0
			m.pending = 0
			return m, nil
		}
		cmd := m.handleFanoutEvent(msg.Event)
		if m.active {
			return m, tea.Batch(cmd, waitForFanoutEvent(msg.Ch))
		}
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.completionMessage = ""
	var viewCmd tea.Cmd
	m.view, viewCmd = m.view.Update(msg)
	return m, tea.Batch(cmd, viewCmd)
}

func (m model) View() string {
	picker := m.pickerView()
	footerLines := 4
	if picker != "" {
		footerLines += lipgloss.Height(picker)
	}
	m.resize(footerLines)
	status := statusStyle.Render(m.statusLine())
	if m.message != "" {
		status = status + "  " + messageStyle.Render(m.message)
	}
	if m.completionMessage != "" {
		status = status + "  " + messageStyle.Render(m.completionMessage)
	}
	parts := []string{
		m.view.View(),
	}
	if picker != "" {
		parts = append(parts, picker)
	}
	parts = append(parts, m.promptBoxView(), status)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) submit() (tea.Model, tea.Cmd) {
	if m.active {
		m.message = "batch already running"
		return m, nil
	}
	line := strings.TrimSpace(m.input.Value())
	if line == "" || line == promptStarter {
		return m, nil
	}
	if line == ":quit" || line == ":exit" {
		return m, tea.Quit
	}
	if line == ":help" {
		m.appendTranscript("usage:\n  [ 'host' ] ( 'command...' )\n  [ 'host1', 'host2' ] ( 'show ip int brief', 'show version' )\n  [ 'prefix(1,2,3)' ] ( 'command...' )\n  [ 'select:group:local/lab' ] ( 'command...' )\n  :quit | :exit\n")
		m.input.SetValue(promptStarter)
		m.input.SetCursor(promptStarterCursor)
		return m, nil
	}
	req, err := repl.ParseLine(line)
	if err != nil {
		m.message = err.Error()
		return m, nil
	}
	targets, err := repl.ResolveTargets(req, m.opts.Resolver)
	if err != nil {
		m.message = err.Error()
		return m, nil
	}
	batchCtx, cancel := context.WithCancel(m.ctx)
	ch := repl.ExecuteCommandFanoutStream(batchCtx, targets, req.Commands, m.opts.Concurrency, m.opts.Runner)
	m.cancel = cancel
	m.active = true
	m.command = req.Command
	m.total = len(targets) * len(req.Commands)
	m.running = 0
	m.pending = m.total
	m.done = 0
	m.failed = 0
	m.startedMap = make(map[int]bool, m.total)
	m.pendingResults = make(map[int]repl.CommandResult, m.total)
	m.nextResultIndex = 0
	m.batchTargetCount = len(targets)
	m.message = ""
	m.started++
	m.rememberHistory(line)
	m.input.SetValue(promptStarter)
	m.input.SetCursor(promptStarterCursor)
	return m, waitForFanoutEvent(ch)
}

func (m *model) rememberHistory(line string) {
	m.history = append(m.history, line)
	m.historyCursor = -1
	if m.opts.History != nil {
		_ = m.opts.History.Append(line)
	}
}

func (m *model) recallHistory(direction int) {
	if len(m.history) == 0 {
		return
	}
	if m.historyCursor == -1 {
		if direction < 0 {
			m.historyCursor = len(m.history) - 1
		} else {
			m.historyCursor = 0
		}
	} else {
		m.historyCursor += direction
		if m.historyCursor < 0 {
			m.historyCursor = len(m.history) - 1
		}
		if m.historyCursor >= len(m.history) {
			m.historyCursor = 0
		}
	}
	m.input.SetValue(m.history[m.historyCursor])
	m.input.CursorEnd()
}

func (m *model) openTargetPicker() bool {
	suggester, ok := m.opts.Resolver.(repl.HostSuggester)
	if !ok {
		return false
	}
	prefix, _, _, ok := targetInputPrefix(m.input.Value())
	if !ok {
		return false
	}
	if prefix == "" {
		return false
	}
	matches, err := suggester.SuggestHosts(prefix)
	if err != nil {
		m.message = err.Error()
		return true
	}
	m.completionMessage = ""
	m.picker = completionPicker{}
	matches = cleanHostMatches(matches)
	if len(matches) == 0 {
		m.completionMessage = "no host matches"
		return false
	}
	m.openPicker(matches)
	return true
}

func (m *model) acceptTargetSuggestion() bool {
	value := m.input.Value()
	prefix, start, end, ok := targetInputPrefix(value)
	if !ok {
		return false
	}
	if prefix == "" {
		return false
	}
	suggestion := m.targetSuggestion(value)
	if suggestion == "" {
		return false
	}
	m.replaceTargetSpan(start, end, prefix+suggestion)
	m.completionMessage = ""
	return true
}

func (m *model) completePromptStructure() bool {
	value := m.input.Value()
	prefix, start, end, ok := targetInputPrefix(value)
	if !ok {
		return false
	}
	expanded, err := repl.ExpandHostPattern(prefix)
	if err != nil {
		return false
	}
	parts := make([]string, 0, len(expanded))
	for _, host := range expanded {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		parts = append(parts, quotePromptItem(host))
	}
	if len(parts) == 0 {
		return false
	}
	next := value[:start]
	if len(parts) == 1 {
		next += strings.Trim(parts[0], "'")
	} else {
		next = value[:targetGroupBodyStart(value)] + " " + strings.Join(parts, ", ") + " " + value[targetGroupBodyEnd(value):]
		m.input.SetValue(next)
		m.input.SetCursor(commandPromptCursor(next))
		m.picker = completionPicker{}
		m.completionMessage = ""
		return true
	}
	next += value[end:]
	if next == value {
		return false
	}
	m.input.SetValue(next)
	m.input.SetCursor(commandPromptCursor(next))
	m.picker = completionPicker{}
	m.completionMessage = ""
	return true
}

func cleanHostMatches(matches []string) []string {
	seen := make(map[string]bool, len(matches))
	cleaned := make([]string, 0, len(matches))
	for _, match := range matches {
		match = strings.TrimSpace(match)
		if match == "" || seen[match] {
			continue
		}
		seen[match] = true
		cleaned = append(cleaned, match)
	}
	return cleaned
}

func (m *model) openPicker(matches []string) {
	m.picker = completionPicker{open: true, matches: matches}
	m.completionMessage = fmt.Sprintf("%d host matches", len(matches))
}

func (m model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.picker = completionPicker{}
		m.completionMessage = ""
		return m, nil
	case tea.KeyCtrlC:
		m.picker = completionPicker{}
		m.completionMessage = ""
		return m, nil
	case tea.KeyUp:
		m.picker.selected--
		if m.picker.selected < 0 {
			m.picker.selected = len(m.picker.matches) - 1
		}
		return m, nil
	case tea.KeyDown:
		m.picker.selected++
		if m.picker.selected >= len(m.picker.matches) {
			m.picker.selected = 0
		}
		return m, nil
	case tea.KeySpace:
		m.togglePickerSelection()
		return m, nil
	case tea.KeyEnter, tea.KeyRight:
		m.acceptPickerSelection()
		return m, nil
	}
	return m, nil
}

func (m *model) togglePickerSelection() {
	if m.picker.selected < 0 || m.picker.selected >= len(m.picker.matches) {
		return
	}
	if m.picker.checked == nil {
		m.picker.checked = make(map[int]bool)
	}
	if m.picker.checked[m.picker.selected] {
		delete(m.picker.checked, m.picker.selected)
		return
	}
	m.picker.checked[m.picker.selected] = true
}

func (m *model) acceptPickerSelection() {
	if len(m.picker.matches) == 0 {
		m.picker = completionPicker{}
		m.completionMessage = ""
		return
	}
	hosts := m.pickerSelectedHosts()
	if len(hosts) == 0 && m.picker.selected >= 0 && m.picker.selected < len(m.picker.matches) {
		hosts = []string{m.picker.matches[m.picker.selected]}
	}
	if len(hosts) == 0 {
		m.picker = completionPicker{}
		m.completionMessage = ""
		return
	}
	value := m.input.Value()
	next := promptWithTargets(value, hosts)
	m.input.SetValue(next)
	m.input.SetCursor(commandPromptCursor(next))
	m.picker = completionPicker{}
	m.completionMessage = ""
}

func (m model) pickerSelectedHosts() []string {
	if len(m.picker.checked) == 0 {
		return nil
	}
	hosts := make([]string, 0, len(m.picker.checked))
	for i, host := range m.picker.matches {
		if m.picker.checked[i] {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func (m model) pickerView() string {
	if !m.picker.open || len(m.picker.matches) == 0 {
		return ""
	}
	limit := min(8, len(m.picker.matches))
	lines := make([]string, 0, limit)
	for i, host := range m.picker.matches[:limit] {
		cursor := "  "
		style := pickerItemStyle
		if i == m.picker.selected {
			cursor = "> "
			style = pickerSelectedStyle
		}
		checked := "[ ] "
		if m.picker.checked[i] {
			checked = "[x] "
		}
		lines = append(lines, style.Render(cursor+checked+targetStyle.Render(host)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) handleFanoutEvent(event repl.FanoutEvent) tea.Cmd {
	if event.Kind == repl.FanoutStarted {
		eventIndex := m.eventOrderIndex(event)
		if !m.startedMap[eventIndex] {
			m.startedMap[eventIndex] = true
			m.running++
			if m.pending > 0 {
				m.pending--
			}
		}
		return nil
	}
	if event.Kind != repl.FanoutCompleted {
		return nil
	}
	eventIndex := m.eventOrderIndex(event)
	if m.startedMap[eventIndex] && m.running > 0 {
		m.running--
	} else if m.pending > 0 {
		m.pending--
	}
	if event.Result.Err != nil {
		m.failed++
	} else {
		m.done++
	}
	if m.pendingResults == nil {
		m.pendingResults = make(map[int]repl.CommandResult)
	}
	m.pendingResults[eventIndex] = event.Result
	m.flushCompletedResults()
	if m.done+m.failed >= m.total {
		m.active = false
		m.cancel = nil
		m.running = 0
		m.pending = 0
	}
	return nil
}

func (m model) eventOrderIndex(event repl.FanoutEvent) int {
	targetCount := max(1, m.batchTargetCount)
	batch := event.Batch
	if batch <= 0 {
		batch = 1
	}
	return (batch-1)*targetCount + event.Index
}

func (m *model) flushCompletedResults() {
	for {
		result, ok := m.pendingResults[m.nextResultIndex]
		if !ok {
			return
		}
		delete(m.pendingResults, m.nextResultIndex)
		m.appendResult(result)
		m.nextResultIndex++
	}
}

func (m *model) appendResult(result repl.CommandResult) {
	resultCopy := result
	m.transcriptBlocks = append(m.transcriptBlocks, transcriptBlock{result: &resultCopy})
	m.renderTranscript()
}

func (m *model) appendTranscript(s string) {
	m.transcriptBlocks = append(m.transcriptBlocks, transcriptBlock{text: s})
	m.renderTranscript()
}

func (m *model) renderTranscript() {
	m.rendered = renderTranscriptBufferWithDiff(m.transcriptBlocks, m.width, m.opts.Diff)
	m.transcript = m.rendered.text
	m.refreshViewportContent()
	m.view.GotoBottom()
}

func (m *model) refreshViewportContent() {
	content := m.rendered.text
	if m.selection.has {
		content = renderBufferWithSelection(m.rendered, m.selection)
	}
	m.view.SetContent(content)
}

func (m model) updateSelection(msg tea.MouseMsg) (model, tea.Cmd, bool) {
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown ||
		msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelRight {
		return m, nil, false
	}
	if msg.Y < 0 || msg.Y >= m.view.Height {
		return m, nil, false
	}
	point := selectionPoint{line: m.view.YOffset + msg.Y, col: max(0, msg.X)}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil, false
		}
		span, point, ok := m.rendered.spanForPoint(point.line, point.col)
		if !ok {
			return m, nil, true
		}
		m.selection = selectionState{
			has:     true,
			active:  true,
			anchor:  point,
			cursor:  point,
			blockID: span.blockID,
			paneID:  span.paneID,
		}
		m.refreshViewportContent()
		return m, nil, true
	case tea.MouseActionMotion:
		if !m.selection.active {
			return m, nil, false
		}
		m.selection.cursor = point
		m.refreshViewportContent()
		return m, nil, true
	case tea.MouseActionRelease:
		if !m.selection.active {
			return m, nil, false
		}
		m.selection.cursor = point
		m.selection.active = false
		if m.selection.anchor == m.selection.cursor {
			m.selection = selectionState{}
			m.refreshViewportContent()
			return m, nil, true
		}
		selected := m.selectedText()
		m.refreshViewportContent()
		if selected != "" {
			m.message = fmt.Sprintf("copying selection (%d bytes)", len(selected))
			return m, copyToClipboard(m.copyWriter(), selected), true
		}
		m.selection = selectionState{}
		m.refreshViewportContent()
		return m, nil, true
	}
	return m, nil, false
}

func (m *model) clearSelection() {
	m.selection = selectionState{}
	m.refreshViewportContent()
	m.message = ""
}

func (m model) selectedText() string {
	if !m.selection.has {
		return ""
	}
	return selectedTextFromSpans(m.rendered, m.selection)
}

func (m model) copyWriter() io.Writer {
	if m.opts.Out != nil {
		return m.opts.Out
	}
	return os.Stdout
}

func normalizedSelection(selection selectionState) (selectionPoint, selectionPoint) {
	start := selection.anchor
	end := selection.cursor
	if start.line > end.line || (start.line == end.line && start.col > end.col) {
		start, end = end, start
	}
	if start.col < 0 {
		start.col = 0
	}
	if end.col < 0 {
		end.col = 0
	}
	return start, end
}

func renderSelectedLine(line string, startCol, endCol int) string {
	if startCol >= endCol {
		return line
	}
	var out strings.Builder
	var selected strings.Builder
	col := 0
	for i := 0; i < len(line); {
		if loc := ansiPattern.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			seq := line[i : i+loc[1]]
			if col >= startCol && col < endCol {
				selected.WriteString(seq)
			} else {
				flushSelected(&out, &selected)
				out.WriteString(seq)
			}
			i += loc[1]
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if col >= startCol && col < endCol {
			selected.WriteRune(r)
		} else {
			flushSelected(&out, &selected)
			out.WriteRune(r)
		}
		col += max(1, lipgloss.Width(string(r)))
		i += size
	}
	flushSelected(&out, &selected)
	return out.String()
}

func flushSelected(out, selected *strings.Builder) {
	if selected.Len() == 0 {
		return
	}
	out.WriteString(selectionStyle.Render(stripANSICodes(selected.String())))
	selected.Reset()
}

func runeSlice(s string, start, end int) string {
	runes := []rune(s)
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(runes) {
		start = len(runes)
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}

func clampInt(v, minValue, maxValue int) int {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func stripANSICodes(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func isLeakedMouseEscapeRunes(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes || msg.Paste {
		return false
	}
	return leakedMouseEscapePattern.MatchString(string(msg.Runes))
}

func copyToClipboard(out io.Writer, text string) tea.Cmd {
	return func() tea.Msg {
		_, err := fmt.Fprint(out, osc52.New(text))
		return clipboardCopiedMsg{bytes: len(text), err: err}
	}
}

func renderTranscriptBlocks(blocks []transcriptBlock, width int) string {
	if width <= 0 {
		width = 80
	}
	var rendered []string
	for i := 0; i < len(blocks); i++ {
		block := blocks[i]
		if block.result == nil {
			rendered = append(rendered, strings.TrimRight(block.text, "\n"))
			continue
		}
		if i+1 < len(blocks) && blocks[i+1].result != nil {
			left := *block.result
			right := *blocks[i+1].result
			if canRenderResultSplit(left, right, width) {
				rendered = append(rendered, renderResultSplit(left, right, width))
				i++
				continue
			}
		}
		rendered = append(rendered, renderResultBlock(*block.result, width))
	}
	return strings.Join(nonEmptyBlocks(rendered), "\n\n")
}

func nonEmptyBlocks(blocks []string) []string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		out = append(out, block)
	}
	return out
}

func canRenderResultSplit(left, right repl.CommandResult, width int) bool {
	columnWidth := (width - splitGapWidth) / 2
	if columnWidth < minSplitColumnWidth {
		return false
	}
	return true
}

func renderResultSplit(left, right repl.CommandResult, width int) string {
	leftWidth := (width - splitGapWidth) / 2
	rightWidth := width - splitGapWidth - leftWidth
	leftLines := renderResultSplitColumn(left, leftWidth)
	rightLines := renderResultSplitColumn(right, rightWidth)
	height := max(len(leftLines), len(rightLines))
	for len(leftLines) < height {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < height {
		rightLines = append(rightLines, "")
	}
	gap := strings.Repeat(" ", splitGapWidth)
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		lines[i] = padRight(leftLines[i], leftWidth) + gap + rightLines[i]
	}
	return strings.Join(lines, "\n")
}

func renderResultSplitColumn(result repl.CommandResult, width int) []string {
	lines := []string{repl.RenderOutputBannerForWidth(result.Host, result.Command, width)}
	body := resultBodyLines(result)
	gutterWidth := len(strconv.Itoa(max(1, len(body))))
	bodyWidth := width - gutterWidth - 1
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	for i, line := range body {
		gutter := fmt.Sprintf("%*d ", gutterWidth, i+1)
		lines = append(lines, lineNumberStyle.Render(gutter)+clipLine(line, bodyWidth))
	}
	return lines
}

func renderResultBlock(result repl.CommandResult, width int) string {
	var b strings.Builder
	b.WriteString(repl.RenderOutputBannerForWidth(result.Host, result.Command, width))
	b.WriteString("\n")
	for _, line := range resultBodyLines(result) {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func resultBodyLines(result repl.CommandResult) []string {
	var lines []string
	output := strings.TrimRight(normalizeTerminalOutput(string(result.Output)), "\n")
	if output != "" {
		lines = append(lines, strings.Split(output, "\n")...)
	}
	if result.Err != nil {
		lines = append(lines, fmt.Sprintf("error: %v", result.Err))
	}
	return lines
}

func normalizeTerminalOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func resultNaturalWidth(result repl.CommandResult) int {
	width := lipgloss.Width(result.Host+" | "+result.Command) + 2
	body := resultBodyLines(result)
	gutterWidth := len(strconv.Itoa(max(1, len(body)))) + 1
	for _, line := range body {
		width = max(width, lipgloss.Width(line)+gutterWidth)
	}
	return max(width, minSplitColumnWidth)
}

func resultBlockNaturalWidth(result repl.CommandResult) int {
	width := lipgloss.Width(result.Host+" | "+result.Command) + 2
	for _, line := range resultBodyLines(result) {
		width = max(width, lipgloss.Width(line))
	}
	return max(width, minSplitColumnWidth)
}

func resultNaturalHeight(result repl.CommandResult) int {
	return len(strings.Split(renderResultBlock(result, resultBlockNaturalWidth(result)), "\n"))
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func padRight(s string, width int) string {
	padding := width - lipgloss.Width(s)
	if padding <= 0 {
		return s
	}
	return s + strings.Repeat(" ", padding)
}

func clipLine(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	current := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if current+w > width {
			break
		}
		b.WriteRune(r)
		current += w
	}
	return b.String()
}

func (m model) promptBoxView() string {
	width := max(1, m.width-4)
	return promptBoxStyle.Width(width).Render(m.promptInputView())
}

func (m model) promptInputView() string {
	value := m.input.Value()
	if value == "" {
		return m.input.View()
	}
	suggestion := m.targetSuggestion(value)
	pos := m.input.Position()
	if preview, ok := m.pickerPromptPreview(value); ok {
		value = preview
		pos = commandPromptCursor(value)
		suggestion = ""
	}
	return m.input.PromptStyle.Render(m.input.Prompt) + renderEditableValue(value, pos, m.input.Cursor, suggestion, promptCursorGlyph(m.opts.PromptCursor))
}

func (m model) pickerPromptPreview(value string) (string, bool) {
	if !m.picker.open {
		return "", false
	}
	hosts := m.pickerSelectedHosts()
	if len(hosts) == 0 {
		return "", false
	}
	return promptWithTargets(value, hosts), true
}

func (m *model) insertPromptRunes(runes []rune) bool {
	value := m.input.Value()
	if !isStructuredPrompt(value) {
		return false
	}
	pos := m.input.Position()
	for _, r := range runes {
		value = m.input.Value()
		pos = m.input.Position()
		if r == ',' {
			if next, cursor, ok := insertTargetSeparator(value, pos); ok {
				m.input.SetValue(next)
				m.input.SetCursor(cursor)
				continue
			}
		}
		if !promptCanInsertAt(value, pos) {
			continue
		}
		next := insertRuneAt(value, pos, r)
		m.input.SetValue(next)
		m.input.SetCursor(pos + 1)
	}
	return true
}

func (m *model) backspacePromptChar() bool {
	value := m.input.Value()
	if !isStructuredPrompt(value) {
		return false
	}
	pos := m.input.Position()
	if pos <= 0 || !promptEditableRune(value, pos-1) {
		return true
	}
	next := removeRuneAt(value, pos-1)
	m.input.SetValue(next)
	m.input.SetCursor(pos - 1)
	return true
}

func (m *model) deletePromptChar() bool {
	value := m.input.Value()
	if !isStructuredPrompt(value) {
		return false
	}
	pos := m.input.Position()
	if !promptEditableRune(value, pos) {
		return true
	}
	next := removeRuneAt(value, pos)
	m.input.SetValue(next)
	m.input.SetCursor(pos)
	return true
}

func (m *model) movePromptCursorLeft() bool {
	value := m.input.Value()
	if !isStructuredPrompt(value) {
		return false
	}
	spans := promptEditableSpans(value)
	if len(spans) == 0 {
		return true
	}
	pos := min(m.input.Position(), len([]rune(value)))
	for i := len(spans) - 1; i >= 0; i-- {
		span := spans[i]
		switch {
		case pos > span.end:
			m.input.SetCursor(span.end)
			return true
		case pos > span.start && pos <= span.end:
			m.input.SetCursor(pos - 1)
			return true
		case pos == span.start:
			if i > 0 {
				m.input.SetCursor(spans[i-1].end)
			} else {
				m.input.SetCursor(span.start)
			}
			return true
		}
	}
	m.input.SetCursor(spans[0].start)
	return true
}

func (m *model) movePromptCursorRight() bool {
	value := m.input.Value()
	if !isStructuredPrompt(value) {
		return false
	}
	spans := promptEditableSpans(value)
	if len(spans) == 0 {
		return true
	}
	pos := min(m.input.Position(), len([]rune(value)))
	for i, span := range spans {
		switch {
		case pos < span.start:
			m.input.SetCursor(span.start)
			return true
		case pos < span.end:
			m.input.SetCursor(pos + 1)
			return true
		case pos == span.end:
			if i+1 < len(spans) {
				m.input.SetCursor(spans[i+1].start)
			} else {
				m.input.SetCursor(span.end)
			}
			return true
		}
	}
	m.input.SetCursor(spans[len(spans)-1].end)
	return true
}

func (m model) targetSuggestion(value string) string {
	suggester, ok := m.opts.Resolver.(repl.HostSuggester)
	if !ok {
		return ""
	}
	prefix, _, end, ok := targetInputPrefix(value)
	if !ok {
		return ""
	}
	if m.input.Position() != len([]rune(value[:end])) {
		return ""
	}
	if prefix == "" {
		return ""
	}
	if m.picker.open && m.picker.selected >= 0 && m.picker.selected < len(m.picker.matches) {
		host := strings.TrimSpace(m.picker.matches[m.picker.selected])
		if len(host) > len(prefix) && strings.HasPrefix(strings.ToLower(host), strings.ToLower(prefix)) {
			return host[len(prefix):]
		}
	}
	hosts, err := suggester.SuggestHosts(prefix)
	if err != nil || len(hosts) == 0 {
		return ""
	}
	host := strings.TrimSpace(hosts[0])
	if len(host) <= len(prefix) || !strings.HasPrefix(strings.ToLower(host), strings.ToLower(prefix)) {
		return ""
	}
	return host[len(prefix):]
}

func targetInputPrefix(value string) (string, int, int, bool) {
	start, end, ok := targetValueSpan(value)
	if !ok {
		return "", 0, 0, false
	}
	return value[start:end], start, end, true
}

func targetValueSpan(value string) (int, int, bool) {
	bodyStart := targetGroupBodyStart(value)
	bodyEnd := targetGroupBodyEnd(value)
	if bodyStart < 0 || bodyEnd < bodyStart {
		return 0, 0, false
	}
	if strings.TrimSpace(value[bodyEnd+1:]) != "( '' )" {
		return 0, 0, false
	}
	body := value[bodyStart:bodyEnd]
	trimmed := strings.TrimSpace(body)
	offset := strings.Index(body, trimmed)
	if offset < 0 || !strings.HasPrefix(trimmed, "'") || !strings.HasSuffix(trimmed, "'") || strings.Count(trimmed, "'") != 2 {
		return 0, 0, false
	}
	start := bodyStart + offset + 1
	end := bodyStart + offset + len(trimmed) - 1
	return start, end, true
}

func targetGroupBodyStart(value string) int {
	open := strings.Index(value, "[")
	if open < 0 {
		return -1
	}
	return open + 1
}

func targetGroupBodyEnd(value string) int {
	open := strings.Index(value, "[")
	if open < 0 {
		return -1
	}
	inQuote := false
	escaped := false
	for i, r := range value[open+1:] {
		idx := open + 1 + i
		if escaped {
			escaped = false
			continue
		}
		if inQuote && r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && r == ']' {
			return idx
		}
	}
	return -1
}

func (m *model) replaceTargetSpan(start, end int, target string) {
	value := m.input.Value()
	next := value[:start] + escapePromptItem(target) + value[end:]
	m.input.SetValue(next)
	m.input.SetCursor(commandPromptCursor(next))
	m.picker = completionPicker{}
}

func promptWithTargets(value string, targets []string) string {
	quoted := make([]string, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		quoted = append(quoted, quotePromptItem(target))
	}
	if len(quoted) == 0 {
		return value
	}
	body := " " + strings.Join(quoted, ", ") + " "
	bodyStart := targetGroupBodyStart(value)
	bodyEnd := targetGroupBodyEnd(value)
	if bodyStart >= 0 && bodyEnd >= bodyStart {
		return value[:bodyStart] + body + value[bodyEnd:]
	}
	return "[" + body + "] ( '' )"
}

func commandPromptCursor(value string) int {
	if idx := strings.Index(value, "( '' )"); idx >= 0 {
		return len([]rune(value[:idx])) + 3
	}
	return len([]rune(value))
}

func quotePromptItem(value string) string {
	return "'" + escapePromptItem(value) + "'"
}

func escapePromptItem(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "'", "\\'")
}

func isStructuredPrompt(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "[")
}

func promptCanInsertAt(value string, pos int) bool {
	if pos < 0 || pos > len([]rune(value)) {
		return false
	}
	for _, span := range promptEditableSpans(value) {
		if pos >= span.start && pos <= span.end {
			return true
		}
	}
	return false
}

func promptEditableRune(value string, pos int) bool {
	runes := []rune(value)
	if pos < 0 || pos >= len(runes) || runes[pos] == '\'' {
		return false
	}
	return promptCanInsertAt(value, pos)
}

func insertTargetSeparator(value string, pos int) (string, int, bool) {
	runes := []rune(value)
	if pos < 0 || pos > len(runes) {
		return "", 0, false
	}
	bodyStart, bodyEnd, ok := targetGroupBodyRuneRange(value)
	if !ok || pos <= bodyStart || pos >= bodyEnd {
		return "", 0, false
	}
	quoteStart := -1
	quoteEnd := -1
	escaped := false
	for i := bodyStart; i < bodyEnd; i++ {
		r := runes[i]
		if escaped {
			escaped = false
			continue
		}
		if quoteStart >= 0 && r == '\\' {
			escaped = true
			continue
		}
		if r != '\'' {
			continue
		}
		if quoteStart < 0 {
			quoteStart = i
			continue
		}
		quoteEnd = i
		if pos > quoteStart && pos <= quoteEnd+1 {
			break
		}
		quoteStart = -1
		quoteEnd = -1
	}
	if quoteStart < 0 || quoteEnd < 0 || pos > quoteEnd+1 {
		return "", 0, false
	}
	contentEnd := min(pos, quoteEnd)
	if strings.TrimSpace(string(runes[quoteStart+1:contentEnd])) == "" {
		return "", 0, false
	}
	if pos <= quoteEnd && strings.TrimSpace(string(runes[pos:quoteEnd])) != "" {
		return "", 0, false
	}
	insert := []rune(", ''")
	next := append([]rune{}, runes[:quoteEnd+1]...)
	next = append(next, insert...)
	next = append(next, runes[quoteEnd+1:]...)
	return string(next), quoteEnd + len(insert), true
}

func targetGroupBodyRuneRange(value string) (int, int, bool) {
	bodyStart := targetGroupBodyStart(value)
	bodyEnd := targetGroupBodyEnd(value)
	if bodyStart < 0 || bodyEnd < bodyStart {
		return 0, 0, false
	}
	return len([]rune(value[:bodyStart])), len([]rune(value[:bodyEnd])), true
}

func commandGroupBodyRuneRange(value string) (int, int, bool) {
	bodyStart, bodyEnd, ok := commandGroupBodyRange(value)
	if !ok {
		return 0, 0, false
	}
	return len([]rune(value[:bodyStart])), len([]rune(value[:bodyEnd])), true
}

type promptEditableSpan struct {
	start int
	end   int
}

func promptEditableSpans(value string) []promptEditableSpan {
	runes := []rune(value)
	var spans []promptEditableSpan
	if start, end, ok := targetGroupBodyRuneRange(value); ok {
		spans = append(spans, quotedEditableSpans(runes, start, end)...)
	}
	if targetGroupHasContent(value) {
		if start, end, ok := commandGroupBodyRuneRange(value); ok {
			spans = append(spans, quotedEditableSpans(runes, start, end)...)
		}
	}
	return spans
}

func targetGroupHasContent(value string) bool {
	runes := []rune(value)
	start, end, ok := targetGroupBodyRuneRange(value)
	if !ok {
		return false
	}
	for _, span := range quotedEditableSpans(runes, start, end) {
		if strings.TrimSpace(string(runes[span.start:span.end])) != "" {
			return true
		}
	}
	return false
}

func quotedEditableSpans(runes []rune, start, end int) []promptEditableSpan {
	var spans []promptEditableSpan
	quoteStart := -1
	escaped := false
	for i := start; i < end; i++ {
		r := runes[i]
		if escaped {
			escaped = false
			continue
		}
		if quoteStart >= 0 && r == '\\' {
			escaped = true
			continue
		}
		if r != '\'' {
			continue
		}
		if quoteStart < 0 {
			quoteStart = i
			continue
		}
		spans = append(spans, promptEditableSpan{start: quoteStart + 1, end: i})
		quoteStart = -1
	}
	return spans
}

func insertRuneAt(value string, pos int, r rune) string {
	runes := []rune(value)
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	runes = append(runes[:pos], append([]rune{r}, runes[pos:]...)...)
	return string(runes)
}

func removeRuneAt(value string, pos int) string {
	runes := []rune(value)
	if pos < 0 || pos >= len(runes) {
		return value
	}
	runes = append(runes[:pos], runes[pos+1:]...)
	return string(runes)
}

func renderEditableValue(value string, pos int, cursor cursor.Model, suggestion string, cursorGlyph string) string {
	if pos < 0 {
		pos = 0
	}
	if value == promptStarter && strings.TrimSpace(suggestion) == "" {
		return renderStarterPromptExample(pos, cursorGlyph)
	}
	runes := []rune(value)
	if pos > len(runes) {
		pos = len(runes)
	}
	suggestionPos := -1
	if suggestion != "" {
		if _, _, end, ok := targetInputPrefix(value); ok {
			endPos := len([]rune(value[:end]))
			if pos == endPos {
				suggestionPos = endPos
			}
		}
	}
	var b strings.Builder
	mode := rawPromptMode{}
	groups := promptGroupStateFor(value, suggestion)
	cursorWritten := false
	for i, r := range runes {
		if !cursorWritten && i == pos {
			writePromptCursor(&b, cursorGlyph)
			cursorWritten = true
		}
		style := mode.style(runes, i, groups)
		b.WriteString(style.Render(string(r)))
		if i+1 == suggestionPos {
			if !cursorWritten {
				writePromptCursor(&b, cursorGlyph)
				cursorWritten = true
			}
			writeSuggestion(&b, suggestion, 0)
		}
		mode.advance(r)
	}
	if !cursorWritten && pos == len(runes) {
		writePromptCursor(&b, cursorGlyph)
	}
	return b.String()
}

func renderStarterPromptExample(pos int, cursorGlyph string) string {
	var b strings.Builder
	b.WriteString(promptGroupStyle.Render("["))
	b.WriteString(promptGroupStyle.Render("'"))
	if pos == promptStarterCursor {
		writePromptCursor(&b, cursorGlyph)
	}
	b.WriteString(promptPlaceholderTargetStyle.Render("TARGET"))
	b.WriteString(promptGroupStyle.Render("'"))
	b.WriteString(promptGroupStyle.Render("] "))
	b.WriteString(promptGroupStyle.Render("("))
	b.WriteString(promptGroupStyle.Render("'"))
	if pos == commandPromptCursor(promptStarter) {
		writePromptCursor(&b, cursorGlyph)
	}
	b.WriteString(promptPlaceholderCommandStyle.Render("COMMAND"))
	b.WriteString(promptGroupStyle.Render("'"))
	b.WriteString(promptGroupStyle.Render(")"))
	return b.String()
}

type rawPromptRegion int

const (
	rawPromptGroup rawPromptRegion = iota
	rawPromptTarget
	rawPromptCommand
)

type promptGroupState struct {
	targetFilled  bool
	commandFilled bool
}

type rawPromptMode struct {
	region  rawPromptRegion
	inQuote bool
	escaped bool
}

func (m rawPromptMode) style(runes []rune, idx int, groups promptGroupState) lipgloss.Style {
	switch {
	case m.region == rawPromptTarget && m.inQuote && runes[idx] != '\'':
		return targetStyle
	case m.region == rawPromptCommand && m.inQuote && runes[idx] != '\'':
		return commandRuneStyle(runes, idx)
	case runes[idx] == '[' || runes[idx] == ']' || m.region == rawPromptTarget:
		return promptGroupStyleFor(groups.targetFilled)
	case runes[idx] == '(' || runes[idx] == ')' || m.region == rawPromptCommand:
		return promptGroupStyleFor(groups.commandFilled)
	default:
		return promptGroupStyle
	}
}

func promptGroupStyleFor(filled bool) lipgloss.Style {
	if filled {
		return promptGroupSolidStyle
	}
	return promptGroupStyle
}

func promptGroupStateFor(value, suggestion string) promptGroupState {
	targetStart := targetGroupBodyStart(value)
	targetEnd := targetGroupBodyEnd(value)
	state := promptGroupState{}
	if targetStart >= 0 && targetEnd >= targetStart && strings.TrimSpace(suggestion) == "" {
		state.targetFilled = quotedGroupHasContent(value[targetStart:targetEnd])
	}
	commandStart, commandEnd, ok := commandGroupBodyRange(value)
	if ok {
		state.commandFilled = quotedGroupHasContent(value[commandStart:commandEnd])
	}
	return state
}

func commandGroupBodyRange(value string) (int, int, bool) {
	open := strings.Index(value, "(")
	if open < 0 {
		return 0, 0, false
	}
	inQuote := false
	escaped := false
	for i, r := range value[open+1:] {
		idx := open + 1 + i
		if escaped {
			escaped = false
			continue
		}
		if inQuote && r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && r == ')' {
			return open + 1, idx, true
		}
	}
	return 0, 0, false
}

func quotedGroupHasContent(body string) bool {
	inQuote := false
	escaped := false
	var current strings.Builder
	for _, r := range body {
		if escaped {
			if inQuote {
				current.WriteRune(r)
			}
			escaped = false
			continue
		}
		if inQuote && r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' {
			if inQuote && strings.TrimSpace(current.String()) != "" {
				return true
			}
			if inQuote {
				current.Reset()
			}
			inQuote = !inQuote
			continue
		}
		if inQuote {
			current.WriteRune(r)
		}
	}
	return false
}

func (m *rawPromptMode) advance(r rune) {
	if m.escaped {
		m.escaped = false
		return
	}
	if m.inQuote && r == '\\' {
		m.escaped = true
		return
	}
	if r == '\'' {
		m.inQuote = !m.inQuote
		return
	}
	if m.inQuote {
		return
	}
	switch r {
	case '[':
		m.region = rawPromptTarget
	case ']':
		m.region = rawPromptGroup
	case '(':
		m.region = rawPromptCommand
	case ')':
		m.region = rawPromptGroup
	}
}

func commandRuneStyle(runes []rune, idx int) lipgloss.Style {
	return commandStyleForFlag(commandRuneIsFlag(runes, idx))
}

func commandStyleForFlag(flag bool) lipgloss.Style {
	if flag {
		return flagStyle
	}
	return commandEchoStyle
}

func commandRuneIsFlag(runes []rune, idx int) bool {
	if idx >= len(runes) || unicode.IsSpace(runes[idx]) {
		return false
	}
	tokenStart := idx
	for tokenStart > 0 && !unicode.IsSpace(runes[tokenStart-1]) && runes[tokenStart-1] != '\'' && runes[tokenStart-1] != ',' {
		tokenStart--
	}
	return idx < len(runes) && runes[tokenStart] == '-'
}

func writePromptCursor(b *strings.Builder, cursorGlyph string) {
	b.WriteString(promptCursorStyle.Render(cursorGlyph))
}

func promptCursorGlyph(cursor repl.PromptCursor) string {
	if cursor == repl.PromptCursorUnderscore {
		return "_"
	}
	return "|"
}

func writeSuggestion(b *strings.Builder, suggestion string, start int) {
	suggestion = strings.TrimRightFunc(suggestion, unicode.IsSpace)
	runes := []rune(suggestion)
	if start < len(runes) {
		b.WriteString(targetSuggestionStyle.Render(string(runes[start:])))
	}
}

func (m model) statusLine() string {
	return fmt.Sprintf("running %d/%d  done %d/%d  failed %d/%d  pending %d/%d", m.running, m.total, m.done, m.total, m.failed, m.total, m.pending, m.total)
}

func (m *model) resize(footerLines int) {
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 24
	}
	m.input.Width = max(1, m.width-8)
	m.view.Width = m.width
	viewHeight := m.height - footerLines
	if viewHeight < 1 {
		viewHeight = 1
	}
	m.view.Height = viewHeight
}

func waitForFanoutEvent(ch <-chan repl.FanoutEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		return fanoutEventMsg{Event: event, OK: ok, Ch: ch}
	}
}

var (
	statusStyle                   = lipgloss.NewStyle().Foreground(ui.ColorYellow)
	messageStyle                  = lipgloss.NewStyle().Foreground(ui.ColorGray)
	promptGroupStyle              = lipgloss.NewStyle().Foreground(ui.ColorGray).Faint(true)
	promptGroupSolidStyle         = lipgloss.NewStyle().Foreground(ui.ColorGray)
	promptCursorStyle             = lipgloss.NewStyle().Foreground(ui.ColorWhite)
	targetStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	targetSuggestionStyle         = lipgloss.NewStyle().Foreground(ui.ColorGray).Faint(true)
	commandEchoStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	flagStyle                     = lipgloss.NewStyle().Foreground(ui.ColorYellow)
	promptPlaceholderTargetStyle  = targetStyle.Faint(true)
	promptPlaceholderCommandStyle = commandEchoStyle.Faint(true)
	lineNumberStyle               = lipgloss.NewStyle().Foreground(ui.ColorDim)
	diffLeftStyle                 = lipgloss.NewStyle().Background(diffLeftBackgroundColor)
	diffRightStyle                = lipgloss.NewStyle().Background(diffRightBackgroundColor)
	selectionStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("153"))
	pickerItemStyle               = lipgloss.NewStyle().Foreground(ui.ColorGray)
	pickerSelectedStyle           = lipgloss.NewStyle().Foreground(ui.ColorWhite)
	promptBoxStyle                = lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(ui.ColorDim).
					Padding(0, 1)
)

var (
	ansiPattern              = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	leakedMouseEscapePattern = regexp.MustCompile(`^(?:\x1b?\[<\d+;\d+(?:;\d+)?[mM])+$`)
)

const (
	diffLeftBackgroundColor  = lipgloss.Color("#321d1f")
	diffRightBackgroundColor = lipgloss.Color("#1b2e20")
)

const (
	splitGapWidth       = 4
	minSplitColumnWidth = 24
	maxSplitHeightDelta = 4
	promptStarter       = "[ '' ] ( '' )"
	promptStarterCursor = 3
)
