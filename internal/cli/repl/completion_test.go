package repl

import "testing"

func TestCompleteTargetTokenMidLinePreservesCommandsAndSuffix(t *testing.T) {
	line := "[ 'edge-1', 'coZZ', 'other' ] ( 'show version' )"
	cursor := len([]rune("[ 'edge-1', 'co"))
	updated, next, matches := completeTargetToken(line, cursor, []string{"core-1.example.net", "core-2.example.net", "edge-1"})
	want := "[ 'edge-1', 'core-ZZ', 'other' ] ( 'show version' )"
	if updated != want {
		t.Fatalf("updated=%q want=%q", updated, want)
	}
	if next != len([]rune("[ 'edge-1', 'core-")) {
		t.Fatalf("cursor=%d", next)
	}
	if len(matches) != 2 {
		t.Fatalf("matches=%v", matches)
	}
}
func TestCompleteTargetTokenPreservesUserAndDistinctFQDNs(t *testing.T) {
	line := "[ 'ops@leaf.a' ] ( 'show version' )"
	cursor := len([]rune("[ 'ops@leaf.a"))
	updated, _, matches := completeTargetToken(line, cursor, []string{"leaf.alpha.example", "leaf.alpine.example", "leaf.beta.example"})
	if updated != "[ 'ops@leaf.alp' ] ( 'show version' )" {
		t.Fatalf("updated=%q", updated)
	}
	if len(matches) != 2 || matches[0] == matches[1] {
		t.Fatalf("matches=%v", matches)
	}
}
func TestCompleteTargetTokenIgnoresCommandsAndEscapedQuote(t *testing.T) {
	line := "[ 'edge\\'quoted', 'ed' ] ( 'show ed' )"
	cursor := len([]rune("[ 'edge\\'quoted', 'ed"))
	updated, _, _ := completeTargetToken(line, cursor, []string{"edge-1"})
	if updated != "[ 'edge\\'quoted', 'edge-1' ] ( 'show ed' )" {
		t.Fatalf("updated=%q", updated)
	}
	unchanged, cursor, _ := completeTargetToken(line, len([]rune(line)), []string{"edge-1"})
	if unchanged != line || cursor != len([]rune(line)) {
		t.Fatalf("command text changed: %q", unchanged)
	}
}
