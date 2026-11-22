package rex

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		name string
		in   Report
		want string
	}{
		{name: "root working", in: Report{State: StateWorking, Title: "acm-core1", Msg: "Connecting to acm-core1"},
			want: "\x1b]7501;state=working:app=nssh:title=YWNtLWNvcmUx:msg=Q29ubmVjdGluZyB0byBhY20tY29yZTE=\x1b\\"},
		{name: "blocked kind and progress", in: Report{State: StateBlocked, Kind: KindQuestion, Progress: 40},
			want: "\x1b]7501;state=blocked:app=nssh:kind=question:progress=40\x1b\\"},
		{name: "kind only with blocked", in: Report{State: StateIdle, Kind: KindAuth, Progress: 40},
			want: "\x1b]7501;state=idle:app=nssh\x1b\\"},
		{name: "child inherits app", in: Report{State: StateDone, ID: "hosts/core1"},
			want: "\x1b]7501;state=done:id=hosts/core1\x1b\\"},
		{name: "clear root", in: Report{State: StateClear, Title: "x"}, want: "\x1b]7501;state=clear\x1b\\"},
		{name: "clear subtree", in: Report{State: StateClear, ID: "hosts"}, want: "\x1b]7501;state=clear:id=hosts\x1b\\"},
		{name: "controls stripped", in: Report{State: StateIdle, Msg: "\x1b\a\n\u009b"}, want: "\x1b]7501;state=idle:app=nssh\x1b\\"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Encode(tt.in); got != tt.want {
				t.Fatalf("Encode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeRespectsLimits(t *testing.T) {
	got := Encode(Report{State: StateIdle, Title: strings.Repeat("é", 200), Msg: strings.Repeat("x", 5000)})
	if len(got) > 3300 {
		t.Fatalf("report is %d bytes, want under 3300", len(got))
	}
	if got := clean(strings.Repeat("é", 200), maxTitleBytes); len(got) != 192 {
		t.Fatalf("title = %d bytes, want 192", len(got))
	}
}

func TestSegment(t *testing.T) {
	if got := Segment("acm-core1.pit2"); got != "acm-core1.pit2" {
		t.Fatalf("Segment kept name = %q", got)
	}
	for _, name := range []string{"admin@core1", strings.Repeat("a", 33), ""} {
		got := Segment(name)
		if got == name || len(got) != 14 || !strings.HasPrefix(got, "h-") || got != Segment(name) {
			t.Fatalf("Segment(%q) = %q, want stable hash", name, got)
		}
	}
}

func TestDetectedAndWriter(t *testing.T) {
	t.Setenv("REX_SESSION", "s1")
	t.Setenv("REX_BLOCK", "b1")
	t.Setenv("NSSH_REX", "")
	if !Detected() {
		t.Fatal("Detected = false inside Rex")
	}
	var out bytes.Buffer
	NewWriter(&out).Report(Report{State: StateIdle})
	if out.Len() == 0 {
		t.Fatal("writer wrote nothing inside Rex")
	}

	t.Setenv("NSSH_REX", "0")
	if Detected() || NewWriter(&out) != nil {
		t.Fatal("NSSH_REX=0 did not disable integration")
	}
	t.Setenv("NSSH_REX", "")
	t.Setenv("REX_BLOCK", "")
	if Detected() {
		t.Fatal("Detected = true without REX_BLOCK")
	}
	var w *Writer
	w.Report(Report{State: StateIdle}) // nil writer is a no-op
}
