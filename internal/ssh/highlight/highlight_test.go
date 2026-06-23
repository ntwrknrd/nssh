package highlight

import (
	"strings"
	"testing"
)

func TestHighlightDisabledPassesThrough(t *testing.T) {
	input := []byte("ge-0/0/0 down 192.0.2.1/32\n")
	out := New(Options{Enabled: false, Profile: ProfileJunos}).Highlight(input)
	if string(out) != string(input) {
		t.Fatalf("output = %q, want passthrough", out)
	}
}

func TestHighlightBypassesANSIControlAndLongLines(t *testing.T) {
	h := New(Options{Enabled: true, Profile: ProfileJunos})
	tests := []string{
		"\x1b[31mdown\x1b[0m\n",
		"before\x00after down\n",
		strings.Repeat("x", 9000) + " down\n",
	}
	for _, input := range tests {
		out := h.Highlight([]byte(input))
		if string(out) != input {
			t.Fatalf("output = %q, want passthrough for %q", out, input[:min(len(input), 32)])
		}
	}
}

func TestJunosHighlightTokensAndPreservesText(t *testing.T) {
	input := "set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/32\n" +
		"protocols bgp group transit peer-as AS64512 route-target target:64512:100\n" +
		"ae0.10 up 00:11:22:33:44:55 vlan-id 100 active\n" +
		"discard rejected inactive routing-options\n"

	out := string(New(Options{Enabled: true, Profile: ProfileJunos}).Highlight([]byte(input)))
	if stripANSI(out) != input {
		t.Fatalf("stripANSI(output) = %q, want original input", stripANSI(out))
	}
	for _, want := range []string{
		"\x1b[35mset\x1b[0m",
		"\x1b[35minterfaces\x1b[0m",
		"\x1b[34mge-0/0/0\x1b[0m",
		"\x1b[36m192.0.2.1/32\x1b[0m",
		"\x1b[35mbgp\x1b[0m",
		"\x1b[36mAS64512\x1b[0m",
		"\x1b[36mtarget:64512:100\x1b[0m",
		"\x1b[34mae0.10\x1b[0m",
		"\x1b[36m00:11:22:33:44:55\x1b[0m",
		"\x1b[32mup\x1b[0m",
		"\x1b[32mactive\x1b[0m",
		"\x1b[31mdiscard\x1b[0m",
		"\x1b[31mrejected\x1b[0m",
		"\x1b[33minactive\x1b[0m",
		"\x1b[35mrouting-options\x1b[0m",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q in %q", want, out)
		}
	}
}

func TestJunosScanProducesDeterministicNonOverlappingSpans(t *testing.T) {
	spans := JunosProfile{}.Scan([]byte("ge-0/0/0 192.0.2.1 down"))
	if len(spans) != 3 {
		t.Fatalf("len(spans) = %d, want 3: %#v", len(spans), spans)
	}
	for i := 1; i < len(spans); i++ {
		if spans[i].Start < spans[i-1].End {
			t.Fatalf("spans overlap: %#v", spans)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
