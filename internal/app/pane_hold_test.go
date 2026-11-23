package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/exit"
)

func TestHoldFailedPane(t *testing.T) {
	tests := []struct {
		name   string
		active bool
		err    error
		holds  bool
	}{
		{name: "connection failure in a pane", active: true, err: exit.ErrConnectionFailed, holds: true},
		{name: "other error in a pane", active: true, err: errors.New("boom"), holds: true},
		{name: "remote exit status", active: true, err: &exit.ExitError{Code: 1, Message: "ssh exited with code 1", Remote: true}},
		{name: "success", active: true},
		{name: "not a pane", err: exit.ErrConnectionFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.NewReader("\n")
			var output bytes.Buffer
			restoreInput, restoreOutput, restoreActive := paneHoldInput, paneHoldOutput, paneHoldActive
			t.Cleanup(func() { paneHoldInput, paneHoldOutput, paneHoldActive = restoreInput, restoreOutput, restoreActive })
			paneHoldInput, paneHoldOutput = input, &output
			paneHoldActive = func() bool { return tt.active }

			holdFailedPane(tt.err)

			if held := output.Len() > 0; held != tt.holds {
				t.Fatalf("held = %v, want %v (output %q)", held, tt.holds, output.String())
			}
			if tt.holds && !strings.Contains(output.String(), "press Enter") {
				t.Fatalf("output = %q, want the Enter prompt", output.String())
			}
		})
	}
}
