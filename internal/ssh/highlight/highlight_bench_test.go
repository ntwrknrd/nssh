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
	input := []byte(strings.Repeat("ge-0/0/0 up 192.0.2.1/32 bgp established target:64512:100 00:11:22:33:44:55\n", 64))
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
