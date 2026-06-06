package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// termWidth returns the terminal width, defaulting to 80 if unknown.
func termWidth() int {
	if w, _, err := term.GetSize(0); err == nil && w > 0 {
		return w
	}
	if raw := strings.TrimSpace(os.Getenv("COLUMNS")); raw != "" {
		if w, err := strconv.Atoi(raw); err == nil && w > 0 {
			return w
		}
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
