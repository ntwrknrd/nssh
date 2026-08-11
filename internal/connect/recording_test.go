//go:build unix

package connect

import (
	"slices"
	"testing"
)

func TestRecordingWrapperSkipsInsideRecording(t *testing.T) {
	t.Setenv("NSSH_RECORDING_INNER", "1")

	got, err := maybeWrapWithRecording("edge01", []string{"--", "show version"}, Options{})
	if err != nil {
		t.Fatalf("maybeWrapWithRecording() error = %v", err)
	}
	if got {
		t.Fatal("maybeWrapWithRecording() = true, want false inside recording")
	}
}

func TestRecordingWrapperContinuesWhenDisabled(t *testing.T) {
	t.Setenv("NSSH_RECORD", "0")

	got, err := maybeWrapWithRecording("edge01", nil, Options{})
	if err != nil {
		t.Fatalf("maybeWrapWithRecording() error = %v", err)
	}
	if got {
		t.Fatal("maybeWrapWithRecording() = true, want false when disabled")
	}
}

func TestRecordingInnerCommandPreservesNSSHVerbosity(t *testing.T) {
	got := recordingInnerCommand("/bin/nssh", "edge01", []string{"-o", "BatchMode=yes"}, Options{Verbosity: 2})
	want := []string{"/bin/nssh", "-vv", "edge01", "-o", "BatchMode=yes"}
	if !slices.Equal(got, want) {
		t.Fatalf("recordingInnerCommand() = %#v, want %#v", got, want)
	}
}
