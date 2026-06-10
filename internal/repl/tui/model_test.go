package tui

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestSubmitStartsBatchAndUpdatesStatus(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge(1,2)' ] ( 'show hostname' )")

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)

	if !got.active {
		t.Fatal("model should have active batch after submit")
	}
	if got.statusLine() != "running 0/2  done 0/2  failed 0/2  pending 2/2" {
		t.Fatalf("status = %q", got.statusLine())
	}
}

func TestStartedEventMovesPendingToRunning(t *testing.T) {
	m0 := newTestModel()
	m0.total = 2
	m0.pending = 2
	m0.startedMap = map[int]bool{}
	m0.active = true

	next, _ := m0.Update(fanoutEventMsg{
		OK: true,
		Event: repl.FanoutEvent{
			Kind: repl.FanoutStarted,
			Host: "edge01",
		},
	})
	got := next.(model)

	if got.statusLine() != "running 1/2  done 0/2  failed 0/2  pending 1/2" {
		t.Fatalf("status = %q", got.statusLine())
	}
}

func TestCompletedEventAppendsBannerBlock(t *testing.T) {
	m0 := newTestModel()
	m0.command = "show hostname"
	m0.total = 1
	m0.active = true

	next, _ := m0.Update(fanoutEventMsg{
		OK: true,
		Event: repl.FanoutEvent{
			Kind: repl.FanoutCompleted,
			Result: repl.CommandResult{
				Host:    "edge01",
				Command: "show hostname",
				Output:  []byte("Hostname: edge01\n"),
			},
		},
	})
	got := next.(model)

	if !strings.Contains(got.transcript, "edge01 | show hostname") {
		t.Fatalf("transcript missing banner: %q", got.transcript)
	}
	if !strings.Contains(got.transcript, "Hostname: edge01") {
		t.Fatalf("transcript missing output: %q", got.transcript)
	}
	if got.active {
		t.Fatal("single-target batch should no longer be active")
	}
}

func TestFailedEventAppendsErrorText(t *testing.T) {
	m0 := newTestModel()
	m0.command = "show hostname"
	m0.total = 1
	m0.active = true

	next, _ := m0.Update(fanoutEventMsg{
		OK: true,
		Event: repl.FanoutEvent{
			Kind: repl.FanoutCompleted,
			Result: repl.CommandResult{
				Host:    "edge01",
				Command: "show hostname",
				Err:     errors.New("boom"),
			},
		},
	})
	got := next.(model)

	if !strings.Contains(got.transcript, "error: boom") {
		t.Fatalf("transcript missing error: %q", got.transcript)
	}
}

func TestSubmittedBatchCanAppendCompletionAfterModelCopy(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEnter})
	submitted := next.(model)

	next, _ = submitted.Update(fanoutEventMsg{
		OK: true,
		Event: repl.FanoutEvent{
			Kind: repl.FanoutCompleted,
			Result: repl.CommandResult{
				Host:    "edge01",
				Command: "show hostname",
				Output:  []byte("edge01\n"),
			},
		},
	})
	got := next.(model)

	if !strings.Contains(got.transcript, "edge01 | show hostname") {
		t.Fatalf("transcript missing completion after submit: %q", got.transcript)
	}
}

func TestCompletedEventsRenderInTargetOrder(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge(1,2)' ] ( 'show hostname' )")

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEnter})
	submitted := next.(model)

	next, _ = submitted.Update(fanoutEventMsg{
		OK: true,
		Event: repl.FanoutEvent{
			Kind:  repl.FanoutCompleted,
			Index: 1,
			Result: repl.CommandResult{
				Host:    "edge2",
				Command: "show hostname",
				Output:  []byte("edge2\n"),
			},
		},
	})
	afterSecond := next.(model)
	if strings.Contains(afterSecond.transcript, "edge2 | show hostname") {
		t.Fatalf("out-of-order completion rendered early: %q", afterSecond.transcript)
	}

	next, _ = afterSecond.Update(fanoutEventMsg{
		OK: true,
		Event: repl.FanoutEvent{
			Kind:  repl.FanoutCompleted,
			Index: 0,
			Result: repl.CommandResult{
				Host:    "edge1",
				Command: "show hostname",
				Output:  []byte("edge1\n"),
			},
		},
	})
	got := next.(model)

	first := strings.Index(got.transcript, "edge1 | show hostname")
	second := strings.Index(got.transcript, "edge2 | show hostname")
	if first == -1 || second == -1 || first > second {
		t.Fatalf("transcript not in target order: %q", got.transcript)
	}
}

func TestWideTranscriptRendersAdjacentResultBlocksSideBySide(t *testing.T) {
	m0 := newTestModel()
	m0.width = 120
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show one", Output: []byte("one\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show two", Output: []byte("two\n")})

	text := stripANSI(m0.transcript)
	if !strings.Contains(text, "edge01 | show one") || !strings.Contains(text, "edge02 | show two") ||
		!bannersShareLine(text, "edge01 | show one", "edge02 | show two") {
		t.Fatalf("expected side-by-side banners:\n%s", text)
	}
}

func TestSplitTranscriptNormalizesCarriageReturns(t *testing.T) {
	m0 := newTestModel()
	m0.width = 120
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show one", Output: []byte("one\r\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show two", Output: []byte("two\r\n")})

	text := stripANSI(m0.transcript)
	if strings.Contains(text, "\r") {
		t.Fatalf("transcript should not contain carriage returns: %q", text)
	}
}

func TestSplitTranscriptClipsLinesToViewportWidth(t *testing.T) {
	m0 := newTestModel()
	m0.width = 80
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show one", Output: []byte(strings.Repeat("x", 32) + "\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show two", Output: []byte(strings.Repeat("y", 32) + "\n")})

	text := stripANSI(m0.transcript)
	if !bannersShareLine(text, "edge01 | show one", "edge02 | show two") {
		t.Fatalf("expected side-by-side banners:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) > m0.width {
			t.Fatalf("line width = %d, want <= %d: %q", lipgloss.Width(line), m0.width, line)
		}
	}
}

func TestSplitTranscriptAddsLineNumbersPerSide(t *testing.T) {
	m0 := newTestModel()
	m0.width = 120
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show one", Output: []byte("alpha\nbeta\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show two", Output: []byte("gamma\ndelta\n")})

	text := stripANSI(m0.transcript)
	if !lineContains(text, "1 alpha", "1 gamma") {
		t.Fatalf("first output row missing per-side line numbers:\n%s", text)
	}
	if !lineContains(text, "2 beta", "2 delta") {
		t.Fatalf("second output row missing per-side line numbers:\n%s", text)
	}
}

func bannersShareLine(text, left, right string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, left) && strings.Contains(line, right) {
			return true
		}
	}
	return false
}

func lineContains(text string, parts ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		matched := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestViewRendersPromptBoxAboveStatusLine(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")
	m0.input.CursorEnd()
	view := m0.View()

	promptIdx := strings.Index(stripANSI(view), "nssh> [ 'edge01' ] ( 'show hostname' )|")
	statusIdx := strings.Index(view, statusStyle.Render(m0.statusLine()))
	if promptIdx == -1 || !strings.Contains(view, "┌") || !strings.Contains(view, "┐") ||
		!strings.Contains(view, "└") || !strings.Contains(view, "┘") {
		t.Fatalf("view missing boxed prompt:\n%s", view)
	}
	if statusIdx == -1 {
		t.Fatalf("view missing status line:\n%s", view)
	}
	if promptIdx > statusIdx {
		t.Fatalf("prompt should render above status:\n%s", view)
	}
}

func TestNewModelStartsPromptAtTargetPrefix(t *testing.T) {
	m0 := newTestModel()

	if got := m0.input.Value(); got != promptStarter {
		t.Fatalf("input value = %q, want starter", got)
	}
}

func TestPromptInputViewShowsDimmedSyntaxExampleAtTargetPrefix(t *testing.T) {
	m0 := newTestModel()

	got := m0.promptInputView()
	plain := stripANSI(got)

	if !strings.Contains(plain, "['|TARGET'] ('COMMAND')") {
		t.Fatalf("prompt input should show starter syntax: %q", plain)
	}
}

func TestPromptInputViewColorCodesLiveTargetText(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01', 'edge02' ] ( 'show hostname --json' )")
	m0.input.CursorEnd()

	got := m0.promptInputView()
	plain := stripANSI(got)

	if !strings.Contains(got, targetStyle.Render("edge01")) {
		t.Fatalf("prompt input should color target text: %q", got)
	}
	if !strings.Contains(got, targetStyle.Render("edge02")) {
		t.Fatalf("prompt input should color second target text: %q", got)
	}
	if !strings.Contains(got, commandEchoStyle.Render("show hostname")) {
		t.Fatalf("prompt input should color command text: %q", got)
	}
	if !strings.Contains(got, flagStyle.Render("--json")) {
		t.Fatalf("prompt input should color flags: %q", got)
	}
	if !strings.Contains(plain, "[ 'edge01', 'edge02' ] ( 'show hostname --json' )|") {
		t.Fatalf("prompt input should visually group target and command tokens: %q", plain)
	}
	if !strings.Contains(got, promptGroupStyle.Render("[")) {
		t.Fatalf("target bracket should use group style: %q", got)
	}
	if !promptGroupStyle.GetFaint() {
		t.Fatalf("group style should be faint")
	}
}

func TestPromptInputViewUsesSolidSyntaxForFilledGroups(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")
	m0.input.CursorEnd()

	got := m0.promptInputView()

	if !strings.Contains(got, promptGroupSolidStyle.Render("[")) {
		t.Fatalf("filled target bracket should be solid: %q", got)
	}
	if !strings.Contains(got, promptGroupSolidStyle.Render("'")) {
		t.Fatalf("filled group quotes should be solid: %q", got)
	}
	if !strings.Contains(got, promptGroupSolidStyle.Render("(")) {
		t.Fatalf("filled command paren should be solid: %q", got)
	}
	if promptGroupSolidStyle.GetFaint() {
		t.Fatal("solid prompt group style should not be faint")
	}
}

func TestPromptInputViewKeepsStarterSyntaxDimmed(t *testing.T) {
	m0 := newTestModel()

	got := m0.promptInputView()

	if !strings.Contains(got, promptGroupStyle.Render("[")) {
		t.Fatalf("starter bracket should stay dimmed: %q", got)
	}
	groups := promptGroupStateFor(promptStarter, "")
	if groups.targetFilled || groups.commandFilled {
		t.Fatalf("starter groups should not be filled: %+v", groups)
	}
}

func TestPromptInputViewKeepsSuggestedTargetSyntaxDimmed(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'ed' ] ( '' )")
	m0.input.SetCursor(5)

	got := m0.promptInputView()

	if !strings.Contains(got, promptGroupStyle.Render("[")) {
		t.Fatalf("suggested target bracket should stay dimmed: %q", got)
	}
	groups := promptGroupStateFor(m0.input.Value(), m0.targetSuggestion(m0.input.Value()))
	if groups.targetFilled {
		t.Fatalf("suggested target group should not be filled before acceptance: %+v", groups)
	}
}

func TestPromptInputViewShowsPipeCursorAtEditPosition(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")
	m0.input.SetCursor(len([]rune("[ 'edge01' ] ( 'show")))

	got := stripANSI(m0.promptInputView())

	if !strings.Contains(got, "( 'show| hostname' )") {
		t.Fatalf("prompt input should show pipe cursor at edit position: %q", got)
	}
}

func TestPromptInputViewCanShowUnderscoreCursor(t *testing.T) {
	m0 := newTestModel()
	m0.opts.PromptCursor = repl.PromptCursorUnderscore
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")
	m0.input.CursorEnd()

	got := stripANSI(m0.promptInputView())

	if !strings.Contains(got, "( 'show hostname' )_") {
		t.Fatalf("prompt input should show underscore cursor: %q", got)
	}
}

func TestPromptInputViewMovesToCommandGroupAfterTargetSpace(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.CursorEnd()

	got := m0.promptInputView()
	plain := stripANSI(got)

	if !strings.Contains(plain, "[ 'edge01' ] ( '' )|") {
		t.Fatalf("prompt input should open command group after target space: %q", plain)
	}
}

func TestPromptInputViewRendersQuotedMultiCommandGroup(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01', 'edge02' ] ( 'show ip int brief', 'show version' )")
	m0.input.CursorEnd()

	got := m0.promptInputView()
	plain := stripANSI(got)

	if !strings.Contains(plain, "[ 'edge01', 'edge02' ] ( 'show ip int brief', 'show version' )|") {
		t.Fatalf("prompt input should render quoted command group: %q", plain)
	}
	if !strings.Contains(got, commandEchoStyle.Render("show ip int brief")) {
		t.Fatalf("prompt input should color first command text: %q", got)
	}
	if !strings.Contains(got, commandEchoStyle.Render("show version")) {
		t.Fatalf("prompt input should color second command text: %q", got)
	}
}

func TestPromptInputViewKeepsSubmittedTextUnchanged(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01', 'edge02' ] ( 'show hostname --json' )")

	_ = m0.promptInputView()

	if got := m0.input.Value(); got != "[ 'edge01', 'edge02' ] ( 'show hostname --json' )" {
		t.Fatalf("input value changed to %q", got)
	}
}

func TestSubmitStarterPromptDoesNotParse(t *testing.T) {
	m0 := newTestModel()

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)

	if got.message != "" {
		t.Fatalf("message = %q, want empty", got.message)
	}
	if got.started != 0 {
		t.Fatalf("started = %d, want 0", got.started)
	}
	if got.input.Value() != promptStarter {
		t.Fatalf("input value = %q, want starter", got.input.Value())
	}
}

func TestPromptInputViewShowsFaintTargetSuggestion(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'ed' ] ( '' )")
	m0.input.SetCursor(5)

	got := m0.promptInputView()

	if !strings.Contains(got, targetSuggestionStyle.Render("ge01")) {
		t.Fatalf("prompt input should show faint target suggestion: %q", got)
	}
}

func TestPromptInputViewSuggestionDoesNotInsertExtraSpace(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'ed' ] ( '' )")
	m0.input.SetCursor(5)

	got := m0.promptInputView()

	if !strings.Contains(got, targetSuggestionStyle.Render("ge01")) {
		t.Fatalf("prompt input should attach suggestion to prefix: %q", got)
	}
}

func TestPromptInputViewHidesSuggestionWhenCursorIsInsideTarget(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'acm-lab-agg-sw1' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'acm-lab-a")))

	got := m0.promptInputView()
	plain := stripANSI(got)

	if strings.Contains(plain, "acm-lab-a|gg-sw1") {
		return
	}
	t.Fatalf("prompt input should only show real target suffix after cursor: %q", plain)
}

func TestPromptInputViewRendersSuggestionAfterLastTypedRune(t *testing.T) {
	m0 := newTestModelWithResolver(fakeResolver{
		suggestions: []string{"acm-lab-agg-sw1"},
	})
	m0.input.SetValue("[ 'acm-lab-ag' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'acm-lab-ag")))

	got := stripANSI(m0.promptInputView())

	if !strings.Contains(got, "acm-lab-ag|g-sw1") {
		t.Fatalf("prompt input should render cursor after typed prefix before suggestion: %q", got)
	}
	if strings.Contains(got, "acm-lab-a|g-sw1g") {
		t.Fatalf("prompt input moved last typed rune after suggestion: %q", got)
	}
}

func TestTargetStyleUsesLighterBlue(t *testing.T) {
	got := targetStyle.GetForeground()

	if got != lipgloss.Color("81") {
		t.Fatalf("target foreground = %q, want 81", got)
	}
}

func TestCommandStyleUsesGreen(t *testing.T) {
	got := commandEchoStyle.GetForeground()

	if got != lipgloss.Color("10") {
		t.Fatalf("command foreground = %q, want 10", got)
	}
}

func TestPromptGroupStyleMatchesRatatuiGrayDim(t *testing.T) {
	got := promptGroupStyle.GetForeground()

	if got != ui.ColorGray {
		t.Fatalf("prompt group foreground = %q, want %q", got, ui.ColorGray)
	}
	if !promptGroupStyle.GetFaint() {
		t.Fatal("prompt group style should be faint")
	}
}

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestResizeReservesPromptBoxAndStatusFooter(t *testing.T) {
	m0 := newTestModel()
	m0.height = 24

	m0.resize(4)

	if m0.view.Height != 20 {
		t.Fatalf("viewport height = %d, want 20", m0.view.Height)
	}
}

func TestMouseWheelScrollsTranscriptViewport(t *testing.T) {
	m0 := newTestModel()
	m0.view.Height = 3
	m0.view.SetContent(strings.Join([]string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
		"line 6",
	}, "\n"))

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := next.(model)

	if got.view.YOffset == 0 {
		t.Fatal("mouse wheel should scroll transcript viewport")
	}
}

func TestLeakedMouseEscapeRunesDoNotEnterPrompt(t *testing.T) {
	m0 := newTestModel()

	next, _ := m0.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("[<64;64;32M[<65;64;32M"),
	})
	got := next.(model)

	if got.input.Value() != promptStarter {
		t.Fatalf("prompt value = %q, want starter", got.input.Value())
	}
}

func TestMalformedLeakedMouseEscapeRunesDoNotEnterPrompt(t *testing.T) {
	m0 := newTestModel()

	next, _ := m0.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("[<64;53;17M[<65;22M[<65;77;33M"),
	})
	got := next.(model)

	if got.input.Value() != promptStarter {
		t.Fatalf("prompt value = %q, want starter", got.input.Value())
	}
}

func TestPastedMouseLikeTextCanEnterPrompt(t *testing.T) {
	m0 := newTestModel()

	next, _ := m0.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("[<64;64;32M"),
		Paste: true,
	})
	got := next.(model)

	if got.input.Value() != "[ '[<64;64;32M' ] ( '' )" {
		t.Fatalf("prompt value = %q", got.input.Value())
	}
}

func TestMouseDragSelectsTranscriptText(t *testing.T) {
	m0 := newTestModel()
	m0.width = 20
	m0.height = 8
	m0.resize(4)
	m0.appendTranscript("alpha\nbravo\ncharlie\n")

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      0,
	})
	pressed := next.(model)

	next, _ = pressed.Update(tea.MouseMsg{
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
		X:      5,
		Y:      1,
	})
	dragged := next.(model)

	next, _ = dragged.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      5,
		Y:      1,
	})
	got := next.(model)

	if got.selectedText() != "alpha\nbravo" {
		t.Fatalf("selected text = %q", got.selectedText())
	}
	if !got.selection.has {
		t.Fatal("drag should leave a selection")
	}
}

func TestMouseDragSelectionDoesNotCrossSplitPanes(t *testing.T) {
	m0 := newTestModel()
	m0.width = 80
	m0.height = 8
	m0.resize(4)
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\nleft-two\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\nright-two\n")})

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      1,
	})
	pressed := next.(model)

	next, _ = pressed.Update(tea.MouseMsg{
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
		X:      79,
		Y:      2,
	})
	dragged := next.(model)

	next, _ = dragged.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      79,
		Y:      2,
	})
	got := next.(model)

	selected := got.selectedText()
	if !strings.Contains(selected, "left-one") || !strings.Contains(selected, "left-two") {
		t.Fatalf("selection missing left pane text: %q", selected)
	}
	if strings.Contains(selected, "right-one") || strings.Contains(selected, "right-two") {
		t.Fatalf("selection crossed into right pane: %q", selected)
	}
}

func TestMouseDragSelectionIgnoresSplitGutter(t *testing.T) {
	m0 := newTestModel()
	m0.width = 80
	m0.height = 8
	m0.resize(4)
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\nleft-two\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\nright-two\n")})

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      1,
	})
	pressed := next.(model)

	next, _ = pressed.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      12,
		Y:      1,
	})
	got := next.(model)

	if got.selectedText() != "left-one" {
		t.Fatalf("selected text = %q, want body without gutter", got.selectedText())
	}
}

func TestMouseDragSelectionStaysInPaneWhenOtherPaneHasMoreRows(t *testing.T) {
	m0 := newTestModel()
	m0.width = 80
	m0.height = 8
	m0.resize(4)
	m0.appendResult(repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\n")})
	m0.appendResult(repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\nright-two\nright-three\n")})

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      1,
	})
	pressed := next.(model)

	next, _ = pressed.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      79,
		Y:      3,
	})
	got := next.(model)

	selected := got.selectedText()
	if strings.Contains(selected, "right-two") || strings.Contains(selected, "right-three") {
		t.Fatalf("selection crossed into longer right pane: %q", selected)
	}
	if strings.TrimSpace(selected) != "left-one" {
		t.Fatalf("selected text = %q, want only left pane text", selected)
	}
}

func TestMouseDragSelectionUsesFullWidthOutsideSplits(t *testing.T) {
	m0 := newTestModel()
	m0.width = 80
	m0.height = 8
	m0.resize(4)
	m0.appendTranscript("alpha beta gamma\n")

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      0,
	})
	pressed := next.(model)

	next, _ = pressed.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      16,
		Y:      0,
	})
	got := next.(model)

	if got.selectedText() != "alpha beta gamma" {
		t.Fatalf("selected text = %q", got.selectedText())
	}
}

func TestSelectionPreservesUnselectedTranscriptStyles(t *testing.T) {
	styledHost := "\x1b[38;5;81medge01\x1b[0m"
	transcript := styledHost + " plain"
	selected := renderSelectedTranscript(transcript, selectionState{
		has:    true,
		anchor: selectionPoint{line: 0, col: 7},
		cursor: selectionPoint{line: 0, col: 12},
	})

	if !strings.Contains(selected, styledHost) {
		t.Fatalf("selection should preserve unselected styled text: %q", selected)
	}
	if !strings.Contains(stripANSI(selected), "edge01 plain") {
		t.Fatalf("selection should preserve visible text: %q", selected)
	}
}

func TestMouseWheelStillScrollsWithSelectionSupport(t *testing.T) {
	m0 := newTestModel()
	m0.view.Height = 3
	m0.appendTranscript(strings.Join([]string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
		"line 6",
	}, "\n"))

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	got := next.(model)

	if got.view.YOffset == 0 {
		t.Fatal("mouse wheel should still scroll transcript viewport")
	}
}

func TestCtrlYCopiesSelection(t *testing.T) {
	var out bytes.Buffer
	m0 := newTestModel()
	m0.opts.Out = &out
	m0.appendTranscript("alpha\nbravo\n")
	m0.selection = selectionState{
		has:    true,
		anchor: selectionPoint{line: 0, col: 0},
		cursor: selectionPoint{line: 0, col: 5},
	}

	next, cmd := m0.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	got := next.(model)

	if cmd == nil {
		t.Fatal("ctrl+y with selection should return copy command")
	}
	msg := cmd()
	if copied, ok := msg.(clipboardCopiedMsg); !ok || copied.bytes != 5 || copied.err != nil {
		t.Fatalf("copy msg = %#v", msg)
	}
	if !strings.Contains(out.String(), "YWxwaGE=") {
		t.Fatalf("copy output missing OSC52 payload: %q", out.String())
	}
	if got.message != "copying selection (5 bytes)" {
		t.Fatalf("message = %q", got.message)
	}
}

func TestMouseReleaseCopiesSelection(t *testing.T) {
	var out bytes.Buffer
	m0 := newTestModel()
	m0.opts.Out = &out
	m0.width = 20
	m0.height = 8
	m0.resize(4)
	m0.appendTranscript("alpha\nbravo\n")
	m0.selection = selectionState{
		has:    true,
		active: true,
		anchor: selectionPoint{line: 0, col: 0},
		cursor: selectionPoint{line: 1, col: 5},
	}

	next, cmd := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      5,
		Y:      1,
	})
	got := next.(model)

	if cmd == nil {
		t.Fatal("mouse release with selected text should return copy command")
	}
	msg := cmd()
	if copied, ok := msg.(clipboardCopiedMsg); !ok || copied.bytes != len("alpha\nbravo") || copied.err != nil {
		t.Fatalf("copy msg = %#v", msg)
	}
	if !strings.Contains(out.String(), "YWxwaGEKYnJhdm8=") {
		t.Fatalf("copy output missing OSC52 payload: %q", out.String())
	}
	if got.message != "copying selection (11 bytes)" {
		t.Fatalf("message = %q", got.message)
	}
	if got.selectedText() != "alpha\nbravo" {
		t.Fatalf("selected text = %q", got.selectedText())
	}
}

func TestClipboardCopiedMsgUpdatesStatus(t *testing.T) {
	m0 := newTestModel()

	next, _ := m0.Update(clipboardCopiedMsg{bytes: 11})
	got := next.(model)

	if got.message != "copied selection (11 bytes)" {
		t.Fatalf("message = %q", got.message)
	}
}

func TestMouseReleaseWithoutRangeDoesNotCopy(t *testing.T) {
	m0 := newTestModel()
	m0.width = 20
	m0.height = 8
	m0.resize(4)
	m0.appendTranscript("alpha\n")

	next, _ := m0.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      0,
	})
	pressed := next.(model)

	next, cmd := pressed.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      0,
		Y:      0,
	})
	got := next.(model)

	if cmd != nil {
		t.Fatal("single click should not copy")
	}
	if got.selection.has {
		t.Fatal("single click should clear selection")
	}
}

func TestCtrlYWithoutSelectionDoesNotCopy(t *testing.T) {
	m0 := newTestModel()

	next, cmd := m0.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	got := next.(model)

	if cmd != nil {
		t.Fatal("ctrl+y without selection should not return copy command")
	}
	if got.message != "no selection" {
		t.Fatalf("message = %q", got.message)
	}
}

func TestEscapeClearsSelection(t *testing.T) {
	m0 := newTestModel()
	m0.appendTranscript("alpha\n")
	m0.selection = selectionState{
		has:    true,
		anchor: selectionPoint{line: 0, col: 0},
		cursor: selectionPoint{line: 0, col: 5},
	}
	m0.refreshViewportContent()

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)

	if got.selection.has {
		t.Fatal("escape should clear selection")
	}
	if got.selectedText() != "" {
		t.Fatalf("selected text = %q, want empty", got.selectedText())
	}
}

func TestEnterDuringActiveBatchDoesNotStartSecondBatch(t *testing.T) {
	m0 := newTestModel()
	m0.active = true
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)

	if got.message != "batch already running" {
		t.Fatalf("message = %q", got.message)
	}
	if got.started != 0 {
		t.Fatalf("started = %d, want 0", got.started)
	}
}

func TestCtrlCCancelsActiveBatchThenExits(t *testing.T) {
	m0 := newTestModel()
	m0.active = true
	m0.cancel = func() { m0.cancelled = true }

	next, cmd := m0.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := next.(model)
	if !got.cancelled || got.active {
		t.Fatalf("after first ctrl+c: cancelled=%v active=%v", got.cancelled, got.active)
	}
	if cmd != nil {
		t.Fatal("first ctrl+c should cancel, not quit")
	}

	_, cmd = got.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("second ctrl+c should quit")
	}
}

func TestUpDownCycleSubmittedCommandHistory(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyEnter})
	first := next.(model)
	first.active = false

	first.input.SetValue("[ 'edge02' ] ( 'show version' )")
	next, _ = first.Update(tea.KeyMsg{Type: tea.KeyEnter})
	second := next.(model)
	second.active = false

	next, _ = second.Update(tea.KeyMsg{Type: tea.KeyUp})
	got := next.(model)
	if got.input.Value() != "[ 'edge02' ] ( 'show version' )" {
		t.Fatalf("up input = %q", got.input.Value())
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyUp})
	got = next.(model)
	if got.input.Value() != "[ 'edge01' ] ( 'show hostname' )" {
		t.Fatalf("second up input = %q", got.input.Value())
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyDown})
	got = next.(model)
	if got.input.Value() != "[ 'edge02' ] ( 'show version' )" {
		t.Fatalf("down input = %q", got.input.Value())
	}
}

func TestModelLoadsCommandHistoryFromStore(t *testing.T) {
	store := &fakeHistoryStore{lines: []string{"[ 'edge01' ] ( 'show hostname' )"}}
	m0 := newTestModelWithHistory(store)

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyUp})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( 'show hostname' )" {
		t.Fatalf("history input = %q", got.input.Value())
	}
}

func TestSubmitAppendsCommandHistoryToStore(t *testing.T) {
	store := &fakeHistoryStore{}
	m0 := newTestModelWithHistory(store)
	m0.input.SetValue("[ 'edge01' ] ( 'show hostname' )")

	_, _ = m0.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(store.appended) != 1 || store.appended[0] != "[ 'edge01' ] ( 'show hostname' )" {
		t.Fatalf("appended history = %#v", store.appended)
	}
}

func TestHelpDoesNotAppendCommandHistory(t *testing.T) {
	store := &fakeHistoryStore{}
	m0 := newTestModelWithHistory(store)
	m0.input.SetValue(":help")

	_, _ = m0.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(store.appended) != 0 {
		t.Fatalf("appended history = %#v, want none", store.appended)
	}
}

func TestNewTestModelDoesNotUseDefaultHistoryStore(t *testing.T) {
	m0 := newTestModel()

	if _, ok := m0.opts.History.(*repl.FileHistoryStore); ok {
		t.Fatal("newTestModel should not write to the real repl history file")
	}
}

func TestTypingAtHostPrefixDoesNotRenderHostnameList(t *testing.T) {
	m0 := newTestModel()

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ed")})
	got := next.(model)

	if got.completionMessage != "" {
		t.Fatalf("completion message = %q", got.completionMessage)
	}
}

func TestRightArrowCompletesFaintTargetSuggestion(t *testing.T) {
	m0 := newTestModel()
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ed")})
	withSuggestions := next.(model)

	next, _ = withSuggestions.Update(tea.KeyMsg{Type: tea.KeyRight})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.completionMessage != "" {
		t.Fatalf("completion message = %q", got.completionMessage)
	}
}

func TestRightArrowCompletesTypedTargetSuggestion(t *testing.T) {
	m0 := newTestModel()
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ed")})
	withSuggestions := next.(model)

	next, _ = withSuggestions.Update(tea.KeyMsg{Type: tea.KeyRight})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
}

func TestPromptSyntaxCannotBeDeleted(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'edge01'")))

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q, want syntax preserved", got.input.Value())
	}

	got.input.SetCursor(0)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyDelete})
	got = next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q, want syntax preserved", got.input.Value())
	}
}

func TestPromptTypingOutsideQuotedFieldsIsIgnored(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.SetCursor(0)

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q, want syntax preserved", got.input.Value())
	}
}

func TestCursorCannotEnterCommandFieldUntilTargetIsFilled(t *testing.T) {
	m0 := newTestModel()

	for range 20 {
		next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRight})
		m0 = next.(model)
	}
	if m0.input.Position() != promptStarterCursor {
		t.Fatalf("cursor = %d, want target placeholder cursor", m0.input.Position())
	}

	m0.input.SetCursor(commandPromptCursor(m0.input.Value()))
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	got := next.(model)
	if got.input.Value() != promptStarter {
		t.Fatalf("input = %q, want command edit blocked until target is filled", got.input.Value())
	}
}

func TestCursorJumpsBetweenTargetAndCommandFields(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'edge01")))

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRight})
	got := next.(model)
	if got.input.Position() != commandPromptCursor(got.input.Value()) {
		t.Fatalf("right cursor = %d, want command cursor %d", got.input.Position(), commandPromptCursor(got.input.Value()))
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyLeft})
	got = next.(model)
	if got.input.Position() != len([]rune("[ 'edge01")) {
		t.Fatalf("left cursor = %d, want target cursor", got.input.Position())
	}
}

func TestCommaAfterTargetStartsNextQuotedTarget(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'edge01")))

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(",")})
	got := next.(model)

	if got.input.Value() != "[ 'edge01', '' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.input.Position() != len([]rune("[ 'edge01', '")) {
		t.Fatalf("cursor = %d, want inside new target", got.input.Position())
	}
}

func TestCommaAfterClosingTargetQuoteStartsNextQuotedTarget(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'edge01'")))

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(",")})
	got := next.(model)

	if got.input.Value() != "[ 'edge01', '' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.input.Position() != len([]rune("[ 'edge01', '")) {
		t.Fatalf("cursor = %d, want inside new target", got.input.Position())
	}
}

func TestTypingSecondManualTargetAfterComma(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge01' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'edge01")))

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(",edge02")})
	got := next.(model)

	if got.input.Value() != "[ 'edge01', 'edge02' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
}

func TestTabCompletesTargetGroupForExactHostnameMatch(t *testing.T) {
	m0 := newTestModel()
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("edge01")})
	withSuggestions := next.(model)

	next, _ = withSuggestions.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.picker.open {
		t.Fatal("picker should stay closed when completing target group")
	}
}

func TestTabCompletesFaintHostnameSuggestion(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'ed' ] ( '' )")
	m0.input.SetCursor(len([]rune("[ 'ed")))

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := next.(model)

	if got.input.Value() != "[ 'edge01' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.picker.open {
		t.Fatal("picker should stay closed when completing suggestion")
	}
	if got.completionMessage != "" {
		t.Fatalf("completion message = %q", got.completionMessage)
	}
}

func TestTabExpandsHostPatternWhenThereIsNoSuggestion(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'acm-lab-agg-sw(1,2)' ] ( '' )")
	m0.input.CursorEnd()

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := next.(model)

	if got.input.Value() != "[ 'acm-lab-agg-sw1', 'acm-lab-agg-sw2' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.picker.open {
		t.Fatal("picker should stay closed when expanding host pattern")
	}
}

func TestTabCompletesTargetGroupWhenThereIsNoSuggestion(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge99' ] ( '' )")
	m0.input.CursorEnd()

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := next.(model)

	if got.input.Value() != "[ 'edge99' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.picker.open {
		t.Fatal("picker should stay closed when completing target group")
	}
}

func TestPickerArrowKeysAndEnterSelectHostname(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge0' ] ( '' )")
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyUp})
	picking := next.(model)

	next, _ = picking.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	if moved.picker.selected != 1 {
		t.Fatalf("selected = %d, want 1", moved.picker.selected)
	}

	next, _ = moved.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.picker.open {
		t.Fatal("picker should close after selection")
	}
	if got.input.Value() != "[ 'edge02' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
}

func TestPickerSpaceTogglesCheckedHostsAndUpdatesPromptPreview(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge0' ] ( '' )")
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyUp})
	picking := next.(model)

	next, _ = picking.Update(tea.KeyMsg{Type: tea.KeySpace})
	checkedOne := next.(model)
	next, _ = checkedOne.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	next, _ = moved.Update(tea.KeyMsg{Type: tea.KeySpace})
	checkedTwo := next.(model)

	if checkedTwo.input.Value() != "[ 'edge0' ] ( '' )" {
		t.Fatalf("raw input = %q, want original prefix while picker is open", checkedTwo.input.Value())
	}
	if !strings.Contains(stripANSI(checkedTwo.pickerView()), "[x] edge01") ||
		!strings.Contains(stripANSI(checkedTwo.pickerView()), "[x] edge02") {
		t.Fatalf("picker view should show checked hosts:\n%s", stripANSI(checkedTwo.pickerView()))
	}
	if !strings.Contains(stripANSI(checkedTwo.promptInputView()), "[ 'edge01', 'edge02' ] ( '|' )") {
		t.Fatalf("prompt preview should include checked hosts: %q", stripANSI(checkedTwo.promptInputView()))
	}
}

func TestPickerEnterAcceptsCheckedHosts(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge0' ] ( '' )")
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyUp})
	picking := next.(model)

	next, _ = picking.Update(tea.KeyMsg{Type: tea.KeySpace})
	checkedOne := next.(model)
	next, _ = checkedOne.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	next, _ = moved.Update(tea.KeyMsg{Type: tea.KeySpace})
	checkedTwo := next.(model)
	next, _ = checkedTwo.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)

	if got.picker.open {
		t.Fatal("picker should close after accepting checked hosts")
	}
	if got.input.Value() != "[ 'edge01', 'edge02' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
}

func TestPickerSelectionUpdatesFaintTargetSuggestion(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'ed' ] ( '' )")
	m0.input.SetCursor(5)
	m0.picker = completionPicker{open: true, matches: []string{"edge01", "edge02"}, selected: 1}

	got := m0.promptInputView()

	if !strings.Contains(got, targetSuggestionStyle.Render("ge02")) {
		t.Fatalf("prompt input should show selected picker suggestion: %q", got)
	}
}

func TestPickerRightArrowSelectsHostname(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge0' ] ( '' )")
	m0.picker = completionPicker{open: true, matches: []string{"edge01", "edge02"}, selected: 1}

	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyRight})
	got := next.(model)

	if got.picker.open {
		t.Fatal("picker should close after right-arrow selection")
	}
	if got.input.Value() != "[ 'edge02' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
}

func TestPickerEscapeClosesWithoutChangingInput(t *testing.T) {
	m0 := newTestModel()
	m0.input.SetValue("[ 'edge0' ] ( '' )")
	next, _ := m0.Update(tea.KeyMsg{Type: tea.KeyUp})
	picking := next.(model)

	next, _ = picking.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if got.picker.open {
		t.Fatal("picker should close on escape")
	}
	if got.input.Value() != "[ 'edge0' ] ( '' )" {
		t.Fatalf("input = %q", got.input.Value())
	}
}

func TestPickerViewColorCodesTargetText(t *testing.T) {
	m0 := newTestModel()
	m0.picker = completionPicker{open: true, matches: []string{"edge01"}, selected: 0}

	got := m0.pickerView()

	if !strings.Contains(got, targetStyle.Render("edge01")) {
		t.Fatalf("picker view should color target text: %q", got)
	}
}

func newTestModel() model {
	return newTestModelWithHistory(nil)
}

func newTestModelWithResolver(resolver fakeResolver) model {
	return newTestModelWithResolverAndHistory(resolver, &fakeHistoryStore{})
}

func newTestModelWithResolverAndHistory(resolver fakeResolver, history repl.HistoryStore) model {
	runner := fakeRunner{}
	m := newModel(context.Background(), repl.Options{Concurrency: 2, Resolver: resolver, Runner: runner, History: history})
	m.width = 100
	m.height = 24
	return m
}

func newTestModelWithHistory(history repl.HistoryStore) model {
	if history == nil {
		history = &fakeHistoryStore{}
	}
	resolver := fakeResolver{
		hosts:       map[string]string{"edge1": "edge1", "edge2": "edge2", "edge01": "edge01"},
		suggestions: []string{"edge01", "edge02", "spine01"},
	}
	return newTestModelWithResolverAndHistory(resolver, history)
}

type fakeHistoryStore struct {
	lines    []string
	appended []string
}

func (s *fakeHistoryStore) Load() ([]string, error) {
	return append([]string(nil), s.lines...), nil
}

func (s *fakeHistoryStore) Append(line string) error {
	s.appended = append(s.appended, line)
	return nil
}

type fakeResolver struct {
	hosts       map[string]string
	suggestions []string
}

func (r fakeResolver) ResolveHost(host string) (string, error) {
	if resolved, ok := r.hosts[host]; ok {
		return resolved, nil
	}
	return host, nil
}

func (r fakeResolver) SelectHosts(selector string) ([]string, error) {
	return nil, nil
}

func (r fakeResolver) SuggestHosts(prefix string) ([]string, error) {
	var matched []string
	for _, host := range r.suggestions {
		if strings.HasPrefix(host, prefix) {
			matched = append(matched, host)
		}
	}
	return matched, nil
}

type fakeRunner struct{}

func (fakeRunner) RunCommand(ctx context.Context, host, command string) repl.CommandResult {
	return repl.CommandResult{Host: host, Command: command, Output: []byte(host + "\n")}
}
