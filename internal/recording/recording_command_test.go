//go:build unix

package recording

import (
	"slices"
	"testing"
)

func TestBuildAsciinemaCommandOmitsWindowSize(t *testing.T) {
	got := BuildAsciinemaCommand(RecordingPlan{
		CastPath:      "session.cast",
		AsciinemaPath: "/opt/bin/asciinema",
		Title:         "nssh:edge01",
	}, []string{"nssh", "edge01"})

	if slices.Contains(got, "--window-size") {
		t.Fatalf("asciinema command should not set live window size: %#v", got)
	}
}
