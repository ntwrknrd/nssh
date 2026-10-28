package ui

import (
	"strings"
	"testing"
)

func TestSelectIndexWithShortcuts(t *testing.T) {
	options := []string{"Reject", "Accept once", "Accept always"}
	shortcuts := map[rune]int{'o': 1, 'a': 2}
	for input, want := range map[string]int{
		"\r":  0, // enter selects the default first option
		"o":   1,
		"A":   2,
		"j\r": 1, // arrow-style navigation still works
	} {
		got, err := SelectIndexWithShortcuts("Host key verification", options, shortcuts, strings.NewReader(input))
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if got != want {
			t.Fatalf("input %q: got %d, want %d", input, got, want)
		}
	}
}
