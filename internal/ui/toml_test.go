package ui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestHighlightTOMLAddsSyntaxColorWithoutChangingText(t *testing.T) {
	input := "[agent]\n  # Runtime behavior.\nidle_timeout = \"4h\"\nlabel = \"not # a comment\" # real comment\nauto_start = true\n"

	got := HighlightTOML(input)

	if got == input {
		t.Fatal("HighlightTOML should add ANSI styling")
	}
	if stripped := ansiRE.ReplaceAllString(got, ""); stripped != input {
		t.Fatalf("HighlightTOML changed TOML text:\nwant %q\n got %q", input, stripped)
	}
	for _, want := range []string{
		"[agent]",
		"# Runtime behavior.",
		"idle_timeout",
		"\"not # a comment\"",
		"# real comment",
		"\"4h\"",
		"true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("highlighted TOML missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("highlighted TOML missing ANSI escape sequences:\n%q", got)
	}
}
