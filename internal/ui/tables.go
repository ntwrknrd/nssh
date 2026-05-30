package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mattn/go-runewidth"
)

// Table wraps table data for consistent styling.
type Table struct {
	headers    []string
	rows       [][]string
	footerRows [][]string // footer rows rendered with separator border
}

// StreamTable renders rows as they are added. Column widths must be known up
// front because already-printed rows cannot be resized.
type StreamTable struct {
	headers        []string
	preferredWidth []int
	widths         []int
	writer         io.Writer
	margin         int
	started        bool
}

// Truncate shortens a string to maxWidth display width, adding ellipsis if needed.
// Handles multi-byte characters correctly using runewidth.
func Truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return s[:maxWidth]
	}

	// Truncate to maxWidth-3 to leave room for "..."
	targetWidth := maxWidth - 3
	var result []rune
	currentWidth := 0

	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if currentWidth+rw > targetWidth {
			break
		}
		result = append(result, r)
		currentWidth += rw
	}

	return string(result) + "..."
}

// NewTable creates a new table with the given headers.
func NewTable(headers ...string) *Table {
	return &Table{
		headers: headers,
		rows:    make([][]string, 0),
	}
}

// NewStreamTable creates a table that writes each row immediately.
func NewStreamTable(headers ...string) *StreamTable {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = runewidth.StringWidth(header)
	}
	return &StreamTable{
		headers:        headers,
		preferredWidth: widths,
		writer:         os.Stdout,
	}
}

// WithColumnWidths sets preferred content widths for each column.
func (t *StreamTable) WithColumnWidths(widths ...int) *StreamTable {
	for i := range t.preferredWidth {
		if i < len(widths) && widths[i] > t.preferredWidth[i] {
			t.preferredWidth[i] = widths[i]
		}
	}
	return t
}

// WithWriter directs table output to writer. It is mainly useful in tests.
func (t *StreamTable) WithWriter(writer io.Writer) *StreamTable {
	if writer != nil {
		t.writer = writer
	}
	return t
}

// AddRow writes a row immediately, starting the table if needed.
func (t *StreamTable) AddRow(cells ...string) {
	if !t.started {
		t.start()
	}
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	_, _ = fmt.Fprintln(t.writer, t.prefix()+t.renderRow(row, false))
}

// Close writes the bottom border if the table was started.
func (t *StreamTable) Close() {
	if !t.started {
		return
	}
	_, _ = fmt.Fprintln(t.writer, t.prefix()+t.renderBorder("╰", "┴", "╯"))
	t.started = false
}

func (t *StreamTable) start() {
	termW := termWidth()
	t.widths = fitStreamColumnWidths(t.headers, t.preferredWidth, termW)
	tableWidth := streamTableWidth(t.widths)
	t.margin = (termW - tableWidth) / 2
	if t.margin < 0 {
		t.margin = 0
	}
	_, _ = fmt.Fprintln(t.writer, t.prefix()+t.renderBorder("╭", "┬", "╮"))
	_, _ = fmt.Fprintln(t.writer, t.prefix()+t.renderRow(t.headers, true))
	_, _ = fmt.Fprintln(t.writer, t.prefix()+t.renderBorder("├", "┼", "┤"))
	t.started = true
}

func (t *StreamTable) prefix() string {
	if t.margin <= 0 {
		return ""
	}
	return strings.Repeat(" ", t.margin)
}

func (t *StreamTable) renderBorder(left, middle, right string) string {
	borderStyle := lipgloss.NewStyle().Foreground(ColorDim)
	var sb strings.Builder
	sb.WriteString(borderStyle.Render(left))
	for i, width := range t.widths {
		sb.WriteString(borderStyle.Render(strings.Repeat("─", width+2)))
		if i == len(t.widths)-1 {
			sb.WriteString(borderStyle.Render(right))
		} else {
			sb.WriteString(borderStyle.Render(middle))
		}
	}
	return sb.String()
}

func (t *StreamTable) renderRow(cells []string, header bool) string {
	borderStyle := lipgloss.NewStyle().Foreground(ColorDim)
	cellStyle := lipgloss.NewStyle().Foreground(ColorWhite)
	if header {
		cellStyle = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	}
	var sb strings.Builder
	sb.WriteString(borderStyle.Render("│"))
	for i, width := range t.widths {
		cell := ""
		if i < len(cells) {
			cell = Truncate(cells[i], width)
		}
		padding := width - runewidth.StringWidth(cell)
		if padding < 0 {
			padding = 0
		}
		sb.WriteString(" ")
		sb.WriteString(cellStyle.Render(cell))
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(" ")
		sb.WriteString(borderStyle.Render("│"))
	}
	return sb.String()
}

func fitStreamColumnWidths(headers []string, preferred []int, termW int) []int {
	widths := make([]int, len(headers))
	minWidths := make([]int, len(headers))
	for i, header := range headers {
		headerWidth := runewidth.StringWidth(header)
		minWidths[i] = headerWidth
		widths[i] = headerWidth
		if i < len(preferred) && preferred[i] > widths[i] {
			widths[i] = preferred[i]
		}
	}

	available := termW - streamTableOverhead(len(widths))
	if available <= 0 {
		return widths
	}
	for sumInts(widths) > available {
		shrinkIdx := -1
		for i, width := range widths {
			if width <= minWidths[i] {
				continue
			}
			if shrinkIdx == -1 || width > widths[shrinkIdx] {
				shrinkIdx = i
			}
		}
		if shrinkIdx == -1 {
			break
		}
		widths[shrinkIdx]--
	}
	return widths
}

func streamTableWidth(widths []int) int {
	return sumInts(widths) + streamTableOverhead(len(widths))
}

func streamTableOverhead(cols int) int {
	return cols*2 + cols + 1
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cells ...string) {
	// Pad or truncate to match header count
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.rows = append(t.rows, row)
}

// AddFooterRow adds a row that will be styled as a footer with a separator border.
func (t *Table) AddFooterRow(cells ...string) {
	// Pad or truncate to match header count
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.footerRows = append(t.footerRows, row)
}

// Render prints the table to stdout (centered) and returns the left margin used for centering.
func (t *Table) Render() int {
	rendered, margin := t.renderString()
	if rendered != "" {
		fmt.Println(rendered)
	}
	return margin
}

// LeftMargin returns the left margin that would be used for centering without printing.
func (t *Table) LeftMargin() int {
	_, margin := t.renderString()
	return margin
}

// renderString renders the table and returns the centered string and left margin.
func (t *Table) renderString() (string, int) {
	if len(t.headers) == 0 {
		return "", 0
	}

	termW := termWidth()
	numCols := len(t.headers)

	// Calculate natural width of each column (max of header, data rows, and footer rows)
	colWidths := make([]int, numCols)
	for i, h := range t.headers {
		colWidths[i] = runewidth.StringWidth(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < numCols {
				w := runewidth.StringWidth(cell)
				if w > colWidths[i] {
					colWidths[i] = w
				}
			}
		}
	}
	for _, row := range t.footerRows {
		for i, cell := range row {
			if i < numCols {
				w := runewidth.StringWidth(cell)
				if w > colWidths[i] {
					colWidths[i] = w
				}
			}
		}
	}

	// Calculate overhead: borders (numCols+1) + padding (2 per cell)
	borderOverhead := numCols + 1
	paddingOverhead := numCols * 2
	totalOverhead := borderOverhead + paddingOverhead

	// Calculate total natural width
	totalNatural := 0
	for _, w := range colWidths {
		totalNatural += w
	}

	// Truncate columns if table would exceed terminal width
	rows := t.rows
	footerRows := t.footerRows
	headers := t.headers
	var targetWidths []int
	if totalNatural+totalOverhead > termW {
		availableWidth := termW - totalOverhead
		if availableWidth < numCols*3 { // Minimum 3 chars per column
			availableWidth = numCols * 3
		}

		excess := totalNatural - availableWidth

		// Calculate header widths as minimum (don't truncate headers if possible)
		headerWidths := make([]int, numCols)
		for i, h := range t.headers {
			headerWidths[i] = runewidth.StringWidth(h)
		}

		// Calculate target widths: shrink large columns first, preserve small ones
		// Small columns (<=10 chars or header width) keep their size; larger ones shrink proportionally
		targetWidths = make([]int, numCols)
		smallColsWidth := 0
		largeColsWidth := 0
		for i, w := range colWidths {
			minWidth := headerWidths[i]
			if minWidth < 10 {
				minWidth = 10
			}
			if w <= minWidth {
				targetWidths[i] = w
				smallColsWidth += w
			} else {
				largeColsWidth += w
			}
		}

		// Distribute remaining space among large columns
		remainingWidth := availableWidth - smallColsWidth
		if remainingWidth < 0 {
			remainingWidth = numCols * 3 // Fallback minimum
		}

		for i, w := range colWidths {
			minWidth := headerWidths[i]
			if minWidth < 10 {
				minWidth = 10
			}
			if w > minWidth {
				if largeColsWidth > 0 {
					target := w * remainingWidth / largeColsWidth
					// Don't shrink below header width
					if target < headerWidths[i] {
						target = headerWidths[i]
					}
					if target < 8 {
						target = 8 // Absolute minimum
					}
					targetWidths[i] = target
				} else {
					targetWidths[i] = w
				}
			}
		}

		// If we still have excess, shrink only the large columns further
		newTotal := 0
		for _, w := range targetWidths {
			newTotal += w
		}
		if newTotal > availableWidth {
			// Calculate how much large columns need to shrink
			largeTotal := 0
			for i, w := range colWidths {
				minWidth := headerWidths[i]
				if minWidth < 10 {
					minWidth = 10
				}
				if w > minWidth {
					largeTotal += targetWidths[i]
				}
			}

			if largeTotal > 0 {
				shrinkNeeded := newTotal - availableWidth
				for i, w := range colWidths {
					minWidth := headerWidths[i]
					if minWidth < 10 {
						minWidth = 10
					}
					if w > minWidth {
						// Shrink proportionally to current target width
						shrink := targetWidths[i] * shrinkNeeded / largeTotal
						targetWidths[i] -= shrink
						// Don't shrink below header width
						if targetWidths[i] < headerWidths[i] {
							targetWidths[i] = headerWidths[i]
						}
						if targetWidths[i] < 8 {
							targetWidths[i] = 8
						}
					}
				}
			}
		}

		_ = excess // unused but documents intent

		// Truncate headers
		truncHeaders := make([]string, numCols)
		for i, h := range t.headers {
			truncHeaders[i] = Truncate(h, targetWidths[i])
		}
		headers = truncHeaders

		// Truncate rows
		truncRows := make([][]string, len(t.rows))
		for i, row := range t.rows {
			truncRow := make([]string, numCols)
			for j := range truncRow {
				if j < len(row) {
					truncRow[j] = Truncate(row[j], targetWidths[j])
				}
			}
			truncRows[i] = truncRow
		}
		rows = truncRows

		// Truncate footer rows
		truncFooterRows := make([][]string, len(t.footerRows))
		for i, row := range t.footerRows {
			truncRow := make([]string, numCols)
			for j := range truncRow {
				if j < len(row) {
					truncRow[j] = Truncate(row[j], targetWidths[j])
				}
			}
			truncFooterRows[i] = truncRow
		}
		footerRows = truncFooterRows
	}

	// Header style - cyan and bold
	headerStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true).
		Padding(0, 1)

	// Cell style - white text
	cellStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Padding(0, 1)

	// Border style
	borderStyle := lipgloss.NewStyle().Foreground(ColorDim)

	// Create the main table (without footer rows)
	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})

	rendered := tbl.String()

	// If we have footer rows, manipulate the output to add separator and footer
	if len(footerRows) > 0 {
		rendered = t.insertFooterRows(rendered, footerRows, borderStyle)
	}

	// Calculate centering
	tableWidth := lipgloss.Width(rendered)
	leftMargin := (termW - tableWidth) / 2
	if leftMargin < 0 {
		leftMargin = 0
	}

	// Center the table
	centered := lipgloss.PlaceHorizontal(termW, lipgloss.Center, rendered)

	return centered, leftMargin
}

// insertFooterRows manipulates the rendered table string to add a separator and footer rows.
func (t *Table) insertFooterRows(rendered string, footerRows [][]string, borderStyle lipgloss.Style) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		return rendered
	}

	// Find the bottom border line (last non-empty line)
	bottomIdx := len(lines) - 1
	for bottomIdx >= 0 && strings.TrimSpace(lines[bottomIdx]) == "" {
		bottomIdx--
	}
	if bottomIdx < 0 {
		return rendered
	}

	bottomBorder := lines[bottomIdx]

	// Create separator line by transforming bottom border characters
	// ╰ -> ├, ╯ -> ┤, ┴ -> ┼
	separator := strings.ReplaceAll(bottomBorder, "╰", "├")
	separator = strings.ReplaceAll(separator, "╯", "┤")
	separator = strings.ReplaceAll(separator, "┴", "┼")

	// Footer style - bold white (same as cells but emphasized)
	footerStyle := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Bold(true)

	// Description style - dimmed (last column)
	descStyle := lipgloss.NewStyle().
		Foreground(ColorGray)

	// Parse column widths from the separator line
	// The separator looks like: ├───────┼───────┼───────┤
	// We need to extract the widths between the separators
	colWidths := parseColumnWidths(separator)

	// Build footer row strings
	var footerLines []string
	for _, row := range footerRows {
		var cells []string
		for i, cell := range row {
			width := 0
			if i < len(colWidths) {
				width = colWidths[i]
			}
			// Pad cell to column width (accounting for 1 space padding on each side)
			contentWidth := width - 2
			if contentWidth < 0 {
				contentWidth = 0
			}
			// Calculate visible width of cell (without ANSI codes)
			cellWidth := runewidth.StringWidth(cell)
			padding := contentWidth - cellWidth
			if padding < 0 {
				padding = 0
			}
			// Use dim style for last column (description)
			style := footerStyle
			if i == len(row)-1 {
				style = descStyle
			}
			paddedCell := " " + style.Render(cell) + strings.Repeat(" ", padding+1)
			cells = append(cells, paddedCell)
		}
		// Join cells with border character
		rowLine := borderStyle.Render("│") + strings.Join(cells, borderStyle.Render("│")) + borderStyle.Render("│")
		footerLines = append(footerLines, rowLine)
	}

	// Reconstruct the table:
	// 1. All lines except the bottom border
	// 2. Separator line
	// 3. Footer rows
	// 4. Bottom border
	var result []string
	result = append(result, lines[:bottomIdx]...)
	result = append(result, separator)
	result = append(result, footerLines...)
	result = append(result, bottomBorder)

	return strings.Join(result, "\n")
}

// parseColumnWidths extracts column widths from a separator line like ├───────┼───────┼───────┤
func parseColumnWidths(separator string) []int {
	var widths []int
	currentWidth := 0
	inColumn := false

	for _, r := range separator {
		switch r {
		case '├', '┼':
			if inColumn && currentWidth > 0 {
				widths = append(widths, currentWidth)
			}
			currentWidth = 0
			inColumn = true
		case '┤':
			if currentWidth > 0 {
				widths = append(widths, currentWidth)
			}
		case '─':
			currentWidth++
		}
	}

	return widths
}

// TableHeader represents a column header with optional styling.
type TableHeader struct {
	Title string
	Color string // not used currently, kept for API compatibility
}

// BuildTable creates a table without printing it.
func BuildTable(headers []TableHeader, rows [][]string) *Table {
	headerTitles := make([]string, len(headers))
	for i, h := range headers {
		headerTitles[i] = h.Title
	}

	tbl := NewTable(headerTitles...)
	for _, row := range rows {
		tbl.AddRow(row...)
	}

	return tbl
}
