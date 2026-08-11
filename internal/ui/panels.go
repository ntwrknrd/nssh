package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PrintKeyValue prints a formatted key-value pair with info status prefix.
func PrintKeyValue(key, value string) {
	bracket := StyleDim.Render("[-]")
	fmt.Printf("  %s %s %s\n", bracket, StyleLabel.Render(key+":"), StyleValue.Render(value))
}

// NumberedList prints a numbered list.
func NumberedList(items []string) {
	for i, item := range items {
		num := StyleDim.Render(fmt.Sprintf("%d.", i+1))
		fmt.Printf("  %s %s\n", num, item)
	}
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
