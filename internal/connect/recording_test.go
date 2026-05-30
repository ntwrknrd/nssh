//go:build unix

package connect

import "testing"

func TestRecordingWrapperSkipsInsideRecording(t *testing.T) {
	t.Setenv("NSSH_RECORDING_INNER", "1")

	got, err := maybeWrapWithRecording("edge01", []string{"--", "show version"})
	if err != nil {
		t.Fatalf("maybeWrapWithRecording() error = %v", err)
	}
	if got {
		t.Fatal("maybeWrapWithRecording() = true, want false inside recording")
	}
}

func TestRecordingWrapperContinuesWhenDisabled(t *testing.T) {
	t.Setenv("NSSH_RECORD", "0")

	got, err := maybeWrapWithRecording("edge01", nil)
	if err != nil {
		t.Fatalf("maybeWrapWithRecording() error = %v", err)
	}
	if got {
		t.Fatal("maybeWrapWithRecording() = true, want false when disabled")
	}
}
