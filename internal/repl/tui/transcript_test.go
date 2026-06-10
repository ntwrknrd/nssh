package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestRenderBufferSplitEmitsPaneSpans(t *testing.T) {
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\nleft-two\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\nright-two\n")}},
	}, 80)

	left, ok := buf.spanAt(1, 2)
	if !ok {
		t.Fatal("expected left body span")
	}
	if left.paneID != "left" || left.text != "left-one" {
		t.Fatalf("left span = %#v", left)
	}

	right, ok := buf.spanAt(1, 44)
	if !ok {
		t.Fatal("expected right body span")
	}
	if right.paneID != "right" || right.text != "right-one" {
		t.Fatalf("right span = %#v", right)
	}
	if left.blockID != right.blockID {
		t.Fatalf("split panes should share block ID: left=%d right=%d", left.blockID, right.blockID)
	}
}

func TestRenderBufferSplitGutterAndGapAreNotSelectable(t *testing.T) {
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\n")}},
	}, 80)

	if _, ok := buf.spanAt(1, 0); ok {
		t.Fatal("left gutter should not be selectable")
	}
	if _, ok := buf.spanAt(1, 38); ok {
		t.Fatal("split gap should not be selectable")
	}
	if _, ok := buf.spanAt(1, 40); ok {
		t.Fatal("split gap should not be selectable")
	}
	if _, ok := buf.spanAt(1, 42); ok {
		t.Fatal("right gutter should not be selectable")
	}
}

func TestRenderBufferUnevenSplitKeepsLaterRowsInPane(t *testing.T) {
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\nright-two\nright-three\n")}},
	}, 80)

	span, ok := buf.spanAt(3, 44)
	if !ok {
		t.Fatal("expected right pane span on uneven row")
	}
	if span.paneID != "right" || span.text != "right-three" {
		t.Fatalf("span = %#v", span)
	}
	if _, ok := buf.spanAt(3, 3); ok {
		t.Fatal("empty left pane row should not be selectable")
	}
}

func TestCanRenderResultSplitAllowsLongBodyLinesWhenPaneWidthIsSufficient(t *testing.T) {
	left := repl.CommandResult{Host: "edge01", Command: "show", Output: []byte(strings.Repeat("left", 30) + "\n")}
	right := repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right\n")}

	if !canRenderResultSplit(left, right, 80) {
		t.Fatal("long body lines should wrap instead of preventing split rendering")
	}
}

func TestCanRenderResultSplitRejectsNarrowTerminal(t *testing.T) {
	left := repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left\n")}
	right := repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right\n")}

	if canRenderResultSplit(left, right, 40) {
		t.Fatal("terminal too narrow for minimum panes should not split")
	}
}

func TestRenderBufferSplitWrapsLongLeftPaneLine(t *testing.T) {
	longLine := "abcdefghijklmnopqrstuvwxyz0123456789WRAP"
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte(longLine + "\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("short\n")}},
	}, 80)

	first, ok := buf.spanAt(1, 2)
	if !ok {
		t.Fatal("expected first wrapped left segment")
	}
	if first.paneID != "left" || first.text != "abcdefghijklmnopqrstuvwxyz0123456789" {
		t.Fatalf("first segment = %#v", first)
	}
	second, ok := buf.spanAt(2, 2)
	if !ok {
		t.Fatal("expected second wrapped left segment")
	}
	if second.paneID != "left" || second.text != "WRAP" {
		t.Fatalf("second segment = %#v", second)
	}
	if strings.TrimSpace(runeSlice(buf.lines[2].plain, 0, 2)) != "" {
		t.Fatalf("continuation gutter should be blank: %q", buf.lines[2].plain)
	}
	if _, ok := buf.spanAt(2, 44); ok {
		t.Fatal("short right pane should not create a second-row span")
	}
}

func TestRenderBufferSplitWrapsLongRightPaneLine(t *testing.T) {
	longLine := "abcdefghijklmnopqrstuvwxyz0123456789WRAP"
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("short\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte(longLine + "\n")}},
	}, 80)

	first, ok := buf.spanAt(1, 44)
	if !ok {
		t.Fatal("expected first wrapped right segment")
	}
	if first.paneID != "right" || first.text != "abcdefghijklmnopqrstuvwxyz0123456789" {
		t.Fatalf("first segment = %#v", first)
	}
	second, ok := buf.spanAt(2, 44)
	if !ok {
		t.Fatal("expected second wrapped right segment")
	}
	if second.paneID != "right" || second.text != "WRAP" {
		t.Fatalf("second segment = %#v", second)
	}
	if strings.TrimSpace(runeSlice(buf.lines[2].plain, 42, 44)) != "" {
		t.Fatalf("right continuation gutter should be blank: %q", buf.lines[2].plain)
	}
	if _, ok := buf.spanAt(2, 2); ok {
		t.Fatal("short left pane should not create a second-row span")
	}
}

func TestRenderBufferSplitWrapsLongLinesWithoutCrossingGap(t *testing.T) {
	longLine := "abcdefghijklmnopqrstuvwxyz0123456789WRAP"
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte(longLine + "\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte(longLine + "\n")}},
	}, 80)

	for line := 1; line <= 2; line++ {
		if _, ok := buf.spanAt(line, 38); ok {
			t.Fatalf("line %d split gap should not be selectable", line)
		}
		if _, ok := buf.spanAt(line, 40); ok {
			t.Fatalf("line %d split gap should not be selectable", line)
		}
	}
}

func TestRenderBufferSplitWrapsEqualLongLinesWithoutDiffHighlight(t *testing.T) {
	forceColorProfile(t)
	longLine := "abcdefghijklmnopqrstuvwxyz0123456789WRAP"
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte(longLine + "\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte(longLine + "\n")}},
	}, 80)

	if strings.Contains(buf.lines[1].ansi, diffLeftStyle.Render(padRight("abcdefghijklmnopqrstuvwxyz0123456789", 36))) ||
		strings.Contains(buf.lines[2].ansi, diffLeftStyle.Render(padRight("WRAP", 36))) {
		t.Fatalf("equal wrapped rows should not be highlighted: %q\n%q", buf.lines[1].ansi, buf.lines[2].ansi)
	}
}

func TestRenderBufferSplitWrapsChangedLongLinesWithDiffHighlight(t *testing.T) {
	forceColorProfile(t)
	leftLine := "abcdefghijklmnopqrstuvwxyz0123456789LEFT"
	rightLine := "abcdefghijklmnopqrstuvwxyz0123456789RGHT"
	buf := renderTranscriptBufferWithDiff([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte(leftLine + "\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte(rightLine + "\n")}},
	}, 80, true)

	if !strings.Contains(buf.lines[1].ansi, diffLeftStyle.Render(padRight("abcdefghijklmnopqrstuvwxyz0123456789", 36))) ||
		!strings.Contains(buf.lines[2].ansi, diffLeftStyle.Render(padRight("LEFT", 36))) {
		t.Fatalf("changed wrapped left rows should be highlighted: %q\n%q", buf.lines[1].ansi, buf.lines[2].ansi)
	}
	if !strings.Contains(buf.lines[1].ansi, diffRightStyle.Render(padRight("abcdefghijklmnopqrstuvwxyz0123456789", 36))) ||
		!strings.Contains(buf.lines[2].ansi, diffRightStyle.Render(padRight("RGHT", 36))) {
		t.Fatalf("changed wrapped right rows should be highlighted: %q\n%q", buf.lines[1].ansi, buf.lines[2].ansi)
	}
}

func TestRenderBufferSplitDiffHighlightDisabledByDefault(t *testing.T) {
	forceColorProfile(t)
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("same\nleft\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("same\nright\n")}},
	}, 80)

	if strings.Contains(buf.lines[2].ansi, diffLeftStyle.Render(padRight("left", 36))) ||
		strings.Contains(buf.lines[2].ansi, diffRightStyle.Render(padRight("right", 36))) {
		t.Fatalf("diff highlight should be disabled by default: %q", buf.lines[2].ansi)
	}
}

func TestSelectedTextFromWrappedSplitSpansCopiesBodyOnly(t *testing.T) {
	longLine := "abcdefghijklmnopqrstuvwxyz0123456789WRAP"
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte(longLine + "\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("short\n")}},
	}, 80)
	span, ok := buf.spanAt(1, 2)
	if !ok {
		t.Fatal("expected wrapped left span")
	}
	selected := selectedTextFromSpans(buf, selectionState{
		has:     true,
		blockID: span.blockID,
		paneID:  span.paneID,
		anchor:  selectionPoint{line: 1, col: span.startCol},
		cursor:  selectionPoint{line: 2, col: 80},
	})

	if selected != "abcdefghijklmnopqrstuvwxyz0123456789\nWRAP" {
		t.Fatalf("selected text = %q", selected)
	}
}

func TestSelectedTextFromSpansDoesNotCrossPanes(t *testing.T) {
	buf := renderTranscriptBuffer([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("left-one\nleft-two\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("right-one\nright-two\n")}},
	}, 80)
	span, ok := buf.spanAt(1, 2)
	if !ok {
		t.Fatal("expected left pane span")
	}
	selection := selectionState{
		has:     true,
		blockID: span.blockID,
		paneID:  span.paneID,
		anchor:  selectionPoint{line: 1, col: 2},
		cursor:  selectionPoint{line: 2, col: 79},
	}

	selected := selectedTextFromSpans(buf, selection)
	if selected != "left-one\nleft-two" {
		t.Fatalf("selected text = %q", selected)
	}
	if strings.Contains(selected, "right") {
		t.Fatalf("selection crossed pane: %q", selected)
	}
}

func TestRenderBufferSplitDiffColorsLeftOnlyBodyText(t *testing.T) {
	forceColorProfile(t)
	buf := renderTranscriptBufferWithDiff([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("same\nleft-only\ntail\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("same\ntail\n")}},
	}, 80, true)

	line := buf.lines[2].ansi
	if !strings.Contains(line, lineNumberStyle.Render("2 ")+diffLeftStyle.Render(padRight("left-only", 36))) {
		t.Fatalf("left-only line missing dim gutter plus highlighted body: %q", line)
	}
	if strings.Contains(line, diffLeftStyle.Render("2 ")) {
		t.Fatalf("left gutter should not be diff-colored: %q", line)
	}
}

func TestRenderBufferSplitDiffColorsRightOnlyBodyText(t *testing.T) {
	forceColorProfile(t)
	buf := renderTranscriptBufferWithDiff([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("same\ntail\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("same\nright-only\ntail\n")}},
	}, 80, true)

	line := buf.lines[2].ansi
	if !strings.Contains(line, lineNumberStyle.Render("2 ")+diffRightStyle.Render(padRight("right-only", 36))) {
		t.Fatalf("right-only line missing dim gutter plus highlighted body: %q", line)
	}
	if strings.Contains(line, diffRightStyle.Render("2 ")) {
		t.Fatalf("right gutter should not be diff-colored: %q", line)
	}
}

func TestRenderBufferSplitDiffColorsChangedBodyText(t *testing.T) {
	forceColorProfile(t)
	buf := renderTranscriptBufferWithDiff([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("same\nleft\ntail\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("same\nright\ntail\n")}},
	}, 80, true)

	line := buf.lines[2].ansi
	if !strings.Contains(line, diffLeftStyle.Render(padRight("left", 36))) {
		t.Fatalf("changed left text should be highlighted: %q", line)
	}
	if !strings.Contains(line, diffRightStyle.Render(padRight("right", 36))) {
		t.Fatalf("changed right text should be highlighted: %q", line)
	}
}

func TestRenderBufferSplitDiffLeavesEqualRowsUncolored(t *testing.T) {
	forceColorProfile(t)
	buf := renderTranscriptBufferWithDiff([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("same\nleft\ntail\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("same\nright\ntail\n")}},
	}, 80, true)

	line := buf.lines[1].ansi
	if strings.Contains(line, diffLeftStyle.Render("same")) || strings.Contains(line, diffRightStyle.Render("same")) {
		t.Fatalf("equal row should not be diff-colored: %q", line)
	}
}

func TestRenderBufferSplitDiffSelectionCopiesPlainBodyText(t *testing.T) {
	forceColorProfile(t)
	buf := renderTranscriptBufferWithDiff([]transcriptBlock{
		{result: &repl.CommandResult{Host: "edge01", Command: "show", Output: []byte("same\nleft-only\ntail\n")}},
		{result: &repl.CommandResult{Host: "edge02", Command: "show", Output: []byte("same\ntail\n")}},
	}, 80, true)
	span, ok := buf.spanAt(2, 2)
	if !ok {
		t.Fatal("expected left diff span")
	}
	selected := selectedTextFromSpans(buf, selectionState{
		has:     true,
		blockID: span.blockID,
		paneID:  span.paneID,
		anchor:  selectionPoint{line: 2, col: span.startCol},
		cursor:  selectionPoint{line: 2, col: span.endCol},
	})

	if selected != "left-only" {
		t.Fatalf("selected text = %q, want plain diff body", selected)
	}
}

func TestSplitDiffStylesUseBackgroundHighlight(t *testing.T) {
	if diffLeftStyle.GetBackground() != diffLeftBackgroundColor {
		t.Fatal("left diff style should use a background highlight")
	}
	if diffRightStyle.GetBackground() != diffRightBackgroundColor {
		t.Fatal("right diff style should use a background highlight")
	}
	if diffLeftStyle.GetBackground() == lipgloss.Color("52") {
		t.Fatal("left diff background should not use saturated ANSI red")
	}
	if diffRightStyle.GetBackground() == lipgloss.Color("22") {
		t.Fatal("right diff background should not use saturated ANSI green")
	}
	if diffLeftStyle.GetForeground() == ui.ColorRed {
		t.Fatal("left diff style should not rely on red foreground text")
	}
	if diffRightStyle.GetForeground() == ui.ColorGreen {
		t.Fatal("right diff style should not rely on green foreground text")
	}
}

func forceColorProfile(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(previous)
	})
}
