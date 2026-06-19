package ui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestHighlightYAMLAddsSyntaxColorWithoutChangingText(t *testing.T) {
	input := "credentials:\n  op-expedient:\n    type: 1password\n    password_ref: op://Expedient/item/password\n    enabled: true\n    port: 22\n    include: [credentials/*.yaml, inventory/*.yaml]\n    # comment\n"

	got := HighlightYAML(input)

	if got == input {
		t.Fatal("HighlightYAML should add ANSI styling")
	}
	if stripped := ansiRE.ReplaceAllString(got, ""); stripped != input {
		t.Fatalf("HighlightYAML changed YAML text:\nwant %q\n got %q", input, stripped)
	}
	if strings.Contains(got, "m1\x1b[0mpassword") {
		t.Fatalf("plain string scalar was partially highlighted as a number:\n%s", got)
	}
	for _, want := range []string{"1password", "op://Expedient/item/password", "true", "22", "# comment"} {
		if !strings.Contains(got, want) {
			t.Fatalf("highlighted YAML missing %q:\n%s", want, got)
		}
	}
}
