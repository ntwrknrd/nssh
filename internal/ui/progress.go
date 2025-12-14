package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ProgressBar renders a styled progress bar that matches the UI theme.
// Example output:
//
//	[-] Exporting [████████████░░░░░░░░░░░░] 60/100 60%
type ProgressBar struct {
	label       string
	current     int
	total       int
	width       int
	showCount   bool
	showPercent bool
}

// NewProgressBar creates a new progress bar with the given label.
func NewProgressBar(label string, total int) *ProgressBar {
	return &ProgressBar{
		label:       label,
		total:       total,
		width:       30,
		showCount:   true,
		showPercent: true,
	}
}

// WithWidth sets the bar width (default 30).
func (p *ProgressBar) WithWidth(width int) *ProgressBar {
	p.width = width
	return p
}

// WithoutCount hides the count display.
func (p *ProgressBar) WithoutCount() *ProgressBar {
	p.showCount = false
	return p
}

// WithoutPercent hides the percentage display.
func (p *ProgressBar) WithoutPercent() *ProgressBar {
	p.showPercent = false
	return p
}

// Update sets the current progress value.
func (p *ProgressBar) Update(current int) {
	p.current = current
}

// Render returns the progress bar as a styled string.
func (p *ProgressBar) Render() string {
	// Calculate percentage
	percent := 0.0
	if p.total > 0 {
		percent = float64(p.current) / float64(p.total)
	}
	if percent > 1.0 {
		percent = 1.0
	}

	// Build the bar
	filled := int(percent * float64(p.width))
	empty := p.width - filled

	barStyle := lipgloss.NewStyle().Foreground(ColorCyan)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorDim)
	bracketStyle := lipgloss.NewStyle().Foreground(ColorGray)

	bar := barStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty))

	// Build status parts
	var parts []string

	// Bracket with status indicator (2-space indent to match other status messages)
	bracket := bracketStyle.Render("[") + Gray("-") + bracketStyle.Render("]")
	parts = append(parts, "  ", bracket)

	// Label
	if p.label != "" {
		parts = append(parts, " ", p.label)
	}

	// Bar with brackets
	parts = append(parts, " ", bracketStyle.Render("["), bar, bracketStyle.Render("]"))

	// Count
	if p.showCount {
		countStr := fmt.Sprintf(" %d/%d", p.current, p.total)
		parts = append(parts, Gray(countStr))
	}

	// Percentage
	if p.showPercent {
		percentStr := fmt.Sprintf(" %d%%", int(percent*100))
		parts = append(parts, Cyan(percentStr))
	}

	return strings.Join(parts, "")
}

// Print renders the progress bar to stdout with carriage return (for updates).
func (p *ProgressBar) Print() {
	fmt.Print("\r" + p.Render())
}

// PrintLn renders the progress bar to stdout with newline (final state).
func (p *ProgressBar) PrintLn() {
	fmt.Println("\r" + p.Render())
}

// Clear clears the progress bar line.
func (p *ProgressBar) Clear() {
	fmt.Print("\r" + strings.Repeat(" ", termWidth()) + "\r")
}

// RenderProgress is a helper to render a simple one-line progress bar.
// Returns the rendered string without printing.
func RenderProgress(label string, current, total int) string {
	return NewProgressBar(label, total).WithWidth(30).Render()
}
