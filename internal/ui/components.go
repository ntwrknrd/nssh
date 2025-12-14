package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// termWidth returns the terminal width, defaulting to 80 if unknown.
func termWidth() int {
	if w, _, err := term.GetSize(0); err == nil && w > 0 {
		return w
	}
	return 80
}

// Ruler prints a horizontal line with optional centered title.
// Example: ──────────── HOST DETAILS ────────────
func Ruler(title string) string {
	width := termWidth()
	lineChar := "─"
	lineStyle := lipgloss.NewStyle().Foreground(ColorDim)
	titleStyle := lipgloss.NewStyle().Foreground(ColorWhite).Bold(true)

	if title == "" {
		return lineStyle.Render(strings.Repeat(lineChar, width))
	}

	// Title with padding
	paddedTitle := " " + title + " "
	titleLen := len(paddedTitle)

	// Calculate line lengths on each side
	remaining := width - titleLen
	if remaining < 2 {
		return titleStyle.Render(title)
	}

	leftLen := remaining / 2
	rightLen := remaining - leftLen

	left := lineStyle.Render(strings.Repeat(lineChar, leftLen))
	right := lineStyle.Render(strings.Repeat(lineChar, rightLen))

	return left + titleStyle.Render(paddedTitle) + right
}

// PrintRuler prints a ruler to stdout.
func PrintRuler(title string) {
	fmt.Println(Ruler(title))
}

// Section prints a section header (ruler with title).
func Section(title string) {
	PrintRuler(title)
	fmt.Println()
}

// SubSection prints a lightweight sub-section header with a mini ruler.
// Example:   ── Preview
// Pass skipNewline=true to omit the leading blank line.
func SubSection(title string, skipNewline ...bool) {
	lineStyle := lipgloss.NewStyle().Foreground(ColorDim)
	titleStyle := lipgloss.NewStyle().Foreground(ColorGray)
	if len(skipNewline) == 0 || !skipNewline[0] {
		fmt.Println()
	}
	fmt.Printf("  %s %s\n", lineStyle.Render("───"), titleStyle.Render(title))
}

// Panel displays key-value content within rulers.
// Top ruler has title, bottom ruler has optional footer (like "OK").
type Panel struct {
	title   string
	footer  string
	rows    []panelRow
	warning bool
}

type panelRow struct {
	label string
	value string
}

// NewPanel creates a new panel with title.
func NewPanel(title string) *Panel {
	return &Panel{title: title}
}

// WithFooter sets the footer text.
func (p *Panel) WithFooter(footer string) *Panel {
	p.footer = footer
	return p
}

// WithWarning makes the panel use warning colors.
func (p *Panel) WithWarning() *Panel {
	p.warning = true
	return p
}

// Row adds a key-value row.
func (p *Panel) Row(label, value string) *Panel {
	p.rows = append(p.rows, panelRow{label: label, value: value})
	return p
}

// Render returns the panel as a string.
func (p *Panel) Render() string {
	var sb strings.Builder

	// Top ruler
	sb.WriteString(Ruler(p.title))
	sb.WriteString("\n\n")

	// Find max label width for alignment
	maxLabel := 0
	for _, row := range p.rows {
		if len(row.label) > maxLabel {
			maxLabel = len(row.label)
		}
	}

	// Rows
	labelStyle := StyleLabel
	valueStyle := StyleCyan
	if p.warning {
		labelStyle = StyleRed.Bold(true)
		valueStyle = StyleYellow
	}

	for _, row := range p.rows {
		label := labelStyle.Render(fmt.Sprintf("%-*s", maxLabel, row.label))
		value := valueStyle.Render(row.value)
		sb.WriteString(label + " " + value + "\n")
	}

	// Bottom ruler
	sb.WriteString("\n")
	sb.WriteString(Ruler(p.footer))

	return sb.String()
}

// Print renders the panel to stdout.
func (p *Panel) Print() {
	fmt.Println(p.Render())
}

// Header prints a prominent header (cyan-colored, for top-level commands).
func Header(title string) {
	fmt.Println(coloredRuler(title, ColorCyan))
	fmt.Println()
}

// StatusType represents the type of status message for output formatting.
type StatusType int

// Status message types for formatted output.
const (
	StatusSuccess  StatusType = iota // Success status with checkmark
	StatusNoop                       // No operation performed
	StatusAbort                      // Operation aborted
	StatusInfo                       // Informational message
	StatusWarning                    // Warning message
	StatusError                      // Error message
	StatusDeletion                   // Item being deleted
	StatusCreation                   // Item being created
)

// statusConfig holds styling for each status type
type statusConfig struct {
	symbol string
	style  lipgloss.Style
}

var statusConfigs = map[StatusType]statusConfig{
	StatusSuccess:  {symbol: "✓", style: lipgloss.NewStyle().Foreground(ColorGreen).Faint(true)},
	StatusNoop:     {symbol: "-", style: lipgloss.NewStyle().Foreground(ColorGray).Faint(true)},
	StatusAbort:    {symbol: "!", style: lipgloss.NewStyle().Foreground(ColorYellow).Faint(true)},
	StatusInfo:     {symbol: "*", style: lipgloss.NewStyle().Foreground(ColorGray).Faint(true)},
	StatusWarning:  {symbol: "!", style: lipgloss.NewStyle().Foreground(ColorYellow).Faint(true)},
	StatusError:    {symbol: "✗", style: lipgloss.NewStyle().Foreground(ColorRed).Faint(true)},
	StatusDeletion: {symbol: "-", style: lipgloss.NewStyle().Foreground(ColorRed).Faint(true)},
	StatusCreation: {symbol: "+", style: lipgloss.NewStyle().Foreground(ColorGreen).Faint(true)},
}

func printStatus(st StatusType, format string, args ...any) {
	cfg := statusConfigs[st]
	bracket := cfg.style.Render("[" + cfg.symbol + "]")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("  %s %s\n", bracket, msg)
}

func printStatusCentered(st StatusType, format string, args ...any) {
	cfg := statusConfigs[st]
	bracket := cfg.style.Render("[" + cfg.symbol + "]")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s", bracket, msg)
	centered := lipgloss.PlaceHorizontal(termWidth(), lipgloss.Center, line)
	fmt.Println(centered)
}

func printStatusWithMargin(st StatusType, margin int, format string, args ...any) {
	cfg := statusConfigs[st]
	bracket := cfg.style.Render("[" + cfg.symbol + "]")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%*s%s %s\n", margin, "", bracket, msg)
}

// Success prints a success message.
func Success(format string, args ...any) {
	printStatus(StatusSuccess, format, args...)
}

// Noop prints a no-op message (nothing changed).
func Noop(format string, args ...any) {
	printStatus(StatusNoop, format, args...)
}

// Abort prints an abort message (user canceled).
func Abort(format string, args ...any) {
	printStatus(StatusAbort, format, args...)
}

// Info prints an info message.
func Info(format string, args ...any) {
	printStatus(StatusInfo, format, args...)
}

// InfoCentered prints a centered info message.
func InfoCentered(format string, args ...any) {
	printStatusCentered(StatusInfo, format, args...)
}

// InfoWithMargin prints an info message with a left margin.
func InfoWithMargin(margin int, format string, args ...any) {
	printStatusWithMargin(StatusInfo, margin, format, args...)
}

// Warning prints a warning message.
func Warning(format string, args ...any) {
	printStatus(StatusWarning, format, args...)
}

// WarningCentered prints a centered warning message.
func WarningCentered(format string, args ...any) {
	printStatusCentered(StatusWarning, format, args...)
}

// Error prints an error message.
func Error(format string, args ...any) {
	printStatus(StatusError, format, args...)
}

// Deletion prints a deletion message (item being removed).
func Deletion(format string, args ...any) {
	printStatus(StatusDeletion, format, args...)
}

// Creation prints a creation message (item being added).
func Creation(format string, args ...any) {
	printStatus(StatusCreation, format, args...)
}

// StatusLine prints an inline status with icon.
// ok=true shows green [✓], ok=false shows red [✗]
func StatusLine(ok bool, label, value string) {
	var icon string
	if ok {
		icon = DimGreen("[✓]")
	} else {
		icon = DimRed("[✗]")
	}
	fmt.Printf("  %s %s: %s\n", icon, label, Gray(value))
}

// StatusLineNeutral prints an inline status with neutral [-] icon (dimmed).
func StatusLineNeutral(label, value string) {
	style := statusConfigs[StatusInfo].style
	bracket := style.Render("[-]")
	fmt.Printf("  %s %s: %s\n", bracket, label, Gray(value))
}

// StatusLineNeutralText prints text with neutral [-] icon (dimmed), no label.
func StatusLineNeutralText(text string) {
	style := statusConfigs[StatusInfo].style
	bracket := style.Render("[-]")
	fmt.Printf("  %s %s\n", bracket, text)
}

// statusLabels maps StatusType to footer label text
var statusLabels = map[StatusType]string{
	StatusSuccess: "OK",
	StatusNoop:    "NO-OP",
	StatusAbort:   "ABORT",
	StatusWarning: "WARNING",
	StatusError:   "ERROR",
}

// statusColors maps StatusType to line color for rulers
var statusColors = map[StatusType]lipgloss.Color{
	StatusSuccess: ColorGreen,
	StatusNoop:    ColorGreen,
	StatusAbort:   ColorYellow,
	StatusWarning: ColorYellow,
	StatusError:   ColorRed,
}

// coloredRuler returns a ruler with dim lines and colored title.
func coloredRuler(title string, color lipgloss.Color) string {
	width := termWidth()
	lineChar := "─"
	lineStyle := lipgloss.NewStyle().Foreground(ColorDim)
	titleStyle := lipgloss.NewStyle().Foreground(color)

	if title == "" {
		return lineStyle.Render(strings.Repeat(lineChar, width))
	}

	// Title with padding
	paddedTitle := " " + title + " "
	titleLen := len(paddedTitle)

	// Calculate line lengths on each side
	remaining := width - titleLen
	if remaining < 2 {
		return titleStyle.Render(title)
	}

	leftLen := remaining / 2
	rightLen := remaining - leftLen

	left := lineStyle.Render(strings.Repeat(lineChar, leftLen))
	right := lineStyle.Render(strings.Repeat(lineChar, rightLen))

	return left + titleStyle.Render(paddedTitle) + right
}

// CommandStart prints the header banner for a command (cyan-colored).
func CommandStart(title string) {
	fmt.Println(coloredRuler(title, ColorCyan))
	fmt.Println()
}

// CommandEnd prints a status-colored footer banner.
func CommandEnd(status StatusType) {
	label := statusLabels[status]
	color := statusColors[status]
	fmt.Println()
	fmt.Println(coloredRuler(label, color))
}

// BatchGroup represents a group of items for batch display.
type BatchGroup struct {
	Name  string
	Items []BatchItem
}

// BatchItem represents an item in a batch operation.
type BatchItem struct {
	Name   string
	Detail string // optional additional info (e.g., hostname when different from alias)
}

// BatchPreview displays a pretty preview of batch operations grouped by context.
// action is "+" for add, "-" for remove
func BatchPreview(groups []BatchGroup, action string) {
	const maxItemsPerGroup = 8

	// Use softer colors - green for add, muted rose for remove
	actionStyle := StyleGreen
	treeStyle := lipgloss.NewStyle().Foreground(ColorGreen).Faint(true)
	if action == "-" || action == "remove" {
		actionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("174")) // soft rose
		treeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("174")).Faint(true)
	}

	for i, group := range groups {
		if i > 0 {
			fmt.Println()
		}

		// Context header with count
		count := len(group.Items)
		noun := "host"
		if count != 1 {
			noun = "hosts"
		}
		header := fmt.Sprintf("%s %s", StyleLabel.Render(group.Name), Gray(fmt.Sprintf("(%d %s)", count, noun)))
		fmt.Printf("  %s\n", header)

		// Determine how many items to show
		showCount := len(group.Items)
		truncated := false
		if showCount > maxItemsPerGroup {
			showCount = maxItemsPerGroup
			truncated = true
		}

		// Items with tree structure
		for j := 0; j < showCount; j++ {
			item := group.Items[j]
			// Tree characters
			var prefix string
			if j == showCount-1 && !truncated {
				prefix = "└─"
			} else {
				prefix = "├─"
			}

			// Format item - no separate +/- symbol, just colored text
			itemText := actionStyle.Render(item.Name)
			if item.Detail != "" {
				itemText += " " + Gray(item.Detail)
			}

			fmt.Printf("  %s %s\n", treeStyle.Render(prefix), itemText)
		}

		// Show truncation indicator
		if truncated {
			remaining := len(group.Items) - maxItemsPerGroup
			fmt.Printf("  %s %s\n", treeStyle.Render("└─"), Gray(fmt.Sprintf("... and %d more", remaining)))
		}
	}
}

// BatchSummaryLine prints a summary line for batch operations.
// Example: "  6 hosts across 2 contexts"
func BatchSummaryLine(totalItems, groupCount int, itemNoun string) {
	if itemNoun == "" {
		itemNoun = "host"
	}
	if totalItems != 1 {
		itemNoun += "s"
	}

	groupNoun := "context"
	if groupCount != 1 {
		groupNoun += "s"
	}

	summary := fmt.Sprintf("%d %s across %d %s", totalItems, itemNoun, groupCount, groupNoun)
	fmt.Printf("\n  %s\n", Gray(summary))
}

// BatchProgress tracks and displays progress of batch operations.
type BatchProgress struct {
	items []batchProgressItem
	width int
}

type batchProgressItem struct {
	name   string
	status string // "pending", "success", "error"
}

// NewBatchProgress creates a new batch progress tracker.
func NewBatchProgress(items []string) *BatchProgress {
	bp := &BatchProgress{
		items: make([]batchProgressItem, len(items)),
		width: 0,
	}
	for i, name := range items {
		bp.items[i] = batchProgressItem{name: name, status: "pending"}
		if len(name) > bp.width {
			bp.width = len(name)
		}
	}
	return bp
}

// MarkSuccess marks an item as successful.
func (bp *BatchProgress) MarkSuccess(name string) {
	bp.printItem(name, "success", "")
}

// MarkError marks an item as failed.
func (bp *BatchProgress) MarkError(name, message string) {
	bp.printItem(name, "error", message)
}

func (bp *BatchProgress) printItem(name, status, message string) {
	var icon string
	var style lipgloss.Style

	switch status {
	case "success":
		icon = "[" + StyleGreen.Faint(true).Render("✓") + "]"
		style = StyleGreen
	case "error":
		icon = "[" + StyleRed.Faint(true).Render("✗") + "]"
		style = StyleRed
	default:
		icon = Gray("[ ]")
		style = StyleDim
	}

	line := fmt.Sprintf("  %s %s", icon, style.Render(name))
	if message != "" {
		line += " " + Gray("- "+message)
	}
	fmt.Println(line)
}
