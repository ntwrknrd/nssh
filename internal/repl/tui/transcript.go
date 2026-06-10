package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ntwrknrd/nssh/internal/repl"
)

type renderBuffer struct {
	lines []renderLine
	text  string
}

type renderLine struct {
	plain string
	ansi  string
	spans []selectableSpan
}

type selectableSpan struct {
	blockID  int
	paneID   string
	startCol int
	endCol   int
	text     string
}

func renderTranscriptBuffer(blocks []transcriptBlock, width int) renderBuffer {
	return renderTranscriptBufferWithDiff(blocks, width, false)
}

func renderTranscriptBufferWithDiff(blocks []transcriptBlock, width int, diffEnabled bool) renderBuffer {
	if width <= 0 {
		width = 80
	}
	var lines []renderLine
	blockID := 0
	for i := 0; i < len(blocks); i++ {
		block := blocks[i]
		var blockLines []renderLine
		if block.result == nil {
			blockLines = renderTextBlock(block.text, blockID)
		} else if i+1 < len(blocks) && blocks[i+1].result != nil &&
			canRenderResultSplit(*block.result, *blocks[i+1].result, width) {
			blockLines = renderResultSplitBuffer(*block.result, *blocks[i+1].result, width, blockID, diffEnabled)
			i++
		} else {
			blockLines = renderResultBlockBuffer(*block.result, width, blockID)
		}
		if len(nonEmptyRenderLines(blockLines)) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, renderLine{})
		}
		lines = append(lines, blockLines...)
		blockID++
	}
	return renderBufferFromLines(lines)
}

func renderBufferFromLines(lines []renderLine) renderBuffer {
	textLines := make([]string, len(lines))
	for i, line := range lines {
		textLines[i] = line.ansi
	}
	return renderBuffer{lines: lines, text: strings.Join(textLines, "\n")}
}

func nonEmptyRenderLines(lines []renderLine) []renderLine {
	out := make([]renderLine, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.plain) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func renderTextBlock(text string, blockID int) []renderLine {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	rawLines := strings.Split(text, "\n")
	lines := make([]renderLine, 0, len(rawLines))
	for _, raw := range rawLines {
		lines = append(lines, fullWidthRenderLine(raw, blockID, ""))
	}
	return lines
}

func renderResultBlockBuffer(result repl.CommandResult, width, blockID int) []renderLine {
	var lines []renderLine
	lines = append(lines, fullWidthRenderLine(repl.RenderOutputBannerForWidth(result.Host, result.Command, width), blockID, ""))
	for _, line := range resultBodyLines(result) {
		lines = append(lines, fullWidthRenderLine(line, blockID, ""))
	}
	return lines
}

func renderResultSplitBuffer(left, right repl.CommandResult, width, blockID int, diffEnabled bool) []renderLine {
	leftWidth := (width - splitGapWidth) / 2
	rightWidth := width - splitGapWidth - leftWidth
	rightStart := leftWidth + splitGapWidth
	leftBody := resultBodyLines(left)
	rightBody := resultBodyLines(right)
	diffRows := buildIndexAlignedSplitDiffRows(leftBody, rightBody)
	if diffEnabled {
		diffRows = buildSplitDiffRows(leftBody, rightBody)
	}
	leftGutterWidth := len(fmt.Sprintf("%d", max(1, len(leftBody))))
	rightGutterWidth := len(fmt.Sprintf("%d", max(1, len(rightBody))))
	leftBodyWidth := max(1, leftWidth-leftGutterWidth-1)
	rightBodyWidth := max(1, rightWidth-rightGutterWidth-1)
	gap := strings.Repeat(" ", splitGapWidth)
	leftHeader := paneRenderLine(repl.RenderOutputBannerForWidth(left.Host, left.Command, leftWidth), blockID, "left", 0)
	rightHeader := shiftRenderLine(paneRenderLine(repl.RenderOutputBannerForWidth(right.Host, right.Command, rightWidth), blockID, "right", 0), rightStart)
	lines := []renderLine{joinSplitRenderLines(leftHeader, rightHeader, leftWidth, gap)}
	for _, row := range diffRows {
		leftLines := renderSplitWrappedSide(row, "left", leftGutterWidth, leftBodyWidth, blockID, diffEnabled)
		rightLines := renderSplitWrappedSide(row, "right", rightGutterWidth, rightBodyWidth, blockID, diffEnabled)
		height := max(len(leftLines), len(rightLines))
		for i := 0; i < height; i++ {
			var leftLine renderLine
			if i < len(leftLines) {
				leftLine = leftLines[i]
			}
			var rightLine renderLine
			if i < len(rightLines) {
				rightLine = shiftRenderLine(rightLines[i], rightStart)
			}
			lines = append(lines, joinSplitRenderLines(leftLine, rightLine, leftWidth, gap))
		}
	}
	return lines
}

func joinSplitRenderLines(leftLine, rightLine renderLine, leftWidth int, gap string) renderLine {
	return renderLine{
		plain: padRight(leftLine.plain, leftWidth) + gap + rightLine.plain,
		ansi:  padRight(leftLine.ansi, leftWidth) + gap + rightLine.ansi,
		spans: append(append([]selectableSpan{}, leftLine.spans...), rightLine.spans...),
	}
}

func renderSplitWrappedSide(row splitDiffRow, paneID string, gutterWidth, bodyWidth, blockID int, diffEnabled bool) []renderLine {
	lineNo, line, kind := splitDiffRowSide(row, paneID)
	if lineNo == 0 && line == "" {
		return nil
	}
	segments := wrapLine(line, bodyWidth)
	lines := make([]renderLine, 0, len(segments))
	for i, segment := range segments {
		segmentLineNo := lineNo
		if i > 0 {
			segmentLineNo = 0
		}
		lines = append(lines, renderSplitBodyLine(segmentLineNo, segment, kind, gutterWidth, bodyWidth, blockID, paneID, diffEnabled))
	}
	return lines
}

func renderSplitBodyLine(lineNo int, bodyText string, kind diffKind, gutterWidth, bodyWidth, blockID int, paneID string, diffEnabled bool) renderLine {
	gutter := strings.Repeat(" ", gutterWidth+1)
	if lineNo > 0 {
		gutter = fmt.Sprintf("%*d ", gutterWidth, lineNo)
	}
	plain := gutter + bodyText
	ansi := lineNumberStyle.Render(gutter) + styleDiffBody(paneID, kind, bodyText, bodyWidth, diffEnabled)
	spans := []selectableSpan{}
	if bodyText != "" {
		spans = append(spans, selectableSpan{
			blockID:  blockID,
			paneID:   paneID,
			startCol: gutterWidth + 1,
			endCol:   gutterWidth + 1 + lipgloss.Width(bodyText),
			text:     bodyText,
		})
	}
	return renderLine{plain: plain, ansi: ansi, spans: spans}
}

func wrapLine(s string, width int) []string {
	if width <= 0 || s == "" {
		return []string{s}
	}
	var lines []string
	var b strings.Builder
	current := 0
	for _, r := range s {
		w := max(1, lipgloss.Width(string(r)))
		if current > 0 && current+w > width {
			lines = append(lines, b.String())
			b.Reset()
			current = 0
		}
		b.WriteRune(r)
		current += w
	}
	lines = append(lines, b.String())
	return lines
}

func splitDiffRowSide(row splitDiffRow, paneID string) (int, string, diffKind) {
	if paneID == "right" {
		return row.rightLineNo, row.rightText, row.rightKind
	}
	return row.leftLineNo, row.leftText, row.leftKind
}

func styleDiffBody(paneID string, kind diffKind, text string, width int, diffEnabled bool) string {
	if text == "" {
		return ""
	}
	padded := padRight(text, width)
	if !diffEnabled {
		return padded
	}
	if paneID == "left" && (kind == diffLeftOnly || kind == diffChanged) {
		return diffLeftStyle.Render(padded)
	}
	if paneID == "right" && (kind == diffRightOnly || kind == diffChanged) {
		return diffRightStyle.Render(padded)
	}
	return text
}

func fullWidthRenderLine(ansi string, blockID int, paneID string) renderLine {
	plain := stripANSICodes(ansi)
	return paneRenderLineWithPlain(plain, ansi, blockID, paneID, 0)
}

func paneRenderLine(ansi string, blockID int, paneID string, startCol int) renderLine {
	plain := stripANSICodes(ansi)
	return paneRenderLineWithPlain(plain, ansi, blockID, paneID, startCol)
}

func paneRenderLineWithPlain(plain, ansi string, blockID int, paneID string, startCol int) renderLine {
	spans := []selectableSpan{}
	if plain != "" {
		spans = append(spans, selectableSpan{
			blockID:  blockID,
			paneID:   paneID,
			startCol: startCol,
			endCol:   startCol + lipgloss.Width(plain),
			text:     plain,
		})
	}
	return renderLine{plain: plain, ansi: ansi, spans: spans}
}

func shiftRenderLine(line renderLine, offset int) renderLine {
	if len(line.spans) == 0 {
		return line
	}
	spans := make([]selectableSpan, len(line.spans))
	for i, span := range line.spans {
		span.startCol += offset
		span.endCol += offset
		spans[i] = span
	}
	line.spans = spans
	return line
}

func (b renderBuffer) spanAt(line, col int) (selectableSpan, bool) {
	if line < 0 || line >= len(b.lines) {
		return selectableSpan{}, false
	}
	for _, span := range b.lines[line].spans {
		if col >= span.startCol && col < span.endCol {
			return span, true
		}
	}
	return selectableSpan{}, false
}

func (b renderBuffer) nearestSpan(line, col int) (selectableSpan, bool) {
	if span, ok := b.spanAt(line, col); ok {
		return span, true
	}
	if line < 0 || line >= len(b.lines) {
		return selectableSpan{}, false
	}
	for _, span := range b.lines[line].spans {
		if col < span.startCol && span.startCol-col <= maxSelectableGutterWidth {
			return span, true
		}
	}
	return selectableSpan{}, false
}

func (b renderBuffer) spanForPoint(line, col int) (selectableSpan, selectionPoint, bool) {
	if span, ok := b.spanAt(line, col); ok {
		return span, selectionPoint{line: line, col: col}, true
	}
	if line < 0 || line >= len(b.lines) {
		return selectableSpan{}, selectionPoint{}, false
	}
	for _, span := range b.lines[line].spans {
		if col < span.startCol && span.startCol-col <= maxSelectableGutterWidth {
			return span, selectionPoint{line: line, col: span.startCol}, true
		}
	}
	return selectableSpan{}, selectionPoint{}, false
}

func selectedTextFromSpans(buffer renderBuffer, selection selectionState) string {
	if !selection.has {
		return ""
	}
	start, end := normalizedSelection(selection)
	if start.line < 0 || end.line < 0 || start.line >= len(buffer.lines) {
		return ""
	}
	if end.line >= len(buffer.lines) {
		end.line = len(buffer.lines) - 1
	}
	var selected []string
	for lineIndex := start.line; lineIndex <= end.line; lineIndex++ {
		var lineText strings.Builder
		for _, span := range buffer.lines[lineIndex].spans {
			if span.blockID != selection.blockID || span.paneID != selection.paneID {
				continue
			}
			startCol := span.startCol
			endCol := span.endCol
			if lineIndex == start.line {
				startCol = max(startCol, start.col)
			}
			if lineIndex == end.line {
				endCol = min(endCol, end.col)
			}
			if endCol <= startCol {
				continue
			}
			lineText.WriteString(textByDisplayColumns(span.text, startCol-span.startCol, endCol-span.startCol))
		}
		if lineText.Len() > 0 {
			selected = append(selected, strings.TrimRight(lineText.String(), " "))
		}
	}
	return strings.Join(selected, "\n")
}

func renderSelectedTranscript(transcript string, selection selectionState) string {
	return renderBufferWithSelection(renderPlainTranscriptBuffer(transcript), selection)
}

func renderPlainTranscriptBuffer(transcript string) renderBuffer {
	if transcript == "" {
		return renderBuffer{}
	}
	rawLines := strings.Split(transcript, "\n")
	lines := make([]renderLine, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, fullWidthRenderLine(line, 0, ""))
	}
	return renderBufferFromLines(lines)
}

func renderBufferWithSelection(buffer renderBuffer, selection selectionState) string {
	if !selection.has {
		return buffer.text
	}
	start, end := normalizedSelection(selection)
	lines := make([]string, len(buffer.lines))
	for i, line := range buffer.lines {
		lines[i] = line.ansi
	}
	for lineIndex := start.line; lineIndex <= end.line && lineIndex < len(buffer.lines); lineIndex++ {
		if lineIndex < 0 {
			continue
		}
		for _, span := range buffer.lines[lineIndex].spans {
			if span.blockID != selection.blockID || span.paneID != selection.paneID {
				continue
			}
			startCol := span.startCol
			endCol := span.endCol
			if lineIndex == start.line {
				startCol = max(startCol, start.col)
			}
			if lineIndex == end.line {
				endCol = min(endCol, end.col)
			}
			if endCol > startCol {
				lines[lineIndex] = renderSelectedLine(lines[lineIndex], startCol, endCol)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func textByDisplayColumns(s string, startCol, endCol int) string {
	if endCol <= startCol {
		return ""
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		w := max(1, lipgloss.Width(string(r)))
		next := col + w
		if next > startCol && col < endCol {
			b.WriteRune(r)
		}
		col = next
		if col >= endCol {
			break
		}
	}
	return b.String()
}

const maxSelectableGutterWidth = 4
