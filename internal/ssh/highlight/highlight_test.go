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
		"set protocols bgp group transit peer-as AS64512 route-target target:64512:100\n" +
		"ae0.10 up 00:11:22:33:44:55 vlan-id 100 active\n" +
		"set routing-options static route 0.0.0.0/0 discard rejected inactive\n"

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
	if actionStyle != "\x1b[93m" {
		t.Fatalf("action style should be visually distinct bright yellow, got %q", actionStyle)
	}
}

func TestJunosHighlightColorsMajorSetHierarchies(t *testing.T) {
	input := "set version 23.4R2-S7.4\n" +
		"set groups re0 system host-name acm-lab-core1-re0\n" +
		"set apply-groups re0\n" +
		"set services ssh root-login deny\n" +
		"set security zones security-zone trust\n" +
		"set snmp community public authorization read-only\n" +
		"set forwarding-options storm-control-profiles default\n" +
		"set event-options policy link-down\n" +
		"set accounting-options file interactive-commands\n"

	out := string(New(Options{Enabled: true, Profile: ProfileJunos}).Highlight([]byte(input)))
	if stripANSI(out) != input {
		t.Fatalf("stripANSI(output) = %q, want original input", stripANSI(out))
	}
	for _, token := range []string{"version", "groups", "apply-groups", "services", "security", "snmp", "forwarding-options", "event-options", "accounting-options"} {
		if !tokenHighlighted(out, token) {
			t.Fatalf("major hierarchy token %q was not highlighted in %q", token, out)
		}
		if tokenStyle(out, token) != tokenStyle(out, "version") {
			t.Fatalf("major hierarchy token %q has style %q, want %q", token, tokenStyle(out, token), tokenStyle(out, "version"))
		}
	}
}

func TestJunosHighlightGatesHierarchyAndProtocolsByLineContext(t *testing.T) {
	input := "This system is for the use of authorized individuals only.\n" +
		"Last login: Tue Apr 29 02:54:36 2025 from re1\n" +
		"system use is monitored.\n" +
		"set system host-name acm-lab-core1\n" +
		"system {\n" +
		"set protocols bgp group edge neighbor 100.64.128.1\n"

	out := string(New(Options{Enabled: true, Profile: ProfileJunos}).Highlight([]byte(input)))
	if stripANSI(out) != input {
		t.Fatalf("stripANSI(output) = %q, want original input", stripANSI(out))
	}
	if firstLine := strings.SplitN(out, "\n", 2)[0]; tokenHighlighted(firstLine, "system") {
		t.Fatalf("banner system should not be highlighted: %q", firstLine)
	}
	if loginLine := strings.Split(out, "\n")[1]; tokenHighlighted(loginLine, "from") {
		t.Fatalf("login banner should not highlight free text: %q", loginLine)
	}
	if systemStartLine := strings.Split(out, "\n")[2]; tokenHighlighted(systemStartLine, "system") {
		t.Fatalf("free text starting with system should not be highlighted: %q", systemStartLine)
	}
	for _, token := range []string{"set", "system", "protocols", "bgp", "100.64.128.1"} {
		if !tokenHighlighted(out, token) {
			t.Fatalf("token %q was not highlighted in config context: %q", token, out)
		}
	}
}

func TestHighlighterCarriesConfigContextAcrossChunks(t *testing.T) {
	h := New(Options{Enabled: true, Profile: ProfileJunos})
	first := string(h.Highlight([]byte("set protocols ")))
	second := string(h.Highlight([]byte("bgp group edge\n")))
	third := string(h.Highlight([]byte("This system is monitored.\n")))

	if stripANSI(first+second+third) != "set protocols bgp group edge\nThis system is monitored.\n" {
		t.Fatalf("highlighting changed text: %q", stripANSI(first+second+third))
	}
	if !tokenHighlighted(second, "bgp") {
		t.Fatalf("continued config chunk should highlight protocol context: %q", second)
	}
	if tokenHighlighted(third, "system") {
		t.Fatalf("context should reset after newline: %q", third)
	}

	h = New(Options{Enabled: true, Profile: ProfileJunos})
	_ = h.Highlight([]byte("set "))
	out := string(h.Highlight([]byte("event-options policy STORM_CTL events l2ald_st_ctl_in_effect\n")))
	if !tokenHighlighted(out, "event-options") {
		t.Fatalf("continued config chunk should highlight hierarchy context: %q", out)
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
