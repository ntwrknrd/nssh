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
	for _, token := range []string{"set", "interfaces", "ge-0/0/0", "192.0.2.1/32", "bgp", "AS64512", "target:64512:100", "ae0.10", "00:11:22:33:44:55", "up", "active", "discard", "rejected", "inactive", "routing-options"} {
		if !tokenHighlighted(out, token) {
			t.Fatalf("token %q was not highlighted in %q", token, out)
		}
	}
}

func TestJunosHighlightProfileContractColorsOnlyBroadCategories(t *testing.T) {
	input := "set protocols bgp group ebgp neighbor 100.64.128.1 peer-as 65551\n" +
		"set protocols ospf3 area 0.0.0.0 interface xe-8/0/0.0 bfd-liveness-detection minimum-interval 999\n" +
		"set protocols mpls interface lo0.0\n" +
		"set protocols lldp interface all\n" +
		"set protocols l2-learning global-mac-table-aging-time 14700\n"

	out := string(New(Options{Enabled: true, Profile: ProfileJunos}).Highlight([]byte(input)))
	if stripANSI(out) != input {
		t.Fatalf("stripANSI(output) = %q, want original input", stripANSI(out))
	}

	for _, token := range []string{"set", "protocols", "bgp", "ospf3", "mpls", "lldp", "l2-learning", "100.64.128.1", "xe-8/0/0.0", "lo0.0"} {
		if !tokenHighlighted(out, token) {
			t.Fatalf("token %q was not highlighted in %q", token, out)
		}
	}

	for _, token := range []string{"group", "neighbor", "peer-as", "area", "interface", "bfd-liveness-detection", "minimum-interval", "global-mac-table-aging-time"} {
		if tokenHighlighted(out, token) {
			t.Fatalf("token %q should not be highlighted in %q", token, out)
		}
	}

	actionStyle := tokenStyle(out, "set")
	hierarchyStyle := tokenStyle(out, "protocols")
	protocolStyle := tokenStyle(out, "bgp")
	if actionStyle == "" || hierarchyStyle == "" || protocolStyle == "" {
		t.Fatalf("missing category style: action=%q hierarchy=%q protocol=%q", actionStyle, hierarchyStyle, protocolStyle)
	}
	if actionStyle == hierarchyStyle || actionStyle == protocolStyle || hierarchyStyle == protocolStyle {
		t.Fatalf("category styles should differ: action=%q hierarchy=%q protocol=%q", actionStyle, hierarchyStyle, protocolStyle)
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

func tokenHighlighted(s, token string) bool {
	return tokenStyle(s, token) != ""
}

func tokenStyle(s, token string) string {
	target := token + "\x1b[0m"
	idx := strings.Index(s, target)
	if idx == -1 {
		return ""
	}
	start := strings.LastIndex(s[:idx], "\x1b[")
	if start == -1 {
		return ""
	}
	return s[start:idx]
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
