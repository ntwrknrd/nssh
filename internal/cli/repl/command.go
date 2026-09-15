// Package repl implements the nssh repl command presentations.
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

func NewCmd() *cobra.Command {
	var plain bool
	var concurrency int
	cmd := &cobra.Command{
		Use:   "repl",
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

const replHelp = `Run grouped remote commands across exact inventory or literal targets.

Syntax:
  [ 'host1', 'host2' ] ( 'command1', 'command2' )
  [ 'irn-border-sw(1,2)', 'irn-agg-sw(1,2)' ] ( 'show env power' )
  [ 'select:provider:netbox' ] ( 'show version' )

Values are single quoted; \' escapes a quote. Other backslashes are preserved.
Trailing prefix(1,2) expands suffixes. One select:QUERY may replace the target
list, using nssh inv list fields: host, hostname, id, user, port, provider, group.
No fuzzy selection or inventory creation occurs. Use --target repl at the root
to connect to a host named repl.

Each command runs across all hosts before the next starts. Results appear in
requested host order, with separate stdout/stderr. A failure skips later
commands only on that host. Remote stdin is EOF; authenticate credential providers first.
Interactive host-key approval is serialized. Plain mode cannot prompt for trust.

Interactive default: type to filter inventory, Up/Down moves, Space selects,
Enter opens commands. Enter one command per line; F5 runs the selected hosts.
Tab switches hosts/commands. Selections persist across filters and runs.
Ctrl-A selects matching hosts; Ctrl-X clears selection in the host list.
Ctrl-P/Ctrl-N recalls history. F2 switches to the syntax editor and back.

Syntax editor: Tab completes a unique host or opens a multi-select picker. There,
Space selects, Up/Down moves, Enter inserts, and Esc closes. Up/Down otherwise
recall history. PgUp/PgDn or the mouse wheel scroll. Ctrl-L toggles stacked
results; Ctrl-G toggles line comparison in split panes. Drag selects lines in
one device pane; Ctrl-Y sends up to 64 KiB to the terminal clipboard.
Ctrl-C cancels active work and waits for local cleanup, or
exits when idle. :help shows help; :quit, :exit, or EOF exits.
Cancellation cannot undo remote effects. Normal interactive quit returns zero.
Plain input stops on failure (exit 1); interruption exits 130.

History: XDG state/nssh/repl_history, mode 0600, up to 1000 entries or 1 MiB.
History stores submitted hosts and commands, which may contain sensitive arguments.
Piped input does not write history. Capture is capped at 8 MiB per command;
the transcript at 32 MiB / 4096 display blocks, with visible truncation/eviction.
Submissions: at most 2 MiB before and after suffix expansion, 1000 targets,
100 commands, 10000 host/command pairs.`

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
			_, _ = fmt.Fprintln(out, replHelp)
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
	composer
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
	m.composer = newComposer()
	if historyErr != nil {
		m.appendTranscript("history: " + historyErr.Error() + "\n")
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	owner.program = p
	_, err := p.Run()
	cancel()
	owner.workers.Wait()
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
	switch v := msg.(type) {
	case tuiCopyMsg:
		m.message = "selection sent to terminal clipboard"
		if v.err != nil {
			m.message = "clipboard write failed"
		}
		return m, nil
	case tuiBatchMsg:
		m.batch++
		m.total, m.running, m.done, m.failed, m.canceled, m.skipped = v.targets*v.commands, 0, 0, 0, 0, 0
		m.commandIndex = -1
		return m, nil
	case tuiResultMsg:
		m.acceptResult(v.event)
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(v)
	case *trustRequest:
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
		if v.Type == tea.KeyCtrlL {
			m.stacked = !m.stacked
			m.selected = false
			m.refreshLayout()
			return m, nil
		}
		if v.Type == tea.KeyCtrlY {
			return m, m.copySelection()
		}
		if v.Type == tea.KeyF2 && !m.active {
			m.guided = !m.guided
			m.matches = nil
			m.refreshLayout()
			return m, nil
		}
		if m.guided && !m.active {
			return m.updateComposer(v)
		}
		if len(m.matches) > 0 {
			m.updatePicker(v)
			return m, nil
		}
		if v.Type == tea.KeyEsc && !m.active {
			return m, tea.Quit
		}
		if v.Type == tea.KeyTab && !m.active {
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
		if !m.active && v.Type == tea.KeyDown && len(m.entries) > 0 {
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
			if line == ":help" {
				m.appendTranscript(replHelp + "\n")
				m.input.SetValue("")
				return m, nil
			}
			if line == ":quit" || line == ":exit" {
				return m, tea.Quit
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
	var cmd tea.Cmd
	if m.guided {
		if m.commandFocus {
			m.commands, cmd = m.commands.Update(msg)
		} else {
			m.hostFilter, cmd = m.hostFilter.Update(msg)
		}
		return m, cmd
	}
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
func (m model) View() string { return m.tuiView() }
