// Package repl implements the nssh --tui command presentations.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/cancelreader"
	"github.com/ntwrknrd/nssh/internal/cli/selection"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/exit"
	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func Explanation() string { return replHelp }

func NewCmd() *cobra.Command {
	var plain bool
	var concurrency int
	cmd := &cobra.Command{
		Use:   "--tui",
		Short: "Run commands across inventory targets",
		Long:  replHelp,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			restoreInterrupts := secret.ManageInterrupts()
			defer restoreInterrupts()
			if concurrency < 1 {
				return fmt.Errorf("concurrency must be at least one")
			}
			interactive := !plain && isTerminal(os.Stdin) && isTerminal(os.Stdout)
			if interactive {
				return runTUI(concurrency)
			}
			return runPlain(os.Stdin, os.Stdout, os.Stderr, concurrency)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "Use line-oriented plain output")
	cmd.Flags().IntVar(&concurrency, "concurrency", core.DefaultConcurrency, "Maximum simultaneous hosts")
	return cmd
}

func isTerminal(f *os.File) bool { return f != nil && term.IsTerminal(int(f.Fd())) }

const replHelp = `Run grouped remote commands or open interactive SSH panes with nssh --tui.

Batch (default):
  [ 'host1', 'host2' ] ( 'command1', 'command2' )
  [ 'irn-border-sw(1,2)' ] ( 'show env power' )
  [ 'select:provider:netbox' ] ( 'show version' )

One request bar contains both devices and commands. Up/Down recalls
complete requests, including their devices. Enter runs a filled request. If its
command is empty, Enter moves into the command quotes. Shift-Tab moves between
host and command fields; Alt-Enter adds a quoted value. Deletion preserves the
syntax delimiters. Typing a hostname in an empty bar starts a request template.
Tab inside a device field opens the picker with its hostname prefix in the
filter bar. Typing edits only that filter; Space updates selected devices in
the request bar immediately. Up/Down moves. Enter finishes selection and moves to
commands (or opens interactive sessions). Esc restores the original draft.

Commands run in order across the requested hosts. Failures skip later commands
on that host; commands are never retried. Each command uses a separate execution
with remote stdin at EOF. History saves devices and commands together, deduplicates
identical requests, and restores the whole request for editing.

Local controls (Ctrl-P in either mode; colon commands only here):
  :interactive  Choose devices or resume the connected session tabs

Keys go directly to the displayed targets, including Space, Enter, q, Tab,
arrows and Ctrl-C/Ctrl-D. Remote shells own their history and line editing.
Ctrl-P opens local controls and pauses remote input. Ctrl-K and Ctrl-L clear
scrollback in both modes. Esc returns to sessions.
Within local controls:
  :new          Choose devices for a new tab
  :tab N        Switch tabs (or click a tab; Alt-Left/Right cycles)
  :close        Disconnect this tab
  :reconnect    Reconnect closed panes (Ctrl-R); :reconnect N targets pane N
  :scroll-lock  Toggle linked scrolling (on by default for each tab)
  :batch        Return to batch, keeping tabs connected
  :target N     Send input only to pane N (or click its top border)
  :all          Resume broadcasting to every pane in this tab
  :next, :prev  Change the visible page when more than four panes are open
  :clear        Clear this tab's scrollback
  :copy         Copy selected output
  :help         Open this index
  :quit         Close every session and exit

Only the active tab receives input. Background tabs retain their connections
and output. Each pane preserves its remote shell, CLI state and pagination.
Wait for each device's prompt before sending input. Focus a pane before answering
its confirmation or handling different device states. A closed session pauses
broadcast; input still requires every targeted session to be open. No input is
replayed. An input key reconnects disconnected targets and is discarded. Input
stays paused until you click a pane top border or use :target N or :all after
the new prompts appear. Ctrl-R also reconnects closed panes explicitly.
Each pane shows its connection state separately from its copyable identity.

Other overlay controls:
  :help         Open this index; Esc/Enter closes, arrows/PgUp/PgDn scroll
  :clear        Clear scrollback (Ctrl-K or Ctrl-L in either mode)
  :wipe         Clear batch scrollback and saved batch history
  :quit, :exit  Exit and close all local SSH terminals

The mouse wheel scrolls output. Batch also supports PgUp/PgDn. Drag selects
lines; right-click copies then clears selection. Batch Ctrl-Y also copies. Clipboard
copies are limited to 64 KiB. In batch, status headings are selectable and stay
pinned above their output. The :stacked overlay command toggles stacked batch results; Ctrl-G toggles
line comparison. New output or resizing clears selections.

Interactive supports eight tabs of 1-16 devices each, with up to four visible per page and 1000
scrollback rows per pane. Host-key approval uses a serialized modal prompt.
Authenticate credential providers before starting the TUI. Cancellation and
closing a terminal cannot undo commands already sent to a device.

--plain or piped input uses batch syntax and separate stdout/stderr. It stops on
failure (exit 1); interruption exits 130. Plain sessions do not write history.
Values are single quoted; \' escapes a quote. Other backslashes are preserved.
Suffix lists expand prefix(1,2). Selectors use inventory list matching rules.
Batch history: XDG state/nssh/repl_history, mode 0600, 1000 entries or 1 MiB.
The old repl subcommand is removed; use nssh --tui.
`

func runPlain(in io.Reader, out, errOut io.Writer, concurrency int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runPlainContext(ctx, in, out, errOut, concurrency)
}

func runPlainContext(ctx context.Context, in io.Reader, out, errOut io.Writer, concurrency int) error {
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	stopRead := context.AfterFunc(ctx, func() { reader.Cancel() })
	defer stopRead()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), core.MaxSubmissionBytes+1)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return &exit.ExitError{Code: 130}
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == ":help" {
			_, _ = fmt.Fprint(out, replHelp)
			continue
		}
		if line == ":quit" || line == ":exit" {
			return nil
		}
		if err := runLine(ctx, line, concurrency, out, errOut, nil); err != nil {
			if ctx.Err() != nil {
				return &exit.ExitError{Code: 130}
			}
			return &exit.ExitError{Code: 1, Message: err.Error()}
		}
	}
	if ctx.Err() != nil {
		return &exit.ExitError{Code: 130}
	}
	return scanner.Err()
}

func runLine(ctx context.Context, line string, concurrency int, out, errOut io.Writer, owner *terminalOwner) error {
	submission, err := core.Parse(line)
	if err != nil {
		return err
	}
	return runSubmission(ctx, submission, concurrency, out, errOut, owner)
}

func runSubmission(ctx context.Context, submission core.Submission, concurrency int, out, errOut io.Writer, owner *terminalOwner) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	cat, err := connect.BuildHostCatalog(cfg)
	if err != nil {
		return err
	}
	emitText := func(text string) {
		if owner != nil && owner.program != nil {
			owner.program.Send(replEventMsg{text: text})
		} else {
			_, _ = io.WriteString(errOut, text)
		}
	}
	executor := core.Executor{Concurrency: concurrency, Resolve: catalogResolver(cfg, cat), Run: captureRunner(owner),
		OnTargets: func(targets []core.ResolvedTarget) {
			if owner != nil && owner.program != nil {
				owner.program.Send(tuiBatchMsg{targets: len(targets), commands: len(submission.Commands)})
				return
			}
			names := make([]string, len(targets))
			for i, target := range targets {
				names[i] = target.Identity
			}
			emitText(fmt.Sprintf("%d targets: %s\n", len(names), strings.Join(names, ", ")))
		}, OnEvent: func(event core.Event) {
			if owner != nil && owner.program != nil {
				owner.program.Send(tuiResultMsg{event: event})
				return
			}
			writePlainEvent(event, out, errOut)
		}}
	events, execErr := executor.Execute(ctx, submission)
	if owner == nil || owner.program == nil {
		emitText(renderSummary(events))
	}
	if execErr != nil {
		return execErr
	}
	for _, event := range events {
		if event.State == core.Failed {
			return fmt.Errorf("one or more remote commands failed")
		}
	}

	return nil
}

func writePlainEvent(event core.Event, out, errOut io.Writer) {
	prefix := fmt.Sprintf("[%s] %s: ", event.Target.Identity, event.Command)
	writeStream := func(w io.Writer, data []byte) {
		if len(data) == 0 {
			return
		}
		_, _ = fmt.Fprint(w, prefix)
		_, _ = w.Write(data)
		if data[len(data)-1] != '\n' {
			_, _ = fmt.Fprintln(w)
		}
	}
	writeStream(out, event.Result.Stdout)
	writeStream(errOut, event.Result.Stderr)
	if event.Result.Truncated {
		_, _ = fmt.Fprintln(errOut, prefix+"output truncated")
	}
	if event.State == core.Failed {
		_, _ = fmt.Fprintf(errOut, "%sfailed (exit %d): %v\n", prefix, event.Result.ExitCode, event.Err)
	} else {
		_, _ = fmt.Fprintln(errOut, prefix+string(event.State))
	}
}

func renderSummary(events []core.Event) string {
	type key struct {
		host  string
		index int
	}
	states := make(map[key]core.Event)
	for _, event := range events {
		if event.State != core.Queued && event.State != core.Running {
			states[key{event.Target.Identity, event.CommandIndex}] = event
		}
	}
	if len(states) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Summary (requested order):\n")
	for _, event := range events {
		if event.State == core.Queued {
			result, ok := states[key{event.Target.Identity, event.CommandIndex}]
			if !ok {
				continue
			}
			_, _ = fmt.Fprintf(&b, "[%s] %s: %s", result.Target.Identity, result.Command, result.State)
			if result.State == core.Completed || result.State == core.Failed {
				_, _ = fmt.Fprintf(&b, " (exit %d)", result.Result.ExitCode)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type targetRequest struct {
	cfg        *config.Config
	cat        *connect.HostCatalog
	host, user string
}

func catalogResolver(cfg *config.Config, cat *connect.HostCatalog) core.Resolver {
	return func(_ context.Context, spec core.Target) ([]core.ResolvedTarget, error) {
		queries := []string{spec.Value}
		if spec.Selector {
			selector, err := selection.Compile(spec.Value, []string{"host", "hostname", "id", "user", "port", "provider", "group"})
			if err != nil {
				return nil, fmt.Errorf("invalid selector: %w", err)
			}
			queries = nil
			for _, host := range cat.All() {
				if selector.Match(selection.Row{"host": host.Canonical, "hostname": host.Hostname, "id": sshconfig.DeriveHostID(host.Canonical), "user": host.Username, "port": selectorPort(host.Port), "provider": host.Provider, "group": selectorGroup(host)}) {
					queries = append(queries, host.Canonical)
				}
			}
			sort.Strings(queries)
		}
		out := make([]core.ResolvedTarget, 0, len(queries))
		for _, query := range queries {
			user, host := splitUserHost(query)
			if strings.TrimSpace(host) == "" {
				return nil, fmt.Errorf("target hostname is empty")
			}
			identity := host
			if managed, ok := cat.Find(host); ok {
				identity = managed.Canonical
				if user == "" {
					user = managed.Username
				}
			}
			if user != "" {
				identity = user + "@" + identity
			}
			out = append(out, core.ResolvedTarget{Identity: identity, Value: targetRequest{cfg: cfg, cat: cat, host: host, user: user}})
		}
		return out, nil
	}
}
func selectorGroup(host *connect.ResolvedHostData) string {
	if host == nil || host.Group == "" {
		return "-"
	}
	return config.FormatInventoryGroupID(host.Provider, host.Group)
}
func selectorPort(port int) string {
	if port == 0 {
		port = 22
	}
	return fmt.Sprint(port)
}

func splitUserHost(query string) (string, string) {
	if i := strings.LastIndex(query, "@"); i >= 0 {
		return query[:i], query[i+1:]
	}
	return "", query
}

func captureRunner(owner *terminalOwner) core.Runner {
	return func(ctx context.Context, target core.ResolvedTarget, command []string) core.Result {
		request, ok := target.Value.(targetRequest)
		if !ok {
			return core.Result{Err: fmt.Errorf("invalid target request")}
		}
		resolved, err := connect.ResolveLiteralHostFromCatalog(ctx, request.host, request.user, request.cfg, request.cat)
		if err != nil {
			return core.Result{Err: err, ExitCode: 1}
		}
		opts := connect.CaptureOptions{MaxOutputBytes: core.MaxCommandOutput}
		if owner != nil {
			opts.HostKeyPrompt = func(prompt connector.HostKeyPrompt) connector.HostKeyAction { return owner.prompt(ctx, prompt) }
		}
		result, err := connect.RunRemoteCommandCapture(ctx, resolved, command, opts)
		return core.Result{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, Err: firstError(err, result.Err), Truncated: result.Truncated}
	}
}
func firstError(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// Retain the newest output; losing old output never hides new failures.
type limitedBuffer struct {
	data      string
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	if b.max <= 0 {
		return n, nil
	}
	if n >= b.max {
		b.truncated = b.truncated || n > b.max || len(b.data) > 0
		b.data = string(data[n-b.max:])
		return n, nil
	}
	if excess := len(b.data) + n - b.max; excess > 0 {
		b.data = b.data[excess:]
		b.truncated = true
	}
	b.data += string(data)
	return n, nil
}
func (b *limitedBuffer) String() string { return b.data }

type replEventMsg struct{ text string }
type finishedMsg struct{ err error }
type trustRequest struct {
	prompt   connector.HostKeyPrompt
	response chan connector.HostKeyAction
}
type trustFinishedMsg struct{ request *trustRequest }
type terminalOwner struct {
	program *tea.Program
	ctx     context.Context
	workers sync.WaitGroup
}

// The main UI owns stdin throughout a serialized modal trust decision. Workers
// never start readers or release the terminal while another command is active.
func (o *terminalOwner) prompt(ctx context.Context, prompt connector.HostKeyPrompt) connector.HostKeyAction {
	if o.program == nil {
		return connector.HostKeyReject
	}
	request := &trustRequest{prompt: prompt, response: make(chan connector.HostKeyAction, 1)}
	o.program.Send(request)
	defer o.program.Send(trustFinishedMsg{request: request})
	select {
	case action := <-request.response:
		return action
	case <-ctx.Done():
		return connector.HostKeyReject
	}
}

type model struct {
	tuiState

	input               textinput.Model
	viewport            viewport.Model
	transcript          limitedBuffer
	concurrency         int
	owner               *terminalOwner
	history             historyStore
	entries, candidates []string
	historyAt           int
	active              bool
	cancel              context.CancelFunc
	trust               *trustRequest
}

func runTUI(concurrency int) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := &terminalOwner{ctx: ctx}
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "[ 'host' ] ( 'command' )"
	input.CharLimit = core.MaxSubmissionBytes
	input.Focus()
	entries, historyErr := defaultHistoryStore().load()
	candidates := loadCandidates()
	vp := viewport.New(80, 20)
	m := model{input: input, viewport: vp, transcript: limitedBuffer{max: core.MaxSessionOutput}, concurrency: concurrency, owner: owner, history: defaultHistoryStore(), entries: entries, historyAt: len(entries), candidates: candidates}
	if historyErr != nil {
		m.appendTranscript("history: " + historyErr.Error() + "\n")
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	owner.program = p
	final, err := p.Run()
	cancel()
	owner.workers.Wait()
	if m, ok := final.(model); ok {
		m.closePanes()
	}
	return err
}
func loadCandidates() []string {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil
	}
	cat, err := connect.BuildHostCatalog(cfg)
	if err != nil {
		return nil
	}
	return cat.Suggestions("")
}
func (m model) Init() tea.Cmd { return textinput.Blink }
func safeTerminalText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (!unicode.IsControl(r) && r != '\r') {
			return r
		}
		return -1
	}, ansi.Strip(text))
}
func (m *model) appendTranscript(text string) {
	m.addBlock(tuiBlock{text: safeTerminalText(text)})
}
func (m model) transcriptContent() string { return m.renderBlocks() }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if updated, ok := next.(model); ok && (updated.input.Value() != m.input.Value() || updated.input.Position() != m.input.Position()) {
		if key, ok := msg.(tea.KeyMsg); ok {

			switch key.Type {
			case tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd, tea.KeyCtrlA, tea.KeyCtrlE, tea.KeyUp, tea.KeyDown, tea.KeyCtrlP, tea.KeyCtrlN, tea.KeyTab, tea.KeyShiftTab:
				if !updated.interactive {
					updated.clampFormCursor()
				}
			}
		}
		updated.refreshLayout()
		return updated, cmd
	}
	return next, cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.trust == nil && !m.helpOpen {
		if key, ok := msg.(tea.KeyMsg); ok {
			if m.interactive && !m.choosing && key.Alt && (key.Type == tea.KeyLeft || key.Type == tea.KeyRight) {
				delta := 1
				if key.Type == tea.KeyLeft {
					delta = -1
				}
				m.cycleTab(delta)
				return m, nil
			}
			if key.Type == tea.KeyCtrlR && m.interactive && !m.choosing {
				m.reconnectTerminals(-1)
				return m, nil
			}
			switch key.Type {
			case tea.KeyCtrlK, tea.KeyCtrlL:
				m.clearDisplay()
				return m, nil
			case tea.KeyCtrlP:
				m.controlOpen = !m.controlOpen
				if m.controlOpen {
					m.controlInput = textinput.New()
					m.controlInput.Focus()
				}
				return m, nil
			}
		}
		if m.controlOpen {
			switch msg.(type) {
			case tea.KeyMsg, tea.MouseMsg:
				next, cmd, _ := m.updateTerminalControl(msg)
				return next, cmd
			}
		}
	}
	if next, cmd, handled := m.updateInteractive(msg); handled {
		return next, cmd
	}

	switch v := msg.(type) {
	case tuiCopyMsg:
		m.message = "selection sent to terminal clipboard"
		if v.err == nil && v.clearAfter && m.selectedText() == v.text {
			m.selected = false
			m.selecting = false
			m.viewport.SetContent(m.renderBlocks())
		}
		if v.err != nil {
			m.message = "clipboard write failed"
		}
		return m, nil
	case tuiBatchMsg:
		m.batch++
		m.total, m.running, m.done, m.failed, m.canceled, m.skipped = v.targets*v.commands, 0, 0, 0, 0, 0
		return m, nil
	case tuiResultMsg:
		m.acceptResult(v.event)
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(v)
	case *trustRequest:
		m.helpOpen = false
		m.trust = v
		return m, nil
	case trustFinishedMsg:
		if m.trust == v.request {
			m.trust = nil
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.selected = false
		m.refreshLayout()
		return m, nil
	case replEventMsg:
		m.appendTranscript(v.text)
		return m, nil
	case tea.KeyMsg:
		if m.helpOpen {
			switch v.Type {
			case tea.KeyEsc, tea.KeyEnter, tea.KeyCtrlC:
				m.helpOpen = false
			case tea.KeyUp:
				m.helpOffset = max(0, m.helpOffset-1)
			case tea.KeyDown:
				m.helpOffset = min(m.helpMaxOffset(), m.helpOffset+1)
			case tea.KeyPgUp:
				m.helpOffset = max(0, m.helpOffset-10)
			case tea.KeyPgDown:
				m.helpOffset = min(m.helpMaxOffset(), m.helpOffset+10)
			case tea.KeyHome:
				m.helpOffset = 0
			case tea.KeyEnd:
				m.helpOffset = m.helpMaxOffset()
			}
			return m, nil
		}
		if v.Type == tea.KeyCtrlC {
			if m.active {
				m.cancel()
				m.appendTranscript("canceling active submission...\n")
				return m, nil
			}
			return m, tea.Quit
		}
		if m.trust != nil {
			action := connector.HostKeyReject
			switch v.String() {
			case "o":
				action = connector.HostKeyAcceptOnce
			case "a":
				action = connector.HostKeyAcceptAlways
			case "r":
			default:
				return m, nil
			}
			m.trust.response <- action
			m.trust = nil
			return m, nil
		}
		if v.Type == tea.KeyCtrlD {
			if m.active {
				m.cancel()
				return m, nil
			}
			return m, tea.Quit
		}
		if v.Type == tea.KeyCtrlG {
			m.diff = !m.diff
			m.selected = false
			m.refreshLayout()
			return m, nil
		}
		if v.Type == tea.KeyCtrlY {
			return m, m.copySelection(false)
		}
		if m.pickerOpen {
			m.updatePicker(v)
			if v.Type == tea.KeyEnter && !m.pickerOpen && m.interactive && m.choosing {
				next, cmd, _ := m.updateInteractive(v)
				return next, cmd
			}
			return m, nil
		}
		if !m.active && m.moveEditorCursor(v) {
			return m, nil
		}
		if v.Type == tea.KeyEsc && !m.active {
			return m, tea.Quit
		}
		if !m.active && v.Type == tea.KeyShiftTab {
			m.focusForm(!m.commandFocused())
			return m, nil
		}
		if !m.active && v.Type == tea.KeyEnter && v.Alt {
			m.addFormRow()
			return m, nil
		}
		if v.Type == tea.KeyTab && !m.active {
			if m.commandFocused() {
				return m, nil
			}
			m.openPicker()
			return m, nil
		}
		if v.Type == tea.KeyPgUp || v.Type == tea.KeyPgDown || v.String() == "up" && m.active || v.String() == "down" && m.active {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(v)
			return m, cmd
		}
		if !m.active && v.Type == tea.KeyUp && len(m.entries) > 0 {
			if m.historyAt > 0 {
				m.historyAt--
			}
			m.restoreHistory(m.entries[m.historyAt])
			return m, nil
		}
		if !m.active && (v.Type == tea.KeyDown || v.Type == tea.KeyCtrlN) && len(m.entries) > 0 {
			if m.historyAt < len(m.entries)-1 {
				m.historyAt++
				m.restoreHistory(m.entries[m.historyAt])
			} else {
				m.historyAt = len(m.entries)
				m.input.SetValue("")
			}
			return m, nil
		}
		if !m.active && v.Type == tea.KeyEnter {
			line := strings.TrimSpace(m.input.Value())
			if !strings.HasPrefix(line, ":") && !m.commandFocused() {
				_, commands := m.formFields()
				if len(commands) == 1 && commands[0].start == commands[0].end {
					m.focusForm(true)
					return m, nil
				}
			}
			if line == "" {
				return m, nil
			}
			submission, err := core.Parse(line)
			if err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.input.SetValue("")
			m.startSubmission(submission, line)
			return m, nil
		}
	case finishedMsg:
		m.active = false
		m.cancel = nil
		m.candidates = loadCandidates()
		if v.err != nil && v.err != context.Canceled {
			m.appendTranscript("error: " + v.err.Error() + "\n")
		}
		return m, nil
	}
	if m.active {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if cmd, handled := m.deleteEditorField(key); handled {
			return m, cmd
		}
	}
	// Ordinary typing starts in the first quoted device. Explicit REPL syntax
	// keeps its original input path, including paste.
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyRunes && len(key.Runes) > 0 && m.input.Value() == "" {
		first := key.Runes[0]
		if first != '[' && !unicode.IsSpace(first) {
			m.input.SetValue("[ '' ] ( '' )")
			m.input.SetCursor(3)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// Clear retained display output without canceling work or changing the draft,
// command history, execution counters, or layout preferences.
func (m *model) clearScrollback() {
	m.blocks = nil
	m.bytes = 0
	m.transcript = limitedBuffer{max: m.transcript.max}
	m.selected = false
	m.selecting = false
	m.selectionHeader = false
	m.selectionStart = 0
	m.selectionEnd = 0
	m.selectionBlock = 0
	m.selectionBodyStart = 0
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.message = "scrollback cleared"
}

func (m model) View() string { return m.tuiView() }
