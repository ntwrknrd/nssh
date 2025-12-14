package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Box prints content in a styled box with optional title.
func Box(title, content string) {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorDim).
		Padding(1, 2)

	if title != "" {
		fmt.Println(Ruler(title))
	}
	fmt.Println(boxStyle.Render(content))
}

// WarningBox prints content in a red warning box.
func WarningBox(title, content string) {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorRed).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(ColorRed).
		Bold(true)

	if title != "" {
		fmt.Println(titleStyle.Render(title))
	}
	fmt.Println(boxStyle.Render(content))
}

// InfoBox prints content in a cyan info box.
func InfoBox(title, content string) {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorCyan).
		Padding(1, 2)

	if title != "" {
		fmt.Println(Ruler(title))
	}
	fmt.Println(boxStyle.Render(content))
}

// SuccessBox prints content in a green success box.
func SuccessBox(title, content string) {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGreen).
		Padding(1, 2)

	if title != "" {
		fmt.Println(Ruler(title))
	}
	fmt.Println(boxStyle.Render(content))
}

// KeyValue returns a formatted key-value pair.
func KeyValue(key, value string) string {
	return fmt.Sprintf("%s %s", StyleLabel.Render(key+":"), StyleValue.Render(value))
}

// PrintKeyValue prints a formatted key-value pair with info status prefix.
func PrintKeyValue(key, value string) {
	bracket := StyleDim.Render("[-]")
	fmt.Printf("  %s %s %s\n", bracket, StyleLabel.Render(key+":"), StyleValue.Render(value))
}

// KeyValueBlock prints multiple key-value pairs aligned.
func KeyValueBlock(pairs map[string]string, order []string) {
	// Find max key length
	maxLen := 0
	for _, k := range order {
		if len(k) > maxLen {
			maxLen = len(k)
		}
	}

	for _, k := range order {
		v := pairs[k]
		label := StyleLabel.Render(fmt.Sprintf("%-*s", maxLen+1, k+":"))
		value := StyleValue.Render(v)
		fmt.Printf("%s %s\n", label, value)
	}
}

// List prints a bulleted list.
func List(items []string) {
	bullet := StyleDim.Render("-")
	for _, item := range items {
		fmt.Printf("  %s %s\n", bullet, item)
	}
}

// NumberedList prints a numbered list.
func NumberedList(items []string) {
	for i, item := range items {
		num := StyleDim.Render(fmt.Sprintf("%d.", i+1))
		fmt.Printf("  %s %s\n", num, item)
	}
}

// Indent returns a string with consistent indentation.
func Indent(s string, level int) string {
	indent := strings.Repeat("  ", level)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// VisualWidth returns the visual width of a string, ignoring ANSI escape codes.
func VisualWidth(s string) int {
	clean := StripANSI(s)
	return len(clean)
}

// StripANSI removes ANSI escape codes from a string.
func StripANSI(s string) string {
	var result strings.Builder
	inEscape := false

	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			// ANSI sequences end with a letter
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteByte(s[i])
	}

	return result.String()
}

// BorderedPanel renders content in a bordered panel with title on the border.
// titleAlign: "left" (default) or "center"
// width: 0 = auto-fit to content
// padY: add empty lines inside top/bottom of box
func BorderedPanel(title, content string, titleAlign string, width int, padY ...bool) string {
	addPadY := len(padY) > 0 && padY[0]
	borderStyle := lipgloss.NewStyle().Foreground(ColorDim)
	titleStyle := lipgloss.NewStyle().Foreground(ColorGray)

	lines := strings.Split(content, "\n")

	// Calculate width if auto (width=0)
	if width == 0 {
		maxLineWidth := 0
		for _, line := range lines {
			if w := VisualWidth(line); w > maxLineWidth {
				maxLineWidth = w
			}
		}
		titleLen := len(title) + 2 // " Title "
		if titleLen > maxLineWidth {
			maxLineWidth = titleLen
		}
		width = maxLineWidth + 4 // borders + padding: "│ " + " │"
	}

	innerWidth := width - 4 // Account for borders and padding

	// Build title text
	titleText := fmt.Sprintf(" %s ", title)
	titleLen := len(titleText)

	var sb strings.Builder

	// Top border with title
	if titleAlign == "center" {
		// Center: split dashes evenly
		dashesTotal := innerWidth - titleLen
		if dashesTotal < 0 {
			dashesTotal = 0
		}
		dashesLeft := dashesTotal / 2
		dashesRight := dashesTotal - dashesLeft

		sb.WriteString(borderStyle.Render("╭" + strings.Repeat("─", dashesLeft+1)))
		sb.WriteString(titleStyle.Render(titleText))
		sb.WriteString(borderStyle.Render(strings.Repeat("─", dashesRight+1) + "╮"))
	} else {
		// Left aligned (default)
		dashesAfterTitle := innerWidth - titleLen
		if dashesAfterTitle < 0 {
			dashesAfterTitle = 0
		}
		sb.WriteString(borderStyle.Render("╭─"))
		sb.WriteString(titleStyle.Render(titleText))
		sb.WriteString(borderStyle.Render(strings.Repeat("─", dashesAfterTitle) + "─╮"))
	}
	sb.WriteString("\n")

	// Helper to render empty padding line
	emptyLine := func() {
		sb.WriteString(borderStyle.Render("│ "))
		sb.WriteString(strings.Repeat(" ", innerWidth))
		sb.WriteString(borderStyle.Render(" │"))
		sb.WriteString("\n")
	}

	// Top padding
	if addPadY {
		emptyLine()
	}

	// Content lines
	for _, line := range lines {
		visualLen := VisualWidth(line)
		padding := innerWidth - visualLen
		if padding < 0 {
			padding = 0
		}

		sb.WriteString(borderStyle.Render("│ "))
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(borderStyle.Render(" │"))
		sb.WriteString("\n")
	}

	// Bottom padding
	if addPadY {
		emptyLine()
	}

	// Bottom border
	sb.WriteString(borderStyle.Render("╰" + strings.Repeat("─", innerWidth+2) + "╯"))

	return sb.String()
}
