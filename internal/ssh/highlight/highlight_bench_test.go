package highlight

import (
	"strings"
	"testing"
)

func BenchmarkHighlightNoTokens(b *testing.B) {
	h := New(Options{Enabled: true, Profile: ProfileJunos})
	input := []byte(strings.Repeat("plain output with ordinary columns and counters 12345\n", 64))
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		_ = h.Highlight(input)
	}
}

func BenchmarkHighlightTokenHeavy(b *testing.B) {
	h := New(Options{Enabled: true, Profile: ProfileJunos})
	input := []byte(strings.Repeat("set interfaces ge-0/0/0 description \"core uplink\" unit 0 family inet address 192.0.2.1/32 # managed\nset system archival archive-sites scp://ops@example.net/configs rib inet.0\nset firewall family inet filter EDGE term allow then accept target:64512:100 00:11:22:33:44:55\n", 32))
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		_ = h.Highlight(input)
	}
}

func BenchmarkHighlightANSIBypass(b *testing.B) {
	h := New(Options{Enabled: true, Profile: ProfileJunos})
	input := []byte("\x1b[31mdown\x1b[0m " + strings.Repeat("ge-0/0/0 down 192.0.2.1/32\n", 64))
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		_ = h.Highlight(input)
	}
}

func BenchmarkHighlightLongLineBypass(b *testing.B) {
	h := New(Options{Enabled: true, Profile: ProfileJunos})
	input := []byte(strings.Repeat("x", 9000) + " down\n")
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		_ = h.Highlight(input)
	}
}
